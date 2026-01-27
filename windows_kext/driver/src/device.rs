use alloc::string::String;
use num_traits::FromPrimitive;
use protocol::{command::CommandType, info::Info};
use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address, Ipv6Address};
use wdk::{
    driver::Driver,
    filter_engine::{
        FilterEngine, 
        callout::FunctionType,
        callout_data::ClassifyDefer, 
        net_buffer::{NetBufferList, NetworkAllocator}, 
        packet::{InjectInfo, Injector}, 
        redirect::{Redirector}
    },
    ioqueue::{self, IOQueue},
    irp_helpers::{ReadRequest, WriteRequest},
};

use crate::{
    ale_redirect_callouts, array_holder::ArrayHolder, bandwidth::Bandwidth, callouts, 
    connection_cache::ConnectionCache, connection_map::Key, dbg, err, id_cache::IdCache, 
    id_cache_generic::GenericIdCache, logger, packet_util::Redirect
};

pub enum Packet {
    PacketLayer(NetBufferList, InjectInfo),
    AleLayer(ClassifyDefer),
}

// Device Context
pub struct Device {
    pub(crate) filter_engine: FilterEngine,
    pub(crate) read_leftover: ArrayHolder,
    pub(crate) event_queue: IOQueue<Info>,          // Queue for events to user-space
    pub(crate) packet_cache: IdCache,               // Cache of pending packets waiting for verdict
    pub(crate) connection_cache: ConnectionCache,   // Cache of connections and their verdicts
    pub(crate) injector: Injector,
    pub(crate) network_allocator: NetworkAllocator,
    pub(crate) bandwidth_stats: Bandwidth,
    
    // Split-tunnel support (Redirection)
    pub(crate) redirector: Redirector,
    pub(crate) redirect_cache: GenericIdCache<ale_redirect_callouts::PendedRedirect>, // Cache of pending redirects
}

// Sub-Layer GUID for Portmaster. Also used as provider GUID, when needed.
const PORTMASTER_SUBLAYER_GUID: u128 = 0x7dab1057_8e2b_40c4_9b85_693e381d7896;

impl Device {
    /// Initialize all members of the device. Memory is handled by windows.
    /// Make sure everything is initialized here.
    pub fn new(driver: &Driver) -> Result<Self, String> {
        let redirector = match Redirector::new(PORTMASTER_SUBLAYER_GUID) {
            Ok(result) => result,
            Err(err) => return Err(alloc::format!("Failed to create redirect handle: {}", err)),            
        };

        let mut filter_engine =
            match FilterEngine::new(driver, PORTMASTER_SUBLAYER_GUID) {
                Ok(fe) => fe,
                Err(err) => return Err(alloc::format!("filter engine error: {}", err)),
            };

        filter_engine.commit(callouts::get_callout_vec())?;

        Ok(Self {
            filter_engine,
            read_leftover: ArrayHolder::default(),
            event_queue: IOQueue::new(),
            packet_cache: IdCache::new(),
            connection_cache: ConnectionCache::new(),
            injector: Injector::new(),
            network_allocator: NetworkAllocator::new(),
            bandwidth_stats: Bandwidth::new(),

            redirector,
            redirect_cache: GenericIdCache::new(),
        })
    }

    /// Cleanup is called just before drop.
    // pub fn cleanup(&mut self) {}

    fn write_buffer(&mut self, read_request: &mut ReadRequest, info: Info) {
        let bytes = info.as_bytes();
        let count = read_request.write(bytes);

        // Check if the full buffer was written.
        if count < bytes.len() {
            // Save the leftovers for later.
            self.read_leftover.save(&bytes[count..]);
        }
    }

    /// Called when handle. Read is called from user-space.
    pub fn read(&mut self, read_request: &mut ReadRequest) {
        if let Some(data) = self.read_leftover.load() {
            // There are leftovers from previous request.
            let count = read_request.write(&data);

            // Check if full command was written.
            if count < data.len() {
                // Save the leftovers for later.
                self.read_leftover.save(&data[count..]);
            }
        } else {
            // Noting left from before. Wait for next commands.
            match self.event_queue.wait_and_pop() {
                Ok(info) => {
                    self.write_buffer(read_request, info);
                }
                Err(ioqueue::Status::Timeout) => {
                    // Timeout. This will only trigger if pop function is called with timeout.
                    read_request.timeout();
                    return;
                }
                Err(err) => {
                    // Queue failed. Send EOF, to notify user-space. Usually happens on rundown.
                    err!("failed to pop value: {}", err);
                    read_request.end_of_file();
                    return;
                }
            }
        }

        // Check if we have more space. InfoType + data_size == 5 bytes
        while read_request.free_space() > 5 {
            match self.event_queue.pop() {
                Ok(info) => {
                    self.write_buffer(read_request, info);
                }
                Err(_) => {
                    break;
                }
            }
        }
        read_request.complete();
    }

    // Called when handle.Write is called from user-space.
    pub fn write(&mut self, write_request: &mut WriteRequest) {
        // Try parsing the command.
        let mut buffer = write_request.get_buffer();
        let command = protocol::command::parse_type(buffer);
        let Some(command) = command else {
            err!("Unknown command number: {}", buffer[0]);
            return;
        };
        buffer = &buffer[1..];

        let mut _classify_defer = None;

        match command {
            CommandType::Shutdown => {
                wdk::dbg!("Shutdown command");
                self.shutdown();
            }
            CommandType::Verdict => {
                let verdict = protocol::command::parse_verdict(buffer);
                wdk::dbg!("Verdict command");
                // Received verdict decision for a specific connection.
                if let Some((key, mut packet)) = self.packet_cache.pop_id(verdict.id) {
                    if let Some(verdict) = FromPrimitive::from_u8(verdict.verdict) {
                        dbg!("Verdict received {}: {}", key, verdict);
                        // Add verdict in the cache.
                        let redirect_info = self.connection_cache.update_connection(key, verdict);

                        // if verdict.is_permanent() {
                        //     dbg!(self.logger, "resetting filters {}: {}", key, verdict);
                        //     _ = self.filter_engine.reset_all_filters();
                        // }

                        match verdict {
                            crate::connection::Verdict::Accept
                            | crate::connection::Verdict::PermanentAccept => {
                                if let Err(err) = self.inject_packet(packet, false) {
                                    err!("failed to inject packet: {}", err);
                                } else {
                                    dbg!("packet injected: {}", key);
                                }
                            }
                            crate::connection::Verdict::RedirectNameServer
                            | crate::connection::Verdict::RedirectTunnel => {
                                if let Some(redirect_info) = redirect_info {
                                    // Will not redirect packets from ALE layer
                                    if let Err(err) = packet.redirect(redirect_info) {
                                        err!("failed to redirect packet: {}", err);
                                    }
                                    if let Err(err) = self.inject_packet(packet, false) {
                                        err!("failed to inject packet: {}", err);
                                    }
                                }
                            }
                            _ => {
                                // Inject only ALE layer. This will trigger proper block/drop.
                                // Packet layer just drop the packet.
                                if let Err(err) = self.inject_packet(packet, true) {
                                    err!("failed to inject packet: {}", err);
                                }
                            }
                        }
                    };
                } else {
                    // Id was not in the packet cache.
                    let id = verdict.id;
                    err!("Verdict invalid id: {}", id);
                }
            }
            CommandType::UpdateV4 => {
                let update = protocol::command::parse_update_v4(buffer);
                // Build the new action.
                if let Some(verdict) = FromPrimitive::from_u8(update.verdict) {
                    // Update with new action.
                    dbg!("Verdict update received {:?}: {}", update, verdict);
                    _classify_defer = self.connection_cache.update_connection(
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
                } else {
                    err!("invalid verdict value: {}", update.verdict);
                }
            }
            CommandType::UpdateV6 => {
                let update = protocol::command::parse_update_v6(buffer);
                // Build the new action.
                if let Some(verdict) = FromPrimitive::from_u8(update.verdict) {
                    // Update with new action.
                    dbg!("Verdict update received {:?}: {}", update, verdict);
                    _classify_defer = self.connection_cache.update_connection(
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
                } else {
                    err!("invalid verdict value: {}", update.verdict);
                }
            }
            CommandType::ClearCache => {
                wdk::dbg!("ClearCache command");
                self.connection_cache.clear();
                if let Err(err) = self.filter_engine.reset_all_filters() {
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
                // Getting the information takes a long time and interferes with the callouts causing the device to crash.
                // TODO(vladimir): Make more optimized version
                // info!(
                //     "Packet cache: {} entries",
                //     self.packet_cache.get_entries_count()
                // );
                // info!(
                //     "BandwidthStats cache: {} entries",
                //     self.bandwidth_stats.get_entries_count()
                // );
                // info!(
                //     "Connection cache: {} entries\n {}",
                //     self.connection_cache.get_entries_count(),
                //     self.connection_cache.get_full_cache_info()
                // );
            }
            CommandType::CleanEndedConnections => {
                wdk::dbg!("CleanEndedConnections command");
                self.connection_cache.clean_ended_connections();
            }
            CommandType::RedirectV4 => {
                let redirect = protocol::command::parse_redirect_v4(buffer);
                wdk::dbg!("RedirectV4 command: {:?}", redirect);

                // Pop the pended redirect from cache
                if let Some(pended) = self.redirect_cache.pop_id(redirect.id) {
                    let new_local_ip = if redirect.redirect != 0 {
                        Some(IpAddress::Ipv4(Ipv4Address::from_bytes(&redirect.local_address)))
                    } else {                        
                        None // No redirect - permit without modification
                    };
                    
                    // Complete the pended redirect operation                    
                    dbg!("Completing BIND redirect for {:?}", redirect);
                    let result = self.redirector.complete_redirect(
                            pended.pend_redirect_result,
                            new_local_ip);
                    
                    if let Err(e) = result {
                        err!("Failed to complete redirect: {:#x}", e);
                    } else {
                        dbg!("Redirect completed successfully for {:?}", redirect);
                    }
                } else {
                    err!("RedirectV4 invalid id: {:?}", redirect);
                }
            }
            CommandType::RedirectV6 => {
                let redirect = protocol::command::parse_redirect_v6(buffer);
                wdk::dbg!("RedirectV6 command: {:?}", redirect);
                
                // Pop the pended redirect from cache
                if let Some(pended) = self.redirect_cache.pop_id(redirect.id) {
                    let new_local_ip = if redirect.redirect != 0 {
                        Some(IpAddress::Ipv6(Ipv6Address::from_bytes(&redirect.local_address)))
                    } else {                        
                        None // No redirect - permit without modification
                    };
                    
                    // Complete the pended redirect operation
                    let result = self.redirector.complete_redirect(
                        pended.pend_redirect_result,
                        new_local_ip);
                    
                    if let Err(e) = result {
                        err!("Failed to complete redirect: {:#x}", e);
                    } else {
                        dbg!("Redirect completed successfully for {:?}", redirect);
                    }
                } else {
                    err!("RedirectV6 invalid id: {:?}", redirect);
                }
            }
            CommandType::EnableSplitTunnel => {
                wdk::dbg!("EnableSplitTunnel command");
                if let Err(err) = self.filter_engine.register_filters(FunctionType::Redirect) {
                    err!("failed to enable split tunnel filters: {}", err);
                } else {
                    dbg!("Split tunnel filters enabled successfully");
                }
                self.connection_cache.clear();
            }
            CommandType::DisableSplitTunnel => {
                wdk::dbg!("DisableSplitTunnel command");
                if let Err(err) = self.filter_engine.unregister_filters(FunctionType::Redirect) {
                    err!("failed to disable split tunnel filters: {}", err);
                } else {
                    dbg!("Split tunnel filters disabled successfully");
                }
                self.connection_cache.clear();
            }
        }
    }

    pub fn shutdown(&mut self) {
        // End blocking operations from the queue. This will end pending read requests.
        self.event_queue.rundown();

		// Resolve all pending packets. This is important for proper driver unload.
        let pending_packets = self.packet_cache.pop_all();
        for el in pending_packets {
            let key = el.value.0;
            let packet = el.value.1;
            // Set any verdict. Driver will unload after that and the filter will not be active.
            _ = self
                .connection_cache
                .update_connection(key, crate::connection::Verdict::PermanentBlock);
            _ = self.inject_packet(packet, true); // Blocked must be set, so it only handles the ALE layer.
        }

        // Resolve all pending redirects. This is important for proper driver unload.
        let pending_redirects = self.redirect_cache.pop_all();
        for el in pending_redirects {
            let pended = el.value;
            // Complete without redirect (permit) to release the pended operation
            let result =self.redirector.complete_redirect(
                    pended.pend_redirect_result,
                    None, // No redirect on shutdown - just permit
                );            
            if let Err(e) = result {
                err!("Failed to complete redirect on shutdown: {:#x}", e);
            }
        }
    }

    pub fn inject_packet(&mut self, packet: Packet, blocked: bool) -> Result<(), String> {
        match packet {
            Packet::PacketLayer(nbl, inject_info) => {
                if !blocked {
                    self.injector.inject_net_buffer_list(nbl, inject_info)
                } else {
                    Ok(())
                }
            }
            Packet::AleLayer(defer) => {
                let packet_list = defer.complete(&mut self.filter_engine)?;
                if let Some(packet_list) = packet_list {
                    self.injector.inject_packet_list_transport(packet_list)?;
                }

                Ok(())
            }
        }
    }
}

impl Drop for Device {
    fn drop(&mut self) {
        _ = logger::flush();
        // dbg!("Device Context drop called.");
    }
}
