use core::{ffi::c_void, ptr::NonNull};

use alloc::string::String;
use widestring::U16CString;
use windows_sys::Win32::{
    Foundation::HANDLE,
    NetworkManagement::{
        IpHelper::IP_ADDRESS_PREFIX,
        WindowsFilteringPlatform::{
            FWPS_METADATA_FIELD_COMPLETION_HANDLE, FWPS_METADATA_FIELD_FRAGMENT_DATA,
            FWPS_METADATA_FIELD_PROCESS_ID, FWPS_METADATA_FIELD_PROCESS_PATH,
            FWPS_METADATA_FIELD_REMOTE_SCOPE_ID, FWPS_METADATA_FIELD_TRANSPORT_CONTROL_DATA,
            FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE, FWP_BYTE_BLOB, FWP_DIRECTION,
        },
    },
    Networking::WinSock::SCOPE_ID,
};

#[repr(C)]
pub(crate) struct FwpsIncomingMetadataValues {
    /// Bitmask representing which values are set.
    current_metadata_values: u32,
    /// Internal flags;
    flags: u32,
    /// Reserved for system use.
    reserved: u64,
    /// Discard module and reason.
    discard_metadata: FwpsDiscardMetadata0,
    /// Flow Handle.
    flow_handle: u64,
    /// IP Header size.
    ip_header_size: u32,
    /// Transport Header size
    transport_header_size: u32,
    /// Process Path.
    process_path: *const FWP_BYTE_BLOB,
    /// Token used for authorization.
    token: u64,
    /// Process Id.
    process_id: u64,
    /// Source and Destination interface indices for discard indications.
    source_interface_index: u32,
    destination_interface_index: u32,
    /// Compartment Id for injection APIs.
    compartment_id: u32,
    /// Fragment data for inbound fragments.
    fragment_metadata: FwpsInboundFragmentMetadata0,
    /// Path MTU for outbound packets (to enable calculation of fragments).
    path_mtu: u32,
    /// Completion handle (required in order to be able to pend at this layer).
    completion_handle: HANDLE,
    /// Endpoint handle for use in outbound transport layer injection.
    transport_endpoint_handle: u64,
    /// Remote scope id for use in outbound transport layer injection.
    remote_scope_id: SCOPE_ID,
    /// Socket control data (and length) for use in outbound transport layer injection.
    control_data: *const u8,
    control_data_length: u32,
    /// Direction for the current packet. Only specified for ALE re-authorization.
    packet_direction: FWP_DIRECTION,
    /// Raw IP header (and length) if the packet is sent with IP header from a RAW socket.
    header_include_header: *mut c_void,
    header_include_header_length: u32,
    destination_prefix: IP_ADDRESS_PREFIX,
    frame_length: u16,
    parent_endpoint_handle: u64,
    icmp_id_and_sequence: u32,
    /// PID of the process that will be accepting the redirected connection
    local_redirect_target_pid: u64,
    /// original destination of a redirected connection
    original_destination: *mut c_void,
    redirect_records: HANDLE,
    /// Bitmask representing which L2 values are set.
    current_l2_metadata_values: u32,
    /// L2 layer Flags;
    l2_flags: u32,
    ethernet_mac_header_size: u32,
    wifi_operation_mode: u32,
    padding0: u32,
    padding1: u16,
    padding2: u32,
    v_switch_packet_context: HANDLE,
    sub_process_tag: *mut c_void,
    // Reserved for system use.
    reserved1: u64,
}

impl FwpsIncomingMetadataValues {
    pub(crate) fn has_field(&self, field: u32) -> bool {
        self.current_metadata_values & field > 0
    }

    pub(crate) fn get_process_id(&self) -> Option<u64> {
        if self.has_field(FWPS_METADATA_FIELD_PROCESS_ID) {
            return Some(self.process_id);
        }

        None
    }

    pub(crate) unsafe fn get_process_path(&self) -> Option<String> {
        if self.has_field(FWPS_METADATA_FIELD_PROCESS_PATH) {
            if let Ok(path16) = U16CString::from_ptr(
                core::mem::transmute((*self.process_path).data),
                (*self.process_path).size as usize / 2,
            ) {
                if let Ok(path) = path16.to_string() {
                    return Some(path);
                }
            }
        }

        None
    }

    pub(crate) fn get_completion_handle(&self) -> Option<HANDLE> {
        if self.has_field(FWPS_METADATA_FIELD_COMPLETION_HANDLE) {
            return Some(self.completion_handle);
        }

        None
    }

    pub(crate) fn get_transport_endpoint_handle(&self) -> Option<u64> {
        if self.has_field(FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE) {
            return Some(self.transport_endpoint_handle);
        }

        None
    }

    pub(crate) fn get_remote_scope_id(&self) -> Option<SCOPE_ID> {
        if self.has_field(FWPS_METADATA_FIELD_REMOTE_SCOPE_ID) {
            return Some(self.remote_scope_id);
        }

        None
    }

    pub(crate) fn is_fragment_data(&self) -> bool {
        if self.has_field(FWPS_METADATA_FIELD_FRAGMENT_DATA) {
            return self.fragment_metadata.fragment_offset != 0;
        }

        false
    }

    pub(crate) unsafe fn get_control_data(&self) -> Option<NonNull<[u8]>> {
        if self.has_field(FWPS_METADATA_FIELD_TRANSPORT_CONTROL_DATA) {
            if self.control_data.is_null() || self.control_data_length == 0 {
                return None;
            }
            let ptr = NonNull::new(self.control_data as *mut u8).unwrap();
            let slice = NonNull::slice_from_raw_parts(ptr, self.control_data_length as usize);
            return Some(slice);
        }

        None
    }
}

#[allow(dead_code)]
#[repr(C)]
enum FwpsDiscardModule0 {
    Network = 0,
    Transport = 1,
    General = 2,
    Max = 3,
}

#[repr(C)]
struct FwpsDiscardMetadata0 {
    discard_module: FwpsDiscardModule0,
    discard_reason: u32,
    filter_id: u64,
}

#[repr(C)]
struct FwpsInboundFragmentMetadata0 {
    fragment_identification: u32,
    fragment_offset: u16,
    fragment_length: u32,
}
