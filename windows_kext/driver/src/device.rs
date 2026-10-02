use alloc::{format, string::String, vec::Vec};
use core::{
    ffi::c_void,
    sync::atomic::{AtomicPtr, AtomicU32, Ordering},
};
use num_traits::FromPrimitive;
use protocol::{
    command::{CommandParseError, CommandType},
    info::Info,
};
use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address, Ipv6Address};
use wdk::{
    driver::Driver,
    filter_engine::{
        callout_data::{CalloutData, ClassifyDefer},
        net_buffer::{NetBufferList, NetworkAllocator},
        packet::{InjectInfo, InjectionPath, Injector},
        FilterEngine, UnregisterCalloutsResult,
    },
    ioqueue::{self, IOQueue},
    irp_helpers::{ReadRequest, WriteRequest},
    passive_mutex::PassiveMutex,
    rw_spin_lock::RwSpinLock,
};

use windows_sys::Win32::Foundation::{
    NTSTATUS, STATUS_INVALID_DEVICE_STATE, STATUS_INVALID_PARAMETER,
};

use crate::{
    array_holder::ArrayHolder,
    bandwidth::Bandwidth,
    callouts,
    connection_cache::ConnectionCache,
    connection_map::Key,
    dbg, err,
    icmp_echo_cache::IcmpEchoCache,
    id_cache::{IdCache, PendingPacket},
    logger,
    packet_util::Redirect,
    tcp_closure_cache::{PendingTcpClosure, TcpClosureCache},
    tcp_endpoint_cache::{TcpEndpointCache, TcpEndpointConnection},
    udp_endpoint_cache::UdpEndpointCache,
    udp_flow_cache::{UdpFlowCache, UdpFlowRegistration},
};

pub enum Packet {
    /// One independently decided network-layer packet.
    Network(NetBufferList, InjectInfo),
    /// Temporary owner used only while a WFP NBL batch is being cloned, redirected
    /// immediately, or split into independent pending requests.
    NetworkBatch(Vec<NetBufferList>, InjectInfo),
    AleLayer(ClassifyDefer),
}

impl Packet {
    /// Returns whether applying a verdict requires a passive-level WFP
    /// management transaction before the saved packet can be reinjected.
    fn requires_reauthorization(&self) -> bool {
        matches!(
            self,
            Self::AleLayer(defer) if defer.is_reauthorization()
        )
    }

    /// Returns whether this packet remains independently deliverable after its
    /// socket connection has ended.
    ///
    /// Outbound UDP is absorbed only after the socket send has completed. Its
    /// network-layer clone already contains the complete datagram and can be
    /// injected without the endpoint that originated it. ALE packets and inbound
    /// network packets still depend on their live connection lifecycle.
    pub(crate) fn survives_connection_end(&self, key: &Key) -> bool {
        key.protocol == IpProtocol::Udp
            && matches!(
                self,
                Self::Network(_, inject_info)
                    | Self::NetworkBatch(_, inject_info)
                    if !inject_info.inbound
            )
    }
}

/// Keeps an ID visible as in-progress until its userspace verdict has been fully
/// applied. Endpoint closure snapshots include these claimed requests as well as
/// IDs still waiting in the packet cache.
struct PendingRequestGuard<'a> {
    device: &'a Device,
    id: u64,
}

impl Drop for PendingRequestGuard<'_> {
    fn drop(&mut self) {
        self.device.finish_pending_request(self.id);
    }
}

pub(crate) struct CommandWriteError {
    pub(crate) status: NTSTATUS,
    pub(crate) bytes_processed: usize,
}

impl CommandWriteError {
    fn new(status: NTSTATUS, bytes_processed: usize) -> Self {
        Self {
            status,
            bytes_processed,
        }
    }
}

// Device Context
pub struct Device {
    pub(crate) filter_engine: PassiveMutex<FilterEngine>,
    /// Serializes the complete event-stream read transaction: consuming a saved
    /// fragment, waiting for/dequeuing records, and publishing the next fragment.
    /// A spin lock cannot protect this state because an empty stream may wait.
    read_stream: PassiveMutex<ArrayHolder>,
    pub(crate) event_queue: IOQueue<Info>, // Queue for events to user-space
    pub(crate) packet_cache: RwSpinLock<IdCache>, // Cache of pending packets waiting for verdict
    pub(crate) connection_cache: ConnectionCache, // Cache of connections and their verdicts
    /// Exact TCP connection generations keyed by WFP transport endpoint handle.
    /// Endpoint closure consumes this identity instead of resolving a reused tuple.
    pub(crate) tcp_endpoint_cache: RwSpinLock<TcpEndpointCache>,
    /// TCP endpoint-closure classifications delayed until the packet requests
    /// already queued for that flow have finished.
    tcp_closure_cache: RwSpinLock<TcpClosureCache>,
    /// UDP remote tuples grouped by WFP transport endpoint handle. A UDP socket
    /// receives one endpoint-closure indication regardless of its remote peers.
    pub(crate) udp_endpoint_cache: RwSpinLock<UdpEndpointCache>,
    /// Contexts currently owned by WFP for per-peer UDP ALE flows.
    pub(crate) udp_flow_cache: UdpFlowCache,
    /// (remote address, echo identifier) -> PID that sent the request.
    /// An inbound echo reply has no process of its own to read, so it is matched
    /// against the outbound request that caused it.
    pub(crate) icmp_echo_cache: RwSpinLock<IcmpEchoCache>,
    pub(crate) injector: Injector,
    pub(crate) network_allocator: NetworkAllocator,
    /// Each protocol/address-family map owns its own spin lock, so unrelated
    /// packet callbacks do not serialize on one global bandwidth lock.
    pub(crate) bandwidth_stats: Bandwidth,
    /// File object for the one accepted user-mode device open. The pointer is an
    /// opaque identity token; it is never dereferenced. A rejected CREATE gets a
    /// different file object, so its CLEANUP cannot release the active owner.
    pub(crate) owner_file_object: AtomicPtr<c_void>,
    /// PID belonging to `owner_file_object`, used by callouts to recognize the
    /// current Portmaster process. Zero means that no device open is accepted.
    pub(crate) owner_pid: AtomicU32,
}

// Every mutable field is owned by its synchronization primitive. The resource
// wrappers in wdk expose their cross-thread contracts individually, so Device's
// auto-traits are checked field by field rather than bypassed with a blanket impl.

/// Sends a final asynchronous WFP injection failure through the driver's existing
/// log ring. WFP can invoke this callback at `DISPATCH_LEVEL`; `err!` allocates its
/// bounded record from nonpaged pool and publishes it under `RwSpinLock`, without
/// a pageable or waitable operation or any reference to `Device` state. The
/// injection-handle destroy path drains every completion before the image unloads.
///
/// # Safety
///
/// `Injector` calls this only while the driver image and its static log ring remain
/// live, at an IRQL no higher than `DISPATCH_LEVEL`.
unsafe fn report_injection_failure(path: InjectionPath, status: NTSTATUS) {
    err!(
        "asynchronous WFP {} injection failed: NTSTATUS({:#010x})",
        path.as_str(),
        status as u32
    );
}

impl Device {
    /// Initialize all members of the device. Memory is handled by windows.
    /// Make sure everything is initialized here.
    pub fn new(driver: &Driver) -> Result<Self, String> {
        // Complete every fallible standalone allocation before registering WFP
        // callbacks. If either resource is unavailable, DriverEntry fails without
        // ever exposing a callback that depends on a partially built Device.
        let injector = Injector::new_with_failure_callback(Some(report_injection_failure))
            .map_err(|err| format!("injector error: {}", err))?;
        let network_allocator =
            NetworkAllocator::new().map_err(|err| format!("network allocator error: {}", err))?;
        let read_stream = PassiveMutex::new(ArrayHolder::default())
            .map_err(|err| format!("read stream lock error: {}", err))?;

        let filter_engine = FilterEngine::new(driver, 0x7dab1057_8e2b_40c4_9b85_693e381d7896)
            .map_err(|err| alloc::format!("filter engine error: {}", err))?;
        // Initialize the passive-level lock before publishing the Device. WFP
        // filters are committed separately, after the complete Device has been
        // stored in the global pointer.
        let filter_engine = PassiveMutex::new(filter_engine)
            .map_err(|err| format!("filter engine lock error: {}", err))?;

        Ok(Self {
            filter_engine,
            read_stream,
            event_queue: IOQueue::new(), // Queue for events to user-space
            packet_cache: RwSpinLock::new(IdCache::new()), // Cache of pending packets waiting for verdict
            connection_cache: ConnectionCache::new(),
            tcp_endpoint_cache: RwSpinLock::new(TcpEndpointCache::new()),
            tcp_closure_cache: RwSpinLock::new(TcpClosureCache::new()),
            udp_endpoint_cache: RwSpinLock::new(UdpEndpointCache::new()),
            udp_flow_cache: UdpFlowCache::new(),
            icmp_echo_cache: RwSpinLock::new(IcmpEchoCache::new()),
            injector,
            network_allocator,
            bandwidth_stats: Bandwidth::new(),
            owner_file_object: AtomicPtr::new(core::ptr::null_mut()),
            owner_pid: AtomicU32::new(0),
        })
    }

    /// Registers and activates all WFP state after the complete Device has been
    /// published. Runtime callbacks can begin as soon as the FWPM transaction is
    /// committed, so publication must precede this call.
    pub fn start_filtering(&self) -> Result<(), String> {
        let mut filter_engine = self
            .filter_engine
            .lock()
            .map_err(|err| format!("failed to acquire filter engine: {}", err))?;
        filter_engine.commit(callouts::get_callout_vec())
    }

    /// Returns the PID of the process that currently has the device handle open, or 0 if none.
    pub fn is_owner_pid(&self, pid: u32) -> bool {
        let p = self.owner_pid.load(Ordering::Acquire);
        p != 0 && p == pid
    }

    /// Marks reads waiting for an event so the owning handle's cleanup can
    /// finish. The queue observes this state at its next bounded wait check.
    pub fn cancel_read_waiters(&self) {
        self.event_queue.cancel_waiters();
    }

    /// Clears the read cancellation state after all reads from the previous
    /// owner have returned and a new owner has been installed.
    pub fn reset_read_cancellation(&self) {
        self.event_queue.reset_cancellation();
    }

    /// Reauthorizes existing ALE flows after a cache policy change.
    ///
    /// WFP management APIs require PASSIVE_LEVEL. Keep this operation behind a
    /// passive-level mutex rather than the spin lock used by packet-path state;
    /// the caller must be a PASSIVE_LEVEL dispatch path (normally a user write).
    fn reset_filters(&self) -> Result<(), String> {
        let mut filter_engine = self
            .filter_engine
            .lock()
            .map_err(|err| format!("failed to acquire filter engine: {}", err))?;
        filter_engine.reset_all_filters()
    }

    /// Reauthorization is already covered when another passive worker owns the
    /// WFP transaction. Preserve the old behavior for a deferred ALE verdict:
    /// the cached verdict is visible to the next classification even when this
    /// particular reset loses the transaction race.
    fn reset_filters_for_defer(&self) -> Result<(), String> {
        match self.reset_filters() {
            Err(err) if err.contains("STATUS_FWP_TXN_IN_PROGRESS") => Ok(()),
            result => result,
        }
    }

    /// Applies a user-space verdict to a saved packet.
    ///
    /// A reauthorization marker is created only for an ALE indication that could
    /// not be pended. Reset the filters before consuming that marker so an IRQL
    /// failure cannot strand its packet after ownership has left the cache.
    fn inject_verdict_packet(&self, packet: Packet, blocked: bool) -> Result<(), String> {
        if packet.requires_reauthorization() {
            self.reset_filters_for_defer()?;
        }
        self.inject_packet(packet, blocked)
    }

    fn write_buffer(read_request: &mut ReadRequest, info: Info, read_stream: &mut ArrayHolder) {
        let bytes = info.as_bytes();
        let count = read_request.write(bytes);

        // Check if the full buffer was written.
        if count < bytes.len() {
            // Save the leftovers for later while the same stream transaction is
            // still exclusively owned by this read.
            read_stream.save(&bytes[count..]);
        }
    }

    /// Discards a partial record after every read admitted for the old owner has
    /// returned. A replacement handle must always begin at a record boundary.
    pub(crate) fn clear_read_leftover(&self) -> Result<(), String> {
        let mut read_stream = self
            .read_stream
            .lock()
            .map_err(|err| format!("failed to lock read stream: {}", err))?;
        read_stream.clear();
        Ok(())
    }

    /// Called when handle. Read is called from user-space.
    pub fn read(&self, read_request: &mut ReadRequest) -> NTSTATUS {
        // Keep one exclusive owner across the entire operation. Locking only the
        // individual load/save calls permits two overlapped reads to consume the
        // same logical stream concurrently and overwrite each other's fragment.
        let mut read_stream = match self.read_stream.lock() {
            Ok(read_stream) => read_stream,
            Err(err) => {
                err!("failed to lock read stream: {}", err);
                return read_request.fail(STATUS_INVALID_DEVICE_STATE);
            }
        };

        if let Some(data) = read_stream.load() {
            // There are leftovers from previous request.
            let count = read_request.write(&data);

            // Check if full command was written.
            if count < data.len() {
                // Save the leftovers for later.
                read_stream.save(&data[count..]);
            }
        } else {
            // Nothing left from before. Wait for the next record.
            match self.event_queue.wait_and_pop_cancellable() {
                Ok(info) => {
                    Self::write_buffer(read_request, info, &mut read_stream);
                }
                Err(ioqueue::Status::Timeout) => {
                    // Timeout. This will only trigger if pop function is called with timeout.
                    return read_request.timeout();
                }
                Err(ioqueue::Status::Cancelled) => {
                    // The owning file object was cleaned up while this
                    // synchronous wait was blocked.
                    return read_request.cancelled();
                }
                Err(err) => {
                    // Queue failed. Send EOF, to notify user-space. Usually happens on rundown.
                    err!("failed to pop value: {}", err);
                    return read_request.end_of_file();
                }
            }
        }

        // Check if we have more space. InfoType + data_size == 5 bytes
        while read_request.free_space() > 5 {
            match self.event_queue.pop() {
                Ok(info) => {
                    Self::write_buffer(read_request, info, &mut read_stream);
                }
                Err(_) => {
                    break;
                }
            }
        }
        read_request.complete()
    }

    /// Applies commands in buffer order until the write is exhausted or one is invalid.
    /// Commands completed before an error remain applied.
    pub(crate) fn write(
        &self,
        write_request: &WriteRequest,
    ) -> Result<(), CommandWriteError> {
        let mut remaining = write_request.get_buffer();
        if remaining.is_empty() {
            err!("Rejecting empty command write");
            return Err(CommandWriteError::new(STATUS_INVALID_PARAMETER, 0));
        }

        let mut offset = 0;
        while !remaining.is_empty() {
            let (command, payload, next) = match protocol::command::split_first_command(remaining) {
                Ok(command) => command,
                Err(CommandParseError::Empty) => {
                    err!("Missing command at byte {}", offset);
                    return Err(CommandWriteError::new(STATUS_INVALID_PARAMETER, offset));
                }
                Err(CommandParseError::UnknownType(command)) => {
                    err!("Unknown command number {} at byte {}", command, offset);
                    return Err(CommandWriteError::new(STATUS_INVALID_PARAMETER, offset));
                }
                Err(CommandParseError::TruncatedPayload {
                    command,
                    expected,
                    actual,
                }) => {
                    err!(
                        "Truncated {:?} command at byte {}: expected {} payload bytes, received {}",
                        command,
                        offset,
                        expected,
                        actual
                    );
                    return Err(CommandWriteError::new(STATUS_INVALID_PARAMETER, offset));
                }
            };

            if let Err(status) = self.apply_command(command, payload) {
                return Err(CommandWriteError::new(status, offset));
            }
            offset += 1 + payload.len();
            remaining = next;
        }

        Ok(())
    }

    fn apply_command(&self, command: CommandType, payload: &[u8]) -> Result<(), NTSTATUS> {
        match command {
            CommandType::Shutdown => {
                wdk::dbg!("Shutdown command");
                self.shutdown();
            }
            CommandType::Verdict => {
                let Some(verdict) = protocol::command::parse_verdict(payload) else {
                    // The length was checked above; keep this guard in case the
                    // parser and command table ever diverge.
                    err!("Failed to decode Verdict command");
                    return Err(STATUS_INVALID_PARAMETER);
                };
                let Some(action): Option<crate::connection::Verdict> =
                    FromPrimitive::from_u8(verdict.verdict)
                else {
                    // Validate the action before consuming this command's pending
                    // packet. Earlier commands from the same write remain applied.
                    err!("invalid verdict value: {}", verdict.verdict);
                    return Err(STATUS_INVALID_PARAMETER);
                };

                wdk::dbg!("Verdict command");
                // Received verdict decision for a specific connection.
                let packet = {
                    let mut packet_cache = self.packet_cache.write_lock();
                    packet_cache.pop_id(verdict.id)
                };
                if let Some(PendingPacket {
                    key,
                    mut packet,
                    connection_instance_id,
                }) = packet
                {
                    let _request_guard = PendingRequestGuard {
                        device: self,
                        id: verdict.id,
                    };
                    dbg!("Verdict received {}: {}", key, action);
                    // A connection verdict belongs to the exact cache instance that
                    // queued this packet. If endpoint/flow cleanup already ended it,
                    // never apply the decision to a tuple replacement. Packets that
                    // depend on the endpoint are completed as blocked; an outbound
                    // UDP network clone remains independently deliverable and is
                    // handled below as a packet-only decision.
                    let packet_survives_connection_end = packet.survives_connection_end(&key);
                    let redirect_info = if let Some(instance_id) = connection_instance_id {
                        match self.connection_cache.update_connection_instance(
                            key,
                            instance_id,
                            action,
                        ) {
                            Some(update) => update.redirect_info,
                            None if packet_survives_connection_end => {
                                // Endpoint closure does not revoke an outbound UDP
                                // datagram whose send already completed before this
                                // packet-layer decision. Apply the verdict to this
                                // clone only, without caching it against a tuple that
                                // may already belong to a replacement socket.
                                dbg!(
                                    "completing outbound UDP packet after connection instance {} ended: {}",
                                    instance_id,
                                    key
                                );
                                None
                            }
                            None => {
                                dbg!(
                                    "discarding stale verdict for ended connection instance {}: {}",
                                    instance_id,
                                    key
                                );
                                if let Err(err) = self.inject_packet(packet, true) {
                                    err!("failed to complete stale packet: {}", err);
                                }
                                return Ok(());
                            }
                        }
                    } else {
                        None
                    };

                    match action {
                        crate::connection::Verdict::Accept
                        | crate::connection::Verdict::PermanentAccept => {
                            if let Err(err) = self.inject_verdict_packet(packet, false) {
                                err!("failed to inject packet: {}", err);
                            } else {
                                dbg!("packet injected: {}", key);
                            }
                        }
                        crate::connection::Verdict::RedirectNameServer
                        | crate::connection::Verdict::RedirectTunnel
                        | crate::connection::Verdict::RedirectSplitTunnel => {
                            if let Some(redirect_info) = redirect_info {
                                // Never inject a clone when redirect validation or
                                // checksum reconstruction failed. The packet is dropped
                                // with this verdict instead of escaping unredirected.
                                match packet.redirect(redirect_info) {
                                    Ok(()) => {
                                        if let Err(err) = self.inject_verdict_packet(packet, false)
                                        {
                                            err!("failed to inject packet: {}", err);
                                        }
                                    }
                                    Err(err) => err!("failed to redirect packet: {}", err),
                                }
                            } else {
                                // The connection disappeared before its verdict was
                                // applied. Complete an ALE pend, if this is one, but
                                // do not inject a packet with no redirect state.
                                if let Err(err) = self.inject_verdict_packet(packet, true) {
                                    err!("failed to complete packet: {}", err);
                                }
                            }
                        }
                        _ => {
                            // Complete ALE operations without injecting their
                            // packet clone. Packet-layer clones are discarded.
                            if let Err(err) = self.inject_verdict_packet(packet, true) {
                                err!("failed to inject packet: {}", err);
                            }
                        }
                    }
                } else {
                    // Id was not in the packet cache.
                    let id = verdict.id;
                    err!("Verdict invalid id: {}", id);
                }
            }
            CommandType::UpdateV4 => {
                let Some(update) = protocol::command::parse_update_v4(payload) else {
                    err!("Failed to decode UpdateV4 command");
                    return Err(STATUS_INVALID_PARAMETER);
                };
                // Build the new action.
                if let Some(verdict) = FromPrimitive::from_u8(update.verdict) {
                    // Update with new action.
                    dbg!("Verdict update received {:?}: {}", update, verdict);
                    _ = self.connection_cache.update_connection(
                        Key {
                            protocol: IpProtocol::from(update.protocol),
                            local_address: IpAddress::Ipv4(Ipv4Address::from_bytes(
                                &update.local_address,
                            )),
                            local_port: update.local_port,
                            remote_address: IpAddress::Ipv4(Ipv4Address::from_bytes(
                                &update.remote_address,
                            )),
                            remote_port: update.remote_port,
                        },
                        verdict,
                    );
                    // Packet-layer lookups observe cache updates on the next packet.
                    // ALE-authorized flows need an explicit policy reauthorization.
                    if let Err(err) = self.reset_filters() {
                        err!("failed to reauthorize connections: {}", err);
                    }
                } else {
                    err!("invalid verdict value: {}", update.verdict);
                    return Err(STATUS_INVALID_PARAMETER);
                }
            }
            CommandType::UpdateV6 => {
                let Some(update) = protocol::command::parse_update_v6(payload) else {
                    err!("Failed to decode UpdateV6 command");
                    return Err(STATUS_INVALID_PARAMETER);
                };
                // Build the new action.
                if let Some(verdict) = FromPrimitive::from_u8(update.verdict) {
                    // Update with new action.
                    dbg!("Verdict update received {:?}: {}", update, verdict);
                    _ = self.connection_cache.update_connection(
                        Key {
                            protocol: IpProtocol::from(update.protocol),
                            local_address: IpAddress::Ipv6(Ipv6Address::from_bytes(
                                &update.local_address,
                            )),
                            local_port: update.local_port,
                            remote_address: IpAddress::Ipv6(Ipv6Address::from_bytes(
                                &update.remote_address,
                            )),
                            remote_port: update.remote_port,
                        },
                        verdict,
                    );
                    // Packet-layer lookups observe cache updates on the next packet.
                    // ALE-authorized flows need an explicit policy reauthorization.
                    if let Err(err) = self.reset_filters() {
                        err!("failed to reauthorize connections: {}", err);
                    }
                } else {
                    err!("invalid verdict value: {}", update.verdict);
                    return Err(STATUS_INVALID_PARAMETER);
                }
            }
            CommandType::ClearCache => {
                wdk::dbg!("ClearCache command");
                self.connection_cache.clear();
                {
                    let mut endpoint_cache = self.tcp_endpoint_cache.write_lock();
                    endpoint_cache.clear();
                }
                {
                    let mut endpoint_cache = self.udp_endpoint_cache.write_lock();
                    endpoint_cache.clear();
                }
                self.clean_udp_lifecycle_state();
                if let Err(err) = self.reset_filters() {
                    err!("failed to reset filters: {}", err);
                }
            }
            CommandType::GetLogs => {
                wdk::dbg!("GetLogs command");
                let lines_vec = logger::flush();
                for line in lines_vec {
                    let _ = self.event_queue.push(line);
                }
            }
            CommandType::GetBandwidthStats => {
                wdk::dbg!("GetBandwidthStats command");
                let stats = self.bandwidth_stats.get_all_updates_tcp_v4();
                if let Some(stats) = stats {
                    _ = self.event_queue.push(stats);
                }

                let stats = self.bandwidth_stats.get_all_updates_tcp_v6();
                if let Some(stats) = stats {
                    _ = self.event_queue.push(stats);
                }

                let stats = self.bandwidth_stats.get_all_updates_udp_v4();
                if let Some(stats) = stats {
                    _ = self.event_queue.push(stats);
                }

                let stats = self.bandwidth_stats.get_all_updates_udp_v6();
                if let Some(stats) = stats {
                    _ = self.event_queue.push(stats);
                }
            }
            CommandType::PrintMemoryStats => {
                // Run the bounded TCP maintenance before taking the snapshot so
                // memory statistics also reveal the post-timeout state. This is
                // intentionally limited to local unestablished generations.
                crate::ale_callouts::expire_unestablished_tcp_connections(self);
                // Snapshot each cache independently. All spin-lock guards must be
                // released before crate::err! allocates the userspace log records.
                let packet_cache_entries = {
                    let packet_cache = self.packet_cache.write_lock();
                    packet_cache.get_entries_count()
                };
                let (connection_v4_entries, connection_v6_entries) =
                    self.connection_cache.get_entries_counts();
                let (untracked_connection_v4_entries, untracked_connection_v6_entries) =
                    self.connection_cache.get_untracked_entries_counts();
                let (bandwidth_tcp_v4, bandwidth_tcp_v6, bandwidth_udp_v4, bandwidth_udp_v6) =
                    self.bandwidth_stats.get_entries_counts();
                let tcp_endpoint_entries = {
                    let endpoint_cache = self.tcp_endpoint_cache.read_lock();
                    endpoint_cache.get_entries_count()
                };
                let tcp_pending_closures = {
                    let closure_cache = self.tcp_closure_cache.read_lock();
                    closure_cache.get_entries_count()
                };
                let (udp_endpoint_entries, udp_endpoint_peers) = {
                    let endpoint_cache = self.udp_endpoint_cache.write_lock();
                    endpoint_cache.get_entries_counts()
                };
                let (udp_flow_entries, udp_flow_callbacks) =
                    self.udp_flow_cache.get_entries_counts();
                let icmp_echo_entries = {
                    let icmp_echo_cache = self.icmp_echo_cache.write_lock();
                    icmp_echo_cache.get_entries_count()
                };

                crate::err!("Packet cache: {} entries", packet_cache_entries);
                crate::err!(
                    "Connection cache: IPv4 {} entries, IPv6 {} entries",
                    connection_v4_entries,
                    connection_v6_entries
                );
                crate::err!(
                    "Untracked connection cache: IPv4 {} entries, IPv6 {} entries",
                    untracked_connection_v4_entries,
                    untracked_connection_v6_entries
                );
                crate::err!(
                    "BandwidthStats cache: TCPv4 {}, TCPv6 {}, UDPv4 {}, UDPv6 {} entries",
                    bandwidth_tcp_v4,
                    bandwidth_tcp_v6,
                    bandwidth_udp_v4,
                    bandwidth_udp_v6
                );
                crate::err!(
                    "TCP endpoint cache: {} entries, {} pending closures",
                    tcp_endpoint_entries,
                    tcp_pending_closures
                );
                crate::err!(
                    "UDP endpoint cache: {} endpoints, {} peers",
                    udp_endpoint_entries,
                    udp_endpoint_peers
                );
                crate::err!(
                    "UDP flow cache: {} entries, {} callbacks in progress",
                    udp_flow_entries,
                    udp_flow_callbacks
                );
                crate::err!("ICMP echo cache: {} entries", icmp_echo_entries);
            }
            CommandType::CleanEndedConnections => {
                wdk::dbg!("CleanEndedConnections command");
                self.connection_cache.clean_ended_connections();
                // An outbound packet-layer fallback has no native endpoint identity
                // and can therefore receive no exact closure callback. End it after
                // one inactive minute and emit the same lifecycle event as native WFP
                // teardown. Tracked and inbound connections remain exempt.
                crate::ale_callouts::expire_inactive_untracked_connections(self);
                crate::ale_callouts::expire_unestablished_tcp_connections(self);
                // Reconcile endpoint and flow bookkeeping for connection instances
                // that native lifecycle callbacks have already ended. Removing a WFP
                // callout context does not close the UDP socket or flow; it merely asks
                // WFP to return our allocation through flowDeleteFn.
                self.clean_udp_lifecycle_state();
                // Same intent for the ICMP echo table: an unanswered request is
                // state that is no longer needed. Expired entries only, so that
                // requests still in flight keep their process attribution.
                //
                // Doing it here also keeps the sweep off the packet path - the
                // only other one runs inside a callout at DISPATCH_LEVEL.
                let mut icmp_echo_cache = self.icmp_echo_cache.write_lock();
                icmp_echo_cache.clean_expired_entries();
            }
        }

        Ok(())
    }

    /// Removes one associated WFP flow context without terminating the socket or
    /// its network flow. WFP reclaims only the callout-owned bookkeeping and invokes
    /// `udp_flow_delete`, which frees the allocation and consumes the exact cache
    /// instance if it is still live.
    fn remove_udp_flow_context(&self, registration: UdpFlowRegistration) {
        match wdk::filter_engine::flow::remove_context(
            registration.flow_id,
            registration.layer_id,
            registration.callout_id,
        ) {
            Ok(wdk::filter_engine::flow::RemoveContextResult::Removed)
            | Ok(wdk::filter_engine::flow::RemoveContextResult::Pending) => {}
            Ok(wdk::filter_engine::flow::RemoveContextResult::AlreadyGone) => {
                // WFP has no association left and therefore cannot call
                // flowDeleteFn; reclaim the driver-owned allocation here.
                crate::ale_callouts::reclaim_udp_flow_context(
                    self,
                    registration.flow_context,
                    registration.connection_instance_id,
                );
            }
            Err(err) => {
                self.udp_flow_cache.retry_removal(
                    registration.flow_context,
                    registration.connection_instance_id,
                );
                crate::err!(
                    "failed to remove UDP flow context {}: {}",
                    registration.flow_id,
                    err
                );
            }
        }
    }

    /// Reconciles endpoint/flow bookkeeping with the live connection cache.
    ///
    /// Snapshot order is intentional: endpoint and flow state are captured before
    /// live connections. Any association created concurrently is therefore visible
    /// in the later live snapshot or remains untouched until the next pass.
    fn clean_udp_lifecycle_state(&self) {
        let now = wdk::utils::get_monotonic_timestamp_ms();
        let endpoint_instances = self.udp_endpoint_cache.write_lock().instance_ids();
        let flow_candidates = self.udp_flow_cache.removal_candidates();
        let live_instances = self.connection_cache.live_udp_instance_ids();

        let stale_endpoint_instances = endpoint_instances
            .into_iter()
            .filter(|instance_id| live_instances.binary_search(instance_id).is_err())
            .collect();
        {
            let mut endpoint_cache = self.udp_endpoint_cache.write_lock();
            let _ = endpoint_cache.remove_instances(stale_endpoint_instances, now);
        }

        for (flow_context, connection_instance_id) in flow_candidates {
            if live_instances
                .binary_search(&connection_instance_id)
                .is_ok()
            {
                continue;
            }
            if let Some(registration) = self
                .udp_flow_cache
                .claim_removal(flow_context, connection_instance_id)
            {
                self.remove_udp_flow_context(registration);
            }
        }
    }

    /// Stops new flow-context associations before the remaining asynchronous ALE
    /// operations are completed. Classify admission must already be closed and
    /// drained by the caller.
    pub fn begin_unload(&self) {
        self.udp_flow_cache.start_shutdown();
    }

    /// Drains every context still owned by WFP, unregisters all runtime callouts,
    /// and destroys injection handles before the Device or its Callout allocations
    /// can be released.
    ///
    /// `FwpsCalloutUnregisterById0` returns STATUS_DEVICE_BUSY while any context
    /// remains associated. Keep the global Device pointer valid until the resulting
    /// flowDeleteFn callbacks have drained this cache and WFP confirms that every
    /// runtime registration is gone. Injection-handle destruction is last because
    /// WFP waits for all pending injections before each handle is destroyed.
    pub fn prepare_unload(&self) {
        self.drain_udp_flow_contexts();
        self.unregister_wfp_state();
        self.destroy_injection_handles();
    }

    fn destroy_injection_handles(&self) {
        loop {
            match self.injector.destroy() {
                Ok(()) => return,
                Err(err) => {
                    // Keep the Injector alive and retry. A failed destroy leaves
                    // its handle published so unload cannot accidentally release
                    // the Device while WFP may still reference it.
                    crate::err!("failed to destroy WFP injection handles: {}", err);
                }
            }
            wdk::utils::sleep_ms(1);
        }
    }

    fn drain_udp_flow_contexts(&self) {
        while !self.udp_flow_cache.is_drained() {
            for registration in self.udp_flow_cache.pending_removals() {
                self.remove_udp_flow_context(registration);
            }

            if !self.udp_flow_cache.is_drained() {
                // STATUS_PENDING completes through flowDeleteFn after any active
                // classification returns. Each association is requested once unless
                // the WFP removal call itself failed.
                wdk::utils::sleep_ms(1);
            }
        }
    }

    fn unregister_wfp_state(&self) {
        loop {
            let result = match self.filter_engine.lock() {
                Ok(mut filter_engine) => filter_engine.unregister_all(),
                Err(err) => Err(format!("failed to acquire filter engine: {}", err)),
            };

            match result {
                Ok(UnregisterCalloutsResult::Complete) => return,
                Ok(UnregisterCalloutsResult::Busy) => {
                    self.drain_udp_flow_contexts();
                    crate::err!(
                        "WFP callout unregister is busy; retrying after flow-context removal"
                    );
                }
                Err(err) => {
                    crate::err!("failed to tear down WFP state: {}", err);
                }
            }
            wdk::utils::sleep_ms(1);
        }
    }

    /// Permanently stops classification for this driver instance and resolves all
    /// pending user-space decisions. This runs from PASSIVE_LEVEL dispatch or
    /// DriverUnload and is idempotent.
    pub fn shutdown(&self) {
        // A user-mode shutdown command can arrive before service-driven unload.
        // Close callback admission here as well as in DriverUnload so no classify
        // can enqueue a new packet after the cache and event queue are drained.
        wdk::callback_barrier::CALLBACK_BARRIER.close_classify_and_wait();
        self.begin_unload();

        // KeRundownQueue cannot race a thread blocked in KeRemoveQueue. Close
        // read admission, wake the bounded waits, and drain their dispatch guards
        // before reclaiming queued entries. The queue itself is reclaimed by
        // Device::drop after DriverUnload has closed all dispatch admission. Do
        // not run it down here: a user-issued shutdown can race CLEANUP followed
        // by a new CREATE, and that new session may reopen read admission while
        // this dispatch is still finishing.
        crate::entry::close_and_wait_for_reads(self);

        // Resolve all pending packets. This is important for proper driver unload.
        let pending_packets = {
            let mut packet_cache = self.packet_cache.write_lock();
            packet_cache.pop_all()
        };
        for el in pending_packets {
            let id = el.id();
            let pending = el.value;
            // Set any verdict. Driver will unload after that and the filter will not be active.
            if let Some(instance_id) = pending.connection_instance_id {
                _ = self.connection_cache.update_connection_instance(
                    pending.key,
                    instance_id,
                    crate::connection::Verdict::PermanentBlock,
                );
            }
            _ = self.inject_packet(pending.packet, true); // Complete ALE pends and discard all packet clones.
            self.finish_pending_request(id);
        }
        self.drain_tcp_endpoint_closures();

        {
            let mut endpoint_cache = self.tcp_endpoint_cache.write_lock();
            endpoint_cache.clear();
        }
        let mut endpoint_cache = self.udp_endpoint_cache.write_lock();
        endpoint_cache.clear();
    }

    /// Publishes a packet decision request only while its exact connection
    /// generation remains live.
    ///
    /// `with_live_connection_instance_matching` holds the connection-map read guard
    /// through both pending-cache insertion and event-queue insertion. Every native
    /// lifecycle end needs the corresponding map write guard, so publication is
    /// ordered wholly before END or rejected wholly after the connection was ended.
    /// The rejected packet is returned for fail-closed completion outside all locks.
    pub(crate) fn publish_pending_packet(
        &self,
        value: (Key, Packet),
        connection_instance_id: Option<u64>,
        process_id: u64,
        direction: crate::connection::Direction,
        ale_layer: bool,
    ) -> Option<PendingPacket> {
        let pending = PendingPacket {
            key: value.0,
            packet: value.1,
            connection_instance_id,
        };

        if let Some(instance_id) = connection_instance_id {
            let key = pending.key;
            return self
                .connection_cache
                .with_live_connection_instance_matching(
                    &key,
                    instance_id,
                    pending,
                    |_, pending| {
                        self.enqueue_pending_packet(
                            pending,
                            process_id,
                            direction,
                            ale_layer,
                        );
                    },
                )
                .err();
        }

        self.enqueue_pending_packet(pending, process_id, direction, ale_layer);
        None
    }

    fn enqueue_pending_packet(
        &self,
        pending: PendingPacket,
        process_id: u64,
        direction: crate::connection::Direction,
        ale_layer: bool,
    ) {
        let key = pending.key;
        let connection_instance_id = pending.connection_instance_id;
        let infos = {
            // Connection publication already holds the family map's read guard.
            // Take closure state before the packet cache, matching endpoint-close
            // and verdict-completion lock order, so a new request cannot escape an
            // existing closure waiter. `IdCache::push` expands a multi-NET_BUFFER
            // packet-layer indication into one independently decided request per
            // packet while both locks remain held.
            let mut closure_cache = self.tcp_closure_cache.write_lock();
            let queued = {
                let mut packet_cache = self.packet_cache.write_lock();
                packet_cache.push(
                    (pending.key, pending.packet),
                    connection_instance_id,
                    process_id,
                    direction,
                    ale_layer,
                )
            };
            for (id, _) in &queued {
                closure_cache.add_request(*id, &key, connection_instance_id);
            }
            queued
        };
        for (_, info) in infos {
            let _ = self.event_queue.push(info);
        }
    }

    /// Pends one native TCP endpoint closure when packet decisions for this flow
    /// were already published. The closure lock is held while the packet-cache
    /// snapshot is taken and the native classification is pended, so a concurrently
    /// finishing verdict either disappears before the snapshot or observes the
    /// inserted waiter afterwards. If there is nothing to wait for, the exact
    /// connection generation is ended under the same family-map write guard.
    pub(crate) fn defer_tcp_endpoint_closure(
        &self,
        endpoint: TcpEndpointConnection,
        process_id: u64,
        data: &mut CalloutData,
    ) -> Result<(), String> {
        let defer_end = || {
            let mut closure_cache = self.tcp_closure_cache.write_lock();
            let request_ids = {
                let packet_cache = self.packet_cache.write_lock();
                packet_cache.tcp_endpoint_request_ids(&endpoint.key, endpoint.instance_id)
            };
            if request_ids.is_empty() {
                return Ok(false);
            }

            let classify = data.pend_classify()?;
            let closure = PendingTcpClosure::new(endpoint, process_id, classify, request_ids);
            let inserted = closure_cache.insert(closure);
            drop(closure_cache);
            match inserted {
                None => Ok(true),
                Some(duplicate) => {
                    duplicate.classify.complete();
                    Err(alloc::format!(
                        "duplicate TCP endpoint closure for connection instance {}",
                        endpoint.instance_id
                    ))
                }
            }
        };

        if endpoint.key.is_ipv6() {
            if let Some(connection) = self.connection_cache.end_connection_instance_v6_unless(
                endpoint.key,
                endpoint.instance_id,
                defer_end,
            )? {
                self.retire_pending_connection_instances(&[endpoint.instance_id]);
                crate::ale_callouts::emit_connection_end_v6(self, connection, process_id);
            }
        } else if let Some(connection) = self.connection_cache.end_connection_instance_v4_unless(
            endpoint.key,
            endpoint.instance_id,
            defer_end,
        )? {
            self.retire_pending_connection_instances(&[endpoint.instance_id]);
            crate::ale_callouts::emit_connection_end_v4(self, connection, process_id);
        }
        Ok(())
    }

    /// Marks one claimed verdict as complete and releases every endpoint closure
    /// whose current request set is now empty.
    fn finish_pending_request(&self, id: u64) {
        let ready = {
            let mut closure_cache = self.tcp_closure_cache.write_lock();
            {
                let mut packet_cache = self.packet_cache.write_lock();
                packet_cache.finish_id(id);
            }
            closure_cache.finish_request(id)
        };

        for endpoint in ready {
            self.complete_ready_tcp_endpoint_closure(endpoint);
        }
    }

    /// Claims a ready closure and ends its exact connection generation while the
    /// family map is write-locked. Packet publication holds the corresponding read
    /// lock through closure-cache insertion, so it cannot add an untracked request
    /// between the final empty check and the lifecycle end.
    fn complete_ready_tcp_endpoint_closure(&self, endpoint: TcpEndpointConnection) {
        if endpoint.key.is_ipv6() {
            let completed = self.connection_cache.end_connection_instance_v6_after(
                endpoint.key,
                endpoint.instance_id,
                || {
                    let mut closure_cache = self.tcp_closure_cache.write_lock();
                    closure_cache.take_ready(endpoint.instance_id)
                },
            );
            if let Some((connection, closure)) = completed {
                if let Some(connection) = connection {
                    self.retire_pending_connection_instances(&[endpoint.instance_id]);
                    crate::ale_callouts::emit_connection_end_v6(
                        self,
                        connection,
                        closure.process_id,
                    );
                }
                closure.classify.complete();
            }
            return;
        }

        let completed = self.connection_cache.end_connection_instance_v4_after(
            endpoint.key,
            endpoint.instance_id,
            || {
                let mut closure_cache = self.tcp_closure_cache.write_lock();
                closure_cache.take_ready(endpoint.instance_id)
            },
        );
        if let Some((connection, closure)) = completed {
            if let Some(connection) = connection {
                self.retire_pending_connection_instances(&[endpoint.instance_id]);
                crate::ale_callouts::emit_connection_end_v4(self, connection, closure.process_id);
            }
            closure.classify.complete();
        }
    }

    fn complete_tcp_endpoint_closure(&self, closure: PendingTcpClosure) {
        crate::ale_callouts::end_tcp_connection(self, closure.endpoint, closure.process_id);
        closure.classify.complete();
    }

    fn drain_tcp_endpoint_closures(&self) {
        let closures = {
            let mut closure_cache = self.tcp_closure_cache.write_lock();
            closure_cache.drain()
        };
        for closure in closures {
            self.complete_tcp_endpoint_closure(closure);
        }
    }

    /// Retires queued decisions for connection instances whose native lifetime
    /// has ended. Outbound UDP network packets remain pending as packet-only
    /// requests; every other packet is removed under the cache lock and any ALE
    /// operation is completed as blocked after the lock has been released.
    pub(crate) fn retire_pending_connection_instances(&self, instance_ids: &[u64]) {
        if instance_ids.len() <= 1 {
            if instance_ids.first().copied().unwrap_or(0) != 0 {
                self.retire_sorted_pending_connection_instances(instance_ids);
            }
            return;
        }

        self.retire_pending_connection_instances_owned(instance_ids.to_vec());
    }

    /// Owned-input variant used by lifecycle paths that already collected IDs.
    /// Sorting in place avoids cloning their temporary vector a second time.
    pub(crate) fn retire_pending_connection_instances_owned(&self, mut instance_ids: Vec<u64>) {
        instance_ids.retain(|instance_id| *instance_id != 0);
        instance_ids.sort_unstable();
        instance_ids.dedup();
        if instance_ids.is_empty() {
            return;
        }

        self.retire_sorted_pending_connection_instances(&instance_ids);
    }

    fn retire_sorted_pending_connection_instances(&self, instance_ids: &[u64]) {
        let pending_packets = {
            let mut packet_cache = self.packet_cache.write_lock();
            packet_cache.retire_connection_instances(instance_ids)
        };
        for entry in pending_packets {
            let id = entry.id();
            let pending = entry.value;
            if let Err(err) = self.inject_packet(pending.packet, true) {
                crate::err!(
                    "failed to discard pending packet for ended connection {}: {}",
                    pending.key,
                    err
                );
            }
            self.finish_pending_request(id);
        }
    }

    pub fn inject_packet(&self, packet: Packet, blocked: bool) -> Result<(), String> {
        match packet {
            Packet::Network(nbl, inject_info) => {
                if !blocked {
                    self.injector.inject_net_buffer_list(nbl, inject_info)?;
                }
                Ok(())
            }
            Packet::NetworkBatch(nbls, inject_info) => {
                if !blocked {
                    for nbl in nbls {
                        self.injector.inject_net_buffer_list(nbl, inject_info)?;
                    }
                }
                Ok(())
            }
            Packet::AleLayer(defer) => {
                // Initial ALE pends can be completed at the callback's IRQL and
                // do not need the FilterEngine. Reauthorization is performed by
                // the PASSIVE_LEVEL verdict path before this packet reaches here.
                // FwpsInjectTransportReceiveAsync requires the pended ALE
                // operation to be completed first, with this same cloned NBL.
                // Keep completion before Injector's TCP DPC handoff.
                let packet_list = defer.complete(!blocked)?;
                if !blocked {
                    if let Some(packet_list) = packet_list {
                        self.injector.inject_ale_packet(packet_list)?;
                    }
                }

                Ok(())
            }
        }
    }
}

impl Drop for Device {
    fn drop(&mut self) {
        // Normal unload closes this barrier before reaching Drop.  Keep the
        // guard here as a last line of defense for an initialization/error path
        // that destroys Device through a different route.
        wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
        _ = logger::flush();
        // dbg!("Device Context drop called.");
    }
}
