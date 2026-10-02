use alloc::{
    boxed::Box,
    format,
    string::{String, ToString},
};
use core::{
    cell::UnsafeCell,
    ffi::c_void,
    marker::PhantomPinned,
    mem::MaybeUninit,
    pin::Pin,
    sync::atomic::{AtomicBool, AtomicPtr, Ordering},
};
use windows_sys::{
    Wdk::{
        Foundation::KDPC,
        System::SystemServices::{
            ExAcquireRundownProtection, ExInitializeRundownProtection, ExReleaseRundownProtection,
            ExRundownCompleted, ExWaitForRundownProtectionRelease, EX_RUNDOWN_REF,
            EX_RUNDOWN_REF_0,
        },
    },
    Win32::{
        Foundation::{BOOLEAN, INVALID_HANDLE_VALUE, NTSTATUS, STATUS_SUCCESS},
        Networking::WinSock::{AF_INET, AF_INET6, AF_UNSPEC, SCOPE_ID, SCOPE_ID_0},
        System::Kernel::{COMPARTMENT_ID, UNSPECIFIED_COMPARTMENT_ID},
    },
};

use crate::{
    ffi::{
        FwpsInjectNetworkReceiveAsync0, FwpsInjectNetworkSendAsync0,
        FwpsInjectTransportReceiveAsync0, FwpsInjectTransportSendAsync1,
        FwpsInjectionHandleCreate0, FwpsInjectionHandleDestroy0, FwpsQueryPacketInjectionState0,
        FWPS_INJECTION_TYPE_NETWORK, FWPS_INJECTION_TYPE_TRANSPORT, FWPS_PACKET_INJECTION_STATE,
        FWPS_TRANSPORT_SEND_PARAMS1, NET_BUFFER_LIST,
    },
    utils::check_ntstatus,
};

use super::{callout_data::CalloutData, net_buffer::NetBufferList};

type TransportInjectDeferredRoutine = unsafe extern "system" fn(
    dpc: *mut KDPC,
    deferred_context: *mut c_void,
    system_argument_1: *mut c_void,
    system_argument_2: *mut c_void,
);

extern "system" {
    fn KeInitializeDpc(
        dpc: *mut KDPC,
        deferred_routine: TransportInjectDeferredRoutine,
        deferred_context: *mut c_void,
    );
    fn KeInsertQueueDpc(
        dpc: *mut KDPC,
        system_argument_1: *mut c_void,
        system_argument_2: *mut c_void,
    ) -> BOOLEAN;
}

// The generated KDPC is allocated in nonpaged pool and initialized in place.
// An accidental binding-layout change would otherwise let KeInitializeDpc
// overwrite the neighboring work-item fields.
#[cfg(target_pointer_width = "64")]
const _: () = {
    assert!(core::mem::size_of::<KDPC>() == 64);
    assert!(core::mem::align_of::<KDPC>() == 8);
};

/// Origin reported by WFP for a packet queried with one of this driver's
/// injection handles.
///
/// `InjectedBySelf` also covers packets previously injected by the same handle:
/// both must bypass the owning callout to prevent a reinjection loop. `Unknown`
/// keeps future native enum values distinct from packets that WFP explicitly says
/// were not injected.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PacketInjectionOrigin {
    NotInjected,
    InjectedBySelf,
    InjectedByOther,
    Unknown,
}

impl PacketInjectionOrigin {
    pub fn is_self_injected(self) -> bool {
        matches!(self, Self::InjectedBySelf)
    }

    pub fn is_injected_by_other(self) -> bool {
        matches!(self, Self::InjectedByOther)
    }

    pub fn is_injected(self) -> bool {
        matches!(self, Self::InjectedBySelf | Self::InjectedByOther)
    }
}

fn packet_injection_origin(state: FWPS_PACKET_INJECTION_STATE) -> PacketInjectionOrigin {
    match state {
        FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_NOT_INJECTED => {
            PacketInjectionOrigin::NotInjected
        }
        FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_SELF
        | FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_PREVIOUSLY_INJECTED_BY_SELF => {
            PacketInjectionOrigin::InjectedBySelf
        }
        FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_OTHER => {
            PacketInjectionOrigin::InjectedByOther
        }
        FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTION_STATE_MAX => {
            PacketInjectionOrigin::Unknown
        }
        _ => PacketInjectionOrigin::Unknown,
    }
}

/// Upper-layer protocol carried by an ALE transport packet clone.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportProtocol {
    Tcp,
    Udp,
}

impl TransportProtocol {
    fn requires_dpc(self) -> bool {
        matches!(self, Self::Tcp)
    }

    fn uses_network_send_for_ale(self, inbound: bool, loopback: bool) -> bool {
        matches!(self, Self::Tcp | Self::Udp) && inbound && loopback
    }
}

/// Asynchronous WFP injection API whose final completion failed.
#[repr(u8)]
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum InjectionPath {
    NetworkReceive,
    NetworkSend,
    TransportReceive,
    TransportSend,
}

impl InjectionPath {
    fn for_network(inbound: bool, loopback: bool) -> Self {
        if inbound && !loopback {
            Self::NetworkReceive
        } else {
            Self::NetworkSend
        }
    }

    fn for_transport(inbound: bool) -> Self {
        if inbound {
            Self::TransportReceive
        } else {
            Self::TransportSend
        }
    }

    pub const fn as_str(self) -> &'static str {
        match self {
            Self::NetworkReceive => "network-receive",
            Self::NetworkSend => "network-send",
            Self::TransportReceive => "transport-receive",
            Self::TransportSend => "transport-send",
        }
    }
}

/// Reports a negative final `NET_BUFFER_LIST::Status` from WFP.
///
/// WFP may invoke this function at any IRQL up to `DISPATCH_LEVEL`. The callback
/// must use only nonpaged code/data, must not block, and must remain callable until
/// [`Injector::destroy`] has returned after draining all injection completions.
pub type InjectionFailureCallback = unsafe fn(InjectionPath, NTSTATUS);

#[derive(Clone, Copy)]
struct InjectionCompletion {
    path: InjectionPath,
    failure_callback: Option<InjectionFailureCallback>,
}

impl InjectionCompletion {
    fn report_if_failed(self, status: NTSTATUS) {
        // Match NT_SUCCESS: zero and positive informational statuses are not
        // failures. Immediate negative statuses never reach this completion path.
        if status >= 0 {
            return;
        }
        if let Some(callback) = self.failure_callback {
            // SAFETY: InjectionFailureCallback's contract requires the function to
            // remain callable through handle destruction and support DISPATCH_LEVEL.
            unsafe { callback(self.path, status) };
        }
    }
}

pub struct TransportPacketList {
    protocol: TransportProtocol,
    ipv6: bool,
    pub net_buffer_list: NetBufferList,
    event_data_offset: usize,
    remote_ip: [u8; 16],
    endpoint_handle: u64,
    remote_scope_id: SCOPE_ID,
    // Owned copy of the WFP control data. The original WFP pointer is only
    // valid during the ALE classify callback; the bytes are copied here so they outlive it.
    control_data: Option<Box<[u8]>>,
    inbound: bool,
    loopback: bool,
    completion: InjectionCompletion,
    compartment_id: Option<u32>,
    interface_index: u32,
    sub_interface_index: u32,
    // send_params and remote_ip must outlive inject_ale_packet
    // because FwpsInjectTransportSendAsync1 may read them after the function returns.
    // Storing send_params here ensures it lives on the heap inside Box<TransportPacketList>
    // until the WFP completion callback (free_transport_packet) drops it.
    send_params: FWPS_TRANSPORT_SEND_PARAMS1,
}

// The list is moved from classify to the verdict path and then reclaimed by a
// potentially different CPU in the WFP injection completion callback. All raw
// pointers either target fields in the same pinned Box or native NBL state whose
// ownership moves with this value.
unsafe impl Send for TransportPacketList {}

/// Ownership passed to a network-injection completion callback.
struct NetworkInjectionContext {
    net_buffer_list: NetBufferList,
    completion: InjectionCompletion,
}

/// One admission reference held from transport-injection submission until the
/// native injection API has either accepted the NBL or rejected it synchronously.
///
/// The referenced rundown object is pinned in `TransportInjectionState` and
/// cannot be released by `Injector::destroy` until every such guard is dropped.
struct TransportSubmission {
    rundown: *mut EX_RUNDOWN_REF,
}

// The guard is transferred through a nonpaged DPC allocation. The kernel rundown
// API provides the cross-CPU synchronization; the raw pointer remains pinned and
// live until teardown has waited for this reference.
unsafe impl Send for TransportSubmission {}

impl Drop for TransportSubmission {
    fn drop(&mut self) {
        unsafe {
            ExReleaseRundownProtection(self.rundown);
        }
    }
}

/// A fresh DPC object and all ownership needed to submit one TCP injection.
///
/// `packet_list` remains `Some` while the DPC is queued. The DPC routine takes
/// it immediately before calling WFP. If queuing fails, dropping this allocation
/// reclaims both the packet clone and its transport-submission admission.
struct TcpTransportInjection {
    dpc: KDPC,
    injection_handle: *mut c_void,
    packet_list: Option<TransportPacketList>,
    _submission: TransportSubmission,
}

// The allocation is published only to the kernel DPC queue and consumed exactly
// once by `inject_tcp_transport_dpc`.
unsafe impl Send for TcpTransportInjection {}

struct TransportInjectionState {
    handle: AtomicPtr<c_void>,
    submissions: UnsafeCell<EX_RUNDOWN_REF>,
    closing: AtomicBool,
    drained: AtomicBool,
    _pin: PhantomPinned,
}

// The handle is atomic, the rundown reference is accessed only through kernel
// interlocked APIs, and the remaining fields are atomic lifecycle state.
unsafe impl Send for TransportInjectionState {}
unsafe impl Sync for TransportInjectionState {}

impl TransportInjectionState {
    fn new() -> Pin<Box<Self>> {
        let state = Box::pin(Self {
            handle: AtomicPtr::new(INVALID_HANDLE_VALUE),
            submissions: UnsafeCell::new(EX_RUNDOWN_REF {
                Anonymous: EX_RUNDOWN_REF_0 { Count: 0 },
            }),
            closing: AtomicBool::new(false),
            drained: AtomicBool::new(false),
            _pin: PhantomPinned,
        });
        unsafe {
            ExInitializeRundownProtection(state.as_ref().get_ref().submissions.get());
        }
        state
    }

    fn acquire_submission(&self) -> Option<TransportSubmission> {
        if self.closing.load(Ordering::Acquire) {
            return None;
        }

        let rundown = self.submissions.get();
        if unsafe { ExAcquireRundownProtection(rundown) } == 0 {
            return None;
        }
        let submission = TransportSubmission { rundown };

        // A closer can publish its state between the first check and the native
        // acquisition. The reference is included in its wait either way; reject
        // the new operation so teardown does not submit additional packet work.
        if self.closing.load(Ordering::Acquire) {
            return None;
        }

        Some(submission)
    }

    /// Closes submission admission and waits until every queued DPC has called
    /// WFP (or reclaimed an immediately rejected packet). This must run at
    /// PASSIVE_LEVEL before the transport injection handle is destroyed.
    fn close_and_wait(&self) {
        if self.drained.load(Ordering::Acquire) {
            return;
        }

        if self
            .closing
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .is_ok()
        {
            unsafe {
                ExWaitForRundownProtectionRelease(self.submissions.get());
                ExRundownCompleted(self.submissions.get());
            }
            self.drained.store(true, Ordering::Release);
            return;
        }

        // Concurrent destroy callers are unusual, but `destroy` takes `&self`.
        // Do not let one caller destroy the handle while the other is still
        // waiting for the DPC submissions that use it.
        while !self.drained.load(Ordering::Acquire) {
            crate::utils::sleep_ms(1);
        }
    }
}

#[derive(Clone, Copy)]
pub struct InjectInfo {
    pub ipv6: bool,
    pub inbound: bool,
    pub loopback: bool,
    /// Routing compartment supplied with the original WFP indication. `None`
    /// means WFP omitted the metadata and injection must use the unspecified
    /// compartment rather than inventing a default compartment identity.
    pub compartment_id: Option<u32>,
    pub interface_index: u32,
    pub sub_interface_index: u32,
}

fn resolve_compartment_id(compartment_id: Option<u32>) -> COMPARTMENT_ID {
    compartment_id
        .map(|id| id as COMPARTMENT_ID)
        .unwrap_or(UNSPECIFIED_COMPARTMENT_ID)
}

impl TransportPacketList {
    /// Packet bytes exposed to user space. Inbound ALE clones start at the IP
    /// header for reinjection, but connection events are transport-layer records.
    pub fn get_event_data(&self) -> Option<&[u8]> {
        self.net_buffer_list
            .get_data()?
            .get(self.event_data_offset..)
    }
}

pub struct Injector {
    transport_injection: Pin<Box<TransportInjectionState>>,
    packet_inject_handle_v4: AtomicPtr<c_void>,
    packet_inject_handle_v6: AtomicPtr<c_void>,
    injection_failure_callback: Option<InjectionFailureCallback>,
}

// Injection handles are atomically published and are destroyed only after both
// callback and dispatch admission have drained. The atomics make the handle
// storage safe to access through `&Device` during the final teardown phase.

// TODO: Implement custom allocator for the packet buffers for reusing memory and reducing allocations. This should improve latency.
impl Injector {
    /// Creates WFP injection handles without a final-status reporter.
    pub fn new() -> Result<Self, String> {
        Self::new_with_failure_callback(None)
    }

    /// Creates WFP injection handles and registers an optional final-status
    /// reporter. The reporter is copied into each accepted injection's owned
    /// completion context, so it never dereferences this `Injector` asynchronously.
    pub fn new_with_failure_callback(
        injection_failure_callback: Option<InjectionFailureCallback>,
    ) -> Result<Self, String> {
        // Commit each output only after WFP reports success and returns a usable
        // handle. A failed native call owns the contents of its output parameter;
        // leaving that value in `self` would make Drop attempt to destroy an
        // uninitialized or otherwise invalid handle.
        let injector = Self {
            transport_injection: TransportInjectionState::new(),
            packet_inject_handle_v4: AtomicPtr::new(INVALID_HANDLE_VALUE),
            packet_inject_handle_v6: AtomicPtr::new(INVALID_HANDLE_VALUE),
            injection_failure_callback,
        };

        unsafe {
            let mut handle = INVALID_HANDLE_VALUE;
            let status =
                FwpsInjectionHandleCreate0(AF_UNSPEC, FWPS_INJECTION_TYPE_TRANSPORT, &mut handle);
            check_ntstatus(status)
                .map_err(|err| format!("failed to create transport injection handle: {}", err))?;
            if handle.is_null() || handle == INVALID_HANDLE_VALUE {
                return Err("WFP returned an invalid transport injection handle".to_string());
            }
            injector
                .transport_injection
                .handle
                .store(handle, Ordering::Release);

            handle = INVALID_HANDLE_VALUE;
            let status =
                FwpsInjectionHandleCreate0(AF_INET, FWPS_INJECTION_TYPE_NETWORK, &mut handle);
            check_ntstatus(status)
                .map_err(|err| format!("failed to create IPv4 injection handle: {}", err))?;
            if handle.is_null() || handle == INVALID_HANDLE_VALUE {
                return Err("WFP returned an invalid IPv4 injection handle".to_string());
            }
            injector
                .packet_inject_handle_v4
                .store(handle, Ordering::Release);

            handle = INVALID_HANDLE_VALUE;
            let status =
                FwpsInjectionHandleCreate0(AF_INET6, FWPS_INJECTION_TYPE_NETWORK, &mut handle);
            check_ntstatus(status)
                .map_err(|err| format!("failed to create IPv6 injection handle: {}", err))?;
            if handle.is_null() || handle == INVALID_HANDLE_VALUE {
                return Err("WFP returned an invalid IPv6 injection handle".to_string());
            }
            injector
                .packet_inject_handle_v6
                .store(handle, Ordering::Release);
        }

        Ok(injector)
    }

    /// Destroys every injection handle owned by this object.
    ///
    /// WFP waits for pending injections before returning from each destroy call.
    /// A handle is cleared only after the API reports `STATUS_SUCCESS`, so a
    /// caller can retry a failed teardown without losing track of a possibly
    /// live handle. This method must run at PASSIVE_LEVEL.
    pub fn destroy(&self) -> Result<(), String> {
        if !crate::utils::is_passive_level() {
            return Err("WFP injection handles must be destroyed at PASSIVE_LEVEL".to_string());
        }

        // A queued TCP DPC is not yet visible to WFP, so handle destruction cannot
        // wait for it. Close DPC admission and wait until every queued routine has
        // either submitted its injection or reclaimed an immediate failure first.
        // Once submitted, FwpsInjectionHandleDestroy0 supplies the second half of
        // the lifetime protocol by waiting for WFP completion callbacks.
        self.transport_injection.close_and_wait();

        fn destroy_one(handle: &AtomicPtr<c_void>, name: &str) -> Result<(), String> {
            let value = handle.load(Ordering::Acquire);
            if value == INVALID_HANDLE_VALUE || value.is_null() {
                return Ok(());
            }

            let status = unsafe { FwpsInjectionHandleDestroy0(value) };
            if status == STATUS_SUCCESS {
                handle.store(INVALID_HANDLE_VALUE, Ordering::Release);
                return Ok(());
            }

            Err(format!(
                "failed to destroy {} injection handle: NTSTATUS({:#010x})",
                name, status as u32
            ))
        }

        let mut first_error = None;
        for (handle, name) in [
            (&self.transport_injection.handle, "transport"),
            (&self.packet_inject_handle_v4, "IPv4 network"),
            (&self.packet_inject_handle_v6, "IPv6 network"),
        ] {
            if let Err(err) = destroy_one(handle, name) {
                if first_error.is_none() {
                    first_error = Some(err);
                }
            }
        }

        match first_error {
            Some(err) => Err(err),
            None => Ok(()),
        }
    }

    /// Creates the packet list used to replay an ALE indication.
    pub fn from_ale_callout(
        &self,
        protocol: TransportProtocol,
        ipv6: bool,
        callout_data: &CalloutData,
        net_buffer_list: NetBufferList,
        event_data_offset: usize,
        remote_ip_slice: &[u8],
        inbound: bool,
        loopback: bool,
        interface_index: u32,
        sub_interface_index: u32,
    ) -> Result<TransportPacketList, String> {
        let control_data = callout_data
            .get_control_data()
            .map(|cd| cd.to_vec().into_boxed_slice());

        let address_length = if ipv6 { 16 } else { 4 };
        if remote_ip_slice.len() != address_length {
            return Err("invalid remote address length".to_string());
        }
        let mut remote_ip = [0; 16];
        remote_ip[..address_length].copy_from_slice(remote_ip_slice);

        let remote_scope_id = callout_data.get_remote_scope_id().unwrap_or(SCOPE_ID {
            Anonymous: SCOPE_ID_0 { Value: 0 },
        });
        let send_params = FWPS_TRANSPORT_SEND_PARAMS1 {
            remote_address: core::ptr::null(),
            remote_scope_id,
            control_data: core::ptr::null_mut(),
            control_data_length: 0,
            header_include_header: core::ptr::null_mut(),
            header_include_header_length: 0,
        };

        Ok(TransportPacketList {
            protocol,
            ipv6,
            net_buffer_list,
            event_data_offset,
            remote_ip,
            endpoint_handle: callout_data.get_transport_endpoint_handle().unwrap_or(0),
            remote_scope_id,
            control_data,
            inbound,
            loopback,
            completion: InjectionCompletion {
                path: InjectionPath::for_transport(inbound),
                failure_callback: self.injection_failure_callback,
            },
            compartment_id: callout_data.get_compartment_id(),
            interface_index,
            sub_interface_index,
            // Pointers are populated after this object has a stable Box address.
            send_params,
        })
    }

    /// Injects a saved ALE packet after its pended operation is complete.
    ///
    /// Inbound TCP and UDP loopback packets are returned through network-send
    /// injection. Windows completes transport-receive injection of either protocol
    /// with `STATUS_DATA_NOT_ACCEPTED`; a TCP network-receive comparison fails with
    /// the same status. UDP loses the datagram, while TCP waits for the sender's
    /// one-second SYN retransmission. Routing the complete IP packet back through
    /// loopback send preserves immediate delivery. Non-loopback UDP uses transport
    /// injection in the caller's current context. Non-loopback TCP is always
    /// transferred to a regular DPC before either transport injection API is called.
    pub fn inject_ale_packet(&self, packet_list: TransportPacketList) -> Result<(), String> {
        if packet_list
            .protocol
            .uses_network_send_for_ale(packet_list.inbound, packet_list.loopback)
        {
            let inject_info = InjectInfo {
                ipv6: packet_list.ipv6,
                inbound: true,
                loopback: true,
                compartment_id: packet_list.compartment_id,
                interface_index: packet_list.interface_index,
                sub_interface_index: packet_list.sub_interface_index,
            };
            return self.inject_net_buffer_list(packet_list.net_buffer_list, inject_info);
        }

        let Some(submission) = self.transport_injection.acquire_submission() else {
            return Err("failed to inject packet: transport injector is closing".to_string());
        };
        let transport_inject_handle = self.transport_injection.handle.load(Ordering::Acquire);
        if transport_inject_handle == INVALID_HANDLE_VALUE || transport_inject_handle.is_null() {
            return Err("failed to inject packet: invalid handle value".to_string());
        }

        if packet_list.protocol.requires_dpc() {
            return Self::queue_tcp_transport_injection(
                transport_inject_handle,
                packet_list,
                submission,
            );
        }

        let result = Self::inject_transport_packet_now(transport_inject_handle, packet_list);
        // Keep teardown from destroying the handle until the native call has
        // either accepted ownership or returned an immediate error.
        drop(submission);
        result
    }

    fn queue_tcp_transport_injection(
        transport_inject_handle: *mut c_void,
        packet_list: TransportPacketList,
        submission: TransportSubmission,
    ) -> Result<(), String> {
        // The global driver allocator uses nonpaged pool, as required for KDPC
        // storage. Initialize the DPC only after the Box has its final address.
        let work = Box::new(TcpTransportInjection {
            dpc: unsafe { MaybeUninit::zeroed().assume_init() },
            injection_handle: transport_inject_handle,
            packet_list: Some(packet_list),
            _submission: submission,
        });
        let work = Box::into_raw(work);

        let queued = unsafe {
            KeInitializeDpc(
                core::ptr::addr_of_mut!((*work).dpc),
                inject_tcp_transport_dpc,
                work.cast(),
            );
            KeInsertQueueDpc(
                core::ptr::addr_of_mut!((*work).dpc),
                core::ptr::null_mut(),
                core::ptr::null_mut(),
            )
        };
        if queued == 0 {
            // This is a fresh KDPC, so it cannot legitimately already be queued.
            // Ownership was not transferred; reclaim the packet and rundown guard.
            unsafe {
                drop(Box::from_raw(work));
            }
            return Err("failed to queue TCP transport injection DPC".to_string());
        }

        Ok(())
    }

    fn inject_transport_packet_now(
        transport_inject_handle: *mut c_void,
        packet_list: TransportPacketList,
    ) -> Result<(), String> {
        if transport_inject_handle == INVALID_HANDLE_VALUE || transport_inject_handle.is_null() {
            return Err("failed to inject packet: invalid handle value".to_string());
        }

        // Box the entire packet_list so that remote_ip and send_params are
        // heap-allocated. Their addresses remain stable until
        // free_transport_packet drops the Box after WFP calls the completion.
        let mut boxed = Box::new(packet_list);
        let raw_nbl = boxed.net_buffer_list.nbl;

        unsafe {
            // Populate send_params with pointers into the boxed struct.
            // These addresses are stable because the Box will not move until freed.
            let mut control_data_length = 0;
            let control_data: *mut c_void = match &boxed.control_data {
                Some(cd) => {
                    control_data_length = cd.len();
                    cd.as_ptr() as *mut c_void
                }
                None => core::ptr::null_mut(),
            };
            boxed.send_params = FWPS_TRANSPORT_SEND_PARAMS1 {
                remote_address: boxed.remote_ip.as_ptr(),
                remote_scope_id: boxed.remote_scope_id,
                control_data,
                control_data_length: control_data_length as u32,
                header_include_header: core::ptr::null_mut(),
                header_include_header_length: 0,
            };

            let address_family = if boxed.ipv6 { AF_INET6 } else { AF_INET };
            let compartment_id = resolve_compartment_id(boxed.compartment_id);
            let raw_ptr = Box::into_raw(boxed);

            // On success WFP owns this Box until free_transport_packet. On an
            // immediate failure the completion callback is not called, so reclaim
            // the exact same allocation below.
            let status = if (*raw_ptr).inbound {
                FwpsInjectTransportReceiveAsync0(
                    transport_inject_handle,
                    core::ptr::null_mut(),
                    core::ptr::null_mut(),
                    0,
                    address_family,
                    compartment_id,
                    (*raw_ptr).interface_index,
                    (*raw_ptr).sub_interface_index,
                    raw_nbl,
                    free_transport_packet,
                    raw_ptr as _,
                )
            } else {
                FwpsInjectTransportSendAsync1(
                    transport_inject_handle,
                    core::ptr::null_mut(),
                    (*raw_ptr).endpoint_handle,
                    0,
                    &mut (*raw_ptr).send_params,
                    address_family,
                    compartment_id,
                    raw_nbl,
                    free_transport_packet,
                    raw_ptr as _,
                )
            };
            reclaim_immediate_injection_failure(raw_ptr, status)?;
        }

        Ok(())
    }

    pub fn inject_net_buffer_list(
        &self,
        net_buffer_list: NetBufferList,
        inject_info: InjectInfo,
    ) -> Result<(), String> {
        let inject_handle = if inject_info.ipv6 {
            self.packet_inject_handle_v6.load(Ordering::Acquire)
        } else {
            self.packet_inject_handle_v4.load(Ordering::Acquire)
        };
        if inject_handle == INVALID_HANDLE_VALUE || inject_handle.is_null() {
            return Err("failed to inject packet: invalid handle value".to_string());
        }

        let path = InjectionPath::for_network(inject_info.inbound, inject_info.loopback);
        // Escape the stack, so both packet ownership and completion-reporting
        // metadata remain valid until WFP invokes free_packet.
        let packet_boxed = Box::new(NetworkInjectionContext {
            net_buffer_list,
            completion: InjectionCompletion {
                path,
                failure_callback: self.injection_failure_callback,
            },
        });
        let nbl = packet_boxed.net_buffer_list.nbl;
        let packet_pointer = Box::into_raw(packet_boxed);

        let compartment_id = resolve_compartment_id(inject_info.compartment_id);

        let status = if path == InjectionPath::NetworkReceive {
            // Inject inbound.
            unsafe {
                FwpsInjectNetworkReceiveAsync0(
                    inject_handle,
                    core::ptr::null_mut(),
                    0,
                    compartment_id,
                    inject_info.interface_index,
                    inject_info.sub_interface_index,
                    nbl,
                    free_packet,
                    packet_pointer.cast(),
                )
            }
        } else {
            // Inject outbound.
            unsafe {
                FwpsInjectNetworkSendAsync0(
                    inject_handle,
                    core::ptr::null_mut(),
                    0,
                    compartment_id,
                    nbl,
                    free_packet,
                    packet_pointer.cast(),
                )
            }
        };

        // Check for error.
        if let Err(err) = check_ntstatus(status) {
            unsafe {
                // Get back ownership for data.
                _ = Box::from_raw(packet_pointer);
            }
            return Err(err);
        }

        return Ok(());
    }

    /// Returns the origin WFP reports for a network-layer NBL.
    ///
    /// # Safety
    ///
    /// `nbl` may be null. Otherwise it must point to a live `NET_BUFFER_LIST`
    /// supplied by WFP and remain valid and readable for the duration of the
    /// synchronous query. The caller must obey WFP's IRQL and synchronization
    /// requirements for `FwpsQueryPacketInjectionState0`.
    pub unsafe fn network_packet_injection_origin(
        &self,
        nbl: *const NET_BUFFER_LIST,
        ipv6: bool,
    ) -> PacketInjectionOrigin {
        let inject_handle = if ipv6 {
            self.packet_inject_handle_v6.load(Ordering::Acquire)
        } else {
            self.packet_inject_handle_v4.load(Ordering::Acquire)
        };
        if inject_handle == INVALID_HANDLE_VALUE || inject_handle.is_null() || nbl.is_null() {
            return PacketInjectionOrigin::Unknown;
        }

        let state = unsafe {
            FwpsQueryPacketInjectionState0(inject_handle, nbl, core::ptr::null_mut())
        };
        packet_injection_origin(state)
    }

    /// Returns whether WFP identifies this network-layer NBL as self-injected.
    ///
    /// # Safety
    ///
    /// The safety requirements are the same as for
    /// [`Self::network_packet_injection_origin`].
    pub unsafe fn was_network_packet_injected_by_self(
        &self,
        nbl: *const NET_BUFFER_LIST,
        ipv6: bool,
    ) -> bool {
        unsafe { self.network_packet_injection_origin(nbl, ipv6) }.is_self_injected()
    }

    /// Returns the origin WFP reports for a transport-layer NBL.
    ///
    /// # Safety
    ///
    /// `nbl` may be null. Otherwise it must point to a live `NET_BUFFER_LIST`
    /// supplied by WFP and remain valid and readable for the duration of the
    /// synchronous query. The caller must obey WFP's IRQL and synchronization
    /// requirements for `FwpsQueryPacketInjectionState0`.
    pub unsafe fn transport_packet_injection_origin(
        &self,
        nbl: *const NET_BUFFER_LIST,
    ) -> PacketInjectionOrigin {
        let transport_inject_handle = self.transport_injection.handle.load(Ordering::Acquire);
        if transport_inject_handle == INVALID_HANDLE_VALUE
            || transport_inject_handle.is_null()
            || nbl.is_null()
        {
            return PacketInjectionOrigin::Unknown;
        }

        let state = unsafe {
            FwpsQueryPacketInjectionState0(
                transport_inject_handle,
                nbl,
                core::ptr::null_mut(),
            )
        };
        packet_injection_origin(state)
    }

    /// Returns whether WFP identifies this transport-layer NBL as self-injected.
    ///
    /// # Safety
    ///
    /// The safety requirements are the same as for
    /// [`Self::transport_packet_injection_origin`].
    pub unsafe fn was_transport_packet_injected_by_self(
        &self,
        nbl: *const NET_BUFFER_LIST,
    ) -> bool {
        unsafe { self.transport_packet_injection_origin(nbl) }.is_self_injected()
    }
}

/// Finalizes the ownership transfer attempted by an asynchronous WFP injection.
///
/// # Safety
///
/// `packet` must be a non-null pointer returned by `Box::into_raw`. On native
/// success the registered completion callback must own and eventually reconstruct
/// that Box. On native failure WFP must not retain or complete the pointer.
unsafe fn reclaim_immediate_injection_failure<T>(
    packet: *mut T,
    status: i32,
) -> Result<(), String> {
    match check_ntstatus(status) {
        Ok(()) => Ok(()),
        Err(err) => {
            unsafe {
                drop(Box::from_raw(packet));
            }
            Err(err)
        }
    }
}

/// Runs one TCP transport injection at DISPATCH_LEVEL.
///
/// The callback barrier keeps this driver code resident through return. The work
/// allocation owns the queued packet and one transport-rundown reference; both are
/// released on every exit path. A successful WFP call moves only the packet Box to
/// `free_transport_packet`, while an immediate failure reclaims it in the call.
unsafe extern "system" fn inject_tcp_transport_dpc(
    _dpc: *mut KDPC,
    deferred_context: *mut c_void,
    _system_argument_1: *mut c_void,
    _system_argument_2: *mut c_void,
) {
    // Acquire code-lifetime admission before interpreting driver-owned context.
    // This remains available after classify admission closes and is drained only
    // after Injector::destroy has waited for this work item.
    let callback_admission = crate::callback_barrier::CALLBACK_BARRIER.enter_callback();

    if deferred_context.is_null() {
        crate::err!("TCP transport injection DPC received a null context");
        return;
    }
    let mut work = unsafe { Box::from_raw(deferred_context as *mut TcpTransportInjection) };

    if callback_admission.is_none() {
        crate::err!("TCP transport injection DPC ran after callback admission closed");
        return;
    }

    let Some(packet_list) = work.packet_list.take() else {
        crate::err!("TCP transport injection DPC had no packet");
        return;
    };
    if let Err(err) = Injector::inject_transport_packet_now(work.injection_handle, packet_list) {
        crate::err!("failed to inject TCP packet from DPC: {}", err);
    }
}

impl Drop for Injector {
    fn drop(&mut self) {
        if !crate::utils::is_passive_level() {
            // The WFP API is PASSIVE_LEVEL-only. Do not call it from an
            // arbitrary destructor context; normal Device teardown invokes
            // destroy before this object is dropped and at PASSIVE_LEVEL.
            crate::err!("cannot destroy injection handles outside PASSIVE_LEVEL");
            return;
        }

        // A live injection handle must never survive the end of DriverUnload.
        // Retry here as a final guard for construction/error paths; normal
        // teardown has already cleared all handles, so this loop is normally
        // entered only once and returns immediately.
        loop {
            match self.destroy() {
                Ok(()) => return,
                Err(err) => {
                    crate::err!("failed to destroy injection handles during drop: {}", err);
                    crate::utils::sleep_ms(1);
                }
            }
        }
    }
}

unsafe extern "system" fn free_packet(
    context: *mut c_void,
    net_buffer_list: *mut NET_BUFFER_LIST,
    _dispatch_level: BOOLEAN,
) {
    // Copy the final status and reporter while WFP and the completion context keep
    // both objects live. Reporting never takes ownership of either packet object.
    let status = unsafe { net_buffer_list.as_ref() }.map(|nbl| nbl.Status);
    let completion = if context.is_null() {
        None
    } else {
        // SAFETY: an accepted network injection supplied exactly one live
        // `NetworkInjectionContext` as this callback context.
        Some(unsafe { (*(context as *mut NetworkInjectionContext)).completion })
    };
    if let (Some(completion), Some(status)) = (completion, status) {
        completion.report_if_failed(status);
    }

    if !context.is_null() {
        // SAFETY: WFP invokes this callback once for the accepted injection, so
        // this is the matching and only reconstruction of the owned context.
        unsafe { drop(Box::from_raw(context as *mut NetworkInjectionContext)) };
    }
}

/// Completion callback for transport inject paths (both inbound and outbound).
/// The context is a `Box<TransportPacketList>` cast to `*mut c_void`.
/// Dropping it also correctly drops the inner `NetBufferList`.
unsafe extern "system" fn free_transport_packet(
    context: *mut c_void,
    net_buffer_list: *mut NET_BUFFER_LIST,
    _dispatch_level: BOOLEAN,
) {
    // WFP keeps the NBL and context live through this callback. Copy only the
    // status and small reporter value before returning packet ownership.
    let status = unsafe { net_buffer_list.as_ref() }.map(|nbl| nbl.Status);
    let completion = if context.is_null() {
        None
    } else {
        // SAFETY: an accepted transport injection supplied exactly one live
        // `TransportPacketList` as this callback context.
        Some(unsafe { (*(context as *mut TransportPacketList)).completion })
    };
    if let (Some(completion), Some(status)) = (completion, status) {
        completion.report_if_failed(status);
    }

    if !context.is_null() {
        // SAFETY: the accepted transport injection transferred exactly one
        // `Box<TransportPacketList>` to WFP as this context. The completion is the
        // matching one-shot ownership return.
        unsafe { drop(Box::from_raw(context as *mut TransportPacketList)) };
    }
}

#[cfg(test)]
mod tests {
    use super::{
        packet_injection_origin, reclaim_immediate_injection_failure, resolve_compartment_id,
        InjectionCompletion, InjectionPath, PacketInjectionOrigin, TransportProtocol,
        UNSPECIFIED_COMPARTMENT_ID,
    };
    use crate::ffi::FWPS_PACKET_INJECTION_STATE;
    use alloc::{boxed::Box, sync::Arc};
    use core::sync::atomic::{AtomicBool, AtomicI32, AtomicU8, AtomicUsize, Ordering};

    struct DropProbe(Arc<AtomicBool>);

    impl Drop for DropProbe {
        fn drop(&mut self) {
            self.0.store(true, Ordering::Release);
        }
    }

    static FAILURE_CALLBACK_COUNT: AtomicUsize = AtomicUsize::new(0);
    static FAILURE_CALLBACK_PATH: AtomicU8 = AtomicU8::new(0);
    static FAILURE_CALLBACK_STATUS: AtomicI32 = AtomicI32::new(0);

    unsafe fn capture_injection_failure(path: InjectionPath, status: i32) {
        FAILURE_CALLBACK_PATH.store(path as u8, Ordering::Release);
        FAILURE_CALLBACK_STATUS.store(status, Ordering::Release);
        FAILURE_CALLBACK_COUNT.fetch_add(1, Ordering::AcqRel);
    }

    #[test]
    fn completion_reports_only_negative_status_with_original_context() {
        const STATUS_DATA_NOT_ACCEPTED: i32 = 0xc000_021bu32 as i32;

        FAILURE_CALLBACK_COUNT.store(0, Ordering::Release);
        FAILURE_CALLBACK_PATH.store(0, Ordering::Release);
        FAILURE_CALLBACK_STATUS.store(0, Ordering::Release);
        let completion = InjectionCompletion {
            path: InjectionPath::TransportReceive,
            failure_callback: Some(capture_injection_failure),
        };

        completion.report_if_failed(0);
        completion.report_if_failed(1);
        assert_eq!(FAILURE_CALLBACK_COUNT.load(Ordering::Acquire), 0);

        completion.report_if_failed(STATUS_DATA_NOT_ACCEPTED);
        assert_eq!(FAILURE_CALLBACK_COUNT.load(Ordering::Acquire), 1);
        assert_eq!(
            FAILURE_CALLBACK_PATH.load(Ordering::Acquire),
            InjectionPath::TransportReceive as u8
        );
        assert_eq!(
            FAILURE_CALLBACK_STATUS.load(Ordering::Acquire),
            STATUS_DATA_NOT_ACCEPTED
        );
    }

    #[test]
    fn completion_paths_match_native_injection_apis() {
        assert_eq!(
            InjectionPath::for_network(true, false),
            InjectionPath::NetworkReceive
        );
        assert_eq!(
            InjectionPath::for_network(true, true),
            InjectionPath::NetworkSend
        );
        assert_eq!(
            InjectionPath::for_network(false, false),
            InjectionPath::NetworkSend
        );
        assert_eq!(
            InjectionPath::for_transport(true),
            InjectionPath::TransportReceive
        );
        assert_eq!(
            InjectionPath::for_transport(false),
            InjectionPath::TransportSend
        );
    }

    #[test]
    fn native_injection_states_preserve_packet_origin() {
        assert_eq!(
            packet_injection_origin(
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_NOT_INJECTED
            ),
            PacketInjectionOrigin::NotInjected
        );
        assert_eq!(
            packet_injection_origin(
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_SELF
            ),
            PacketInjectionOrigin::InjectedBySelf
        );
        assert_eq!(
            packet_injection_origin(
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_PREVIOUSLY_INJECTED_BY_SELF
            ),
            PacketInjectionOrigin::InjectedBySelf
        );
        assert_eq!(
            packet_injection_origin(
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_OTHER
            ),
            PacketInjectionOrigin::InjectedByOther
        );
        assert_eq!(
            packet_injection_origin(
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTION_STATE_MAX
            ),
            PacketInjectionOrigin::Unknown
        );
    }

    #[test]
    fn only_tcp_requires_dpc_transport_injection() {
        assert!(TransportProtocol::Tcp.requires_dpc());
        assert!(!TransportProtocol::Udp.requires_dpc());
    }

    #[test]
    fn only_inbound_loopback_uses_ale_network_send() {
        for protocol in [TransportProtocol::Tcp, TransportProtocol::Udp] {
            assert!(protocol.uses_network_send_for_ale(true, true));
            assert!(!protocol.uses_network_send_for_ale(false, true));
            assert!(!protocol.uses_network_send_for_ale(true, false));
        }
    }

    #[test]
    fn network_injection_preserves_explicit_compartment() {
        assert_eq!(resolve_compartment_id(Some(42)), 42);
    }

    #[test]
    fn network_injection_uses_unspecified_compartment_when_metadata_is_absent() {
        assert_eq!(resolve_compartment_id(None), UNSPECIFIED_COMPARTMENT_ID);
    }

    #[test]
    fn immediate_injection_failure_reclaims_context() {
        let dropped = Arc::new(AtomicBool::new(false));
        let packet = Box::into_raw(Box::new(DropProbe(dropped.clone())));

        let result = unsafe { reclaim_immediate_injection_failure(packet, -1) };

        assert!(result.is_err());
        assert!(dropped.load(Ordering::Acquire));
    }

    #[test]
    fn successful_injection_leaves_context_for_completion() {
        let dropped = Arc::new(AtomicBool::new(false));
        let packet = Box::into_raw(Box::new(DropProbe(dropped.clone())));

        let result = unsafe { reclaim_immediate_injection_failure(packet, 0) };

        assert!(result.is_ok());
        assert!(!dropped.load(Ordering::Acquire));
        unsafe {
            drop(Box::from_raw(packet));
        }
        assert!(dropped.load(Ordering::Acquire));
    }
}
