use alloc::{
    boxed::Box,
    string::{String, ToString},
};
use core::{ffi::c_void, mem::MaybeUninit, ptr::NonNull};
use windows_sys::Win32::{
    Foundation::{HANDLE, INVALID_HANDLE_VALUE},
    Networking::WinSock::{AF_INET, AF_INET6, AF_UNSPEC, SCOPE_ID},
    System::Kernel::UNSPECIFIED_COMPARTMENT_ID,
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

pub struct TransportPacketList {
    ipv6: bool,
    pub net_buffer_list: NetBufferList,
    remote_ip: [u8; 16],
    endpoint_handle: u64,
    remote_scope_id: SCOPE_ID,
    control_data: Option<NonNull<[u8]>>,
    inbound: bool,
    interface_index: u32,
    sub_interface_index: u32,
}

pub struct InjectInfo {
    pub ipv6: bool,
    pub inbound: bool,
    pub loopback: bool,
    pub interface_index: u32,
    pub sub_interface_index: u32,
}

pub struct Injector {
    transport_inject_handle: HANDLE,
    packet_inject_handle_v4: HANDLE,
    packet_inject_handle_v6: HANDLE,
}

// TODO: Implement custom allocator for the packet buffers for reusing memory and reducing allocations. This should improve latency.
impl Injector {
    pub fn new() -> Self {
        let mut transport_inject_handle: HANDLE = INVALID_HANDLE_VALUE;
        let mut packet_inject_handle_v4: HANDLE = INVALID_HANDLE_VALUE;
        let mut packet_inject_handle_v6: HANDLE = INVALID_HANDLE_VALUE;
        unsafe {
            let status = FwpsInjectionHandleCreate0(
                AF_UNSPEC,
                FWPS_INJECTION_TYPE_TRANSPORT,
                &mut transport_inject_handle,
            );
            if let Err(err) = check_ntstatus(status) {
                crate::err!("error allocating transport inject handle: {}", err);
            }
            let status = FwpsInjectionHandleCreate0(
                AF_INET,
                FWPS_INJECTION_TYPE_NETWORK,
                &mut packet_inject_handle_v4,
            );

            if let Err(err) = check_ntstatus(status) {
                crate::err!("error allocating network inject handle: {}", err);
            }
            let status = FwpsInjectionHandleCreate0(
                AF_INET6,
                FWPS_INJECTION_TYPE_NETWORK,
                &mut packet_inject_handle_v6,
            );

            if let Err(err) = check_ntstatus(status) {
                crate::err!("error allocating network inject handle: {}", err);
            }
        }
        Self {
            transport_inject_handle,
            packet_inject_handle_v4,
            packet_inject_handle_v6,
        }
    }

    // TODO: pick a better name
    pub fn from_ale_callout(
        ipv6: bool,
        callout_data: &CalloutData,
        net_buffer_list: NetBufferList,
        remote_ip_slice: &[u8],
        inbound: bool,
        interface_index: u32,
        sub_interface_index: u32,
    ) -> TransportPacketList {
        let mut control_data = None;
        if let Some(cd) = callout_data.get_control_data() {
            control_data = Some(cd);
        }
        let mut remote_ip: [u8; 16] = [0; 16];
        if ipv6 {
            remote_ip[0..16].copy_from_slice(remote_ip_slice);
        } else {
            remote_ip[0..4].copy_from_slice(remote_ip_slice);
        }

        TransportPacketList {
            ipv6,
            net_buffer_list,
            remote_ip,
            endpoint_handle: callout_data.get_transport_endpoint_handle().unwrap_or(0),
            remote_scope_id: callout_data
                .get_remote_scope_id()
                .unwrap_or(unsafe { MaybeUninit::zeroed().assume_init() }),
            control_data,
            inbound,
            interface_index,
            sub_interface_index,
        }
    }

    // TODO: pick a better name. This is not transport
    pub fn inject_packet_list_transport(
        &self,
        packet_list: TransportPacketList,
    ) -> Result<(), String> {
        if self.transport_inject_handle == INVALID_HANDLE_VALUE {
            return Err("failed to inject packet: invalid handle value".to_string());
        }
        unsafe {
            let mut control_data_length = 0;
            let control_data = match &packet_list.control_data {
                Some(cd) => {
                    control_data_length = cd.len();
                    cd.as_ptr().cast()
                }
                None => core::ptr::null_mut(),
            };

            let mut send_params = FWPS_TRANSPORT_SEND_PARAMS1 {
                remote_address: &packet_list.remote_ip as _,
                remote_scope_id: packet_list.remote_scope_id,
                control_data: control_data as _,
                control_data_length: control_data_length as u32,
                header_include_header: core::ptr::null_mut(),
                header_include_header_length: 0,
            };
            let address_family = if packet_list.ipv6 { AF_INET6 } else { AF_INET };

            let net_buffer_list = packet_list.net_buffer_list;
            // Escape the stack. Packet buffer should be valid until the packet is injected.
            let boxed_nbl = Box::new(net_buffer_list);
            let raw_nbl = boxed_nbl.nbl;
            let raw_ptr = Box::into_raw(boxed_nbl);

            // Inject
            let status = if packet_list.inbound {
                FwpsInjectTransportReceiveAsync0(
                    self.transport_inject_handle,
                    core::ptr::null_mut(),
                    core::ptr::null_mut(),
                    0,
                    address_family,
                    UNSPECIFIED_COMPARTMENT_ID,
                    packet_list.interface_index,
                    packet_list.sub_interface_index,
                    raw_nbl,
                    free_packet,
                    raw_ptr as _,
                )
            } else {
                FwpsInjectTransportSendAsync1(
                    self.transport_inject_handle,
                    core::ptr::null_mut(),
                    packet_list.endpoint_handle,
                    0,
                    &mut send_params,
                    address_family,
                    UNSPECIFIED_COMPARTMENT_ID,
                    raw_nbl,
                    free_packet,
                    raw_ptr as _,
                )
            };
            // Check for success
            if let Err(err) = check_ntstatus(status) {
                _ = Box::from_raw(raw_ptr);
                return Err(err);
            }
        }

        return Ok(());
    }

    pub fn inject_net_buffer_list(
        &self,
        net_buffer_list: NetBufferList,
        inject_info: InjectInfo,
    ) -> Result<(), String> {
        if self.packet_inject_handle_v4 == INVALID_HANDLE_VALUE {
            return Err("failed to inject packet: invalid handle value".to_string());
        }
        // Escape the stack, so the data can be freed after inject is complete.
        let packet_boxed = Box::new(net_buffer_list);
        let nbl = packet_boxed.nbl;
        let packet_pointer = Box::into_raw(packet_boxed);

        let inject_handle = if inject_info.ipv6 {
            self.packet_inject_handle_v6
        } else {
            self.packet_inject_handle_v4
        };

        let status = if inject_info.inbound && !inject_info.loopback {
            // Inject inbound.
            unsafe {
                FwpsInjectNetworkReceiveAsync0(
                    inject_handle,
                    core::ptr::null_mut(),
                    0,
                    UNSPECIFIED_COMPARTMENT_ID,
                    inject_info.interface_index,
                    inject_info.sub_interface_index,
                    nbl,
                    free_packet,
                    (packet_pointer as *mut NetBufferList) as _,
                )
            }
        } else {
            // Inject outbound.
            unsafe {
                FwpsInjectNetworkSendAsync0(
                    inject_handle,
                    core::ptr::null_mut(),
                    0,
                    UNSPECIFIED_COMPARTMENT_ID,
                    nbl,
                    free_packet,
                    (packet_pointer as *mut NetBufferList) as _,
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

    pub fn was_network_packet_injected_by_self(
        &self,
        nbl: *const NET_BUFFER_LIST,
        ipv6: bool,
    ) -> bool {
        let inject_handle = if ipv6 {
            self.packet_inject_handle_v6
        } else {
            self.packet_inject_handle_v4
        };
        if inject_handle == INVALID_HANDLE_VALUE || inject_handle.is_null() {
            return false;
        }

        unsafe {
            let state = FwpsQueryPacketInjectionState0(inject_handle, nbl, core::ptr::null_mut());

            match state {
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_NOT_INJECTED => false,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_SELF => true,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_OTHER => false,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_PREVIOUSLY_INJECTED_BY_SELF => true,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTION_STATE_MAX => false,
            }
        }
    }

    pub fn was_network_packet_injected_by_self_ale(&self, nbl: *const NET_BUFFER_LIST) -> bool {
        unsafe {
            let state = FwpsQueryPacketInjectionState0(
                self.transport_inject_handle,
                nbl,
                core::ptr::null_mut(),
            );

            match state {
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_NOT_INJECTED => false,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_SELF => true,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTED_BY_OTHER => false,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_PREVIOUSLY_INJECTED_BY_SELF => true,
                FWPS_PACKET_INJECTION_STATE::FWPS_PACKET_INJECTION_STATE_MAX => false,
            }
        }
    }
}

impl Drop for Injector {
    fn drop(&mut self) {
        unsafe {
            if self.transport_inject_handle != INVALID_HANDLE_VALUE
                && !self.transport_inject_handle.is_null()
            {
                FwpsInjectionHandleDestroy0(self.transport_inject_handle);
                self.transport_inject_handle = INVALID_HANDLE_VALUE;
            }
            if self.packet_inject_handle_v4 != INVALID_HANDLE_VALUE
                && !self.packet_inject_handle_v4.is_null()
            {
                FwpsInjectionHandleDestroy0(self.packet_inject_handle_v4);
                self.packet_inject_handle_v4 = INVALID_HANDLE_VALUE;
            }
            if self.packet_inject_handle_v6 != INVALID_HANDLE_VALUE
                && !self.packet_inject_handle_v6.is_null()
            {
                FwpsInjectionHandleDestroy0(self.packet_inject_handle_v6);
                self.packet_inject_handle_v6 = INVALID_HANDLE_VALUE;
            }
        }
    }
}

unsafe extern "C" fn free_packet(
    context: *mut c_void,
    net_buffer_list: *mut NET_BUFFER_LIST,
    _dispatch_level: bool,
) {
    if let Some(nbl) = net_buffer_list.as_ref() {
        if let Err(err) = check_ntstatus(nbl.Status) {
            crate::err!("inject status: {}", err);
        } else {
            crate::dbg!("inject status: Ok");
        }
    }
    _ = Box::from_raw(context as *mut NetBufferList);
}
