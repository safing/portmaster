use core::ffi::c_void;

use alloc::string::String;
use widestring::U16CString;
use windows_sys::Win32::{
    Foundation::HANDLE,
    NetworkManagement::{
        IpHelper::IP_ADDRESS_PREFIX,
        WindowsFilteringPlatform::{
            FWPS_METADATA_FIELD_COMPARTMENT_ID, FWPS_METADATA_FIELD_COMPLETION_HANDLE,
            FWPS_METADATA_FIELD_FLOW_HANDLE, FWPS_METADATA_FIELD_FRAGMENT_DATA,
            FWPS_METADATA_FIELD_IP_HEADER_SIZE, FWPS_METADATA_FIELD_PACKET_DIRECTION,
            FWPS_METADATA_FIELD_PARENT_ENDPOINT_HANDLE, FWPS_METADATA_FIELD_PROCESS_ID,
            FWPS_METADATA_FIELD_PROCESS_PATH, FWPS_METADATA_FIELD_REMOTE_SCOPE_ID,
            FWPS_METADATA_FIELD_TRANSPORT_CONTROL_DATA,
            FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE,
            FWPS_METADATA_FIELD_TRANSPORT_HEADER_SIZE, FWP_BYTE_BLOB, FWP_DIRECTION,
            FWP_DIRECTION_INBOUND, FWP_DIRECTION_OUTBOUND,
        },
    },
    Networking::WinSock::SCOPE_ID,
};

/// Direction of the packet that triggered an ALE reauthorization.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PacketDirection {
    Outbound,
    Inbound,
}

impl PacketDirection {
    fn from_raw(direction: FWP_DIRECTION) -> Option<Self> {
        match direction {
            FWP_DIRECTION_OUTBOUND => Some(Self::Outbound),
            FWP_DIRECTION_INBOUND => Some(Self::Inbound),
            _ => None,
        }
    }
}

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
    local_redirect_target_pid: u32,
    /// original destination of a redirected connection
    original_destination: *mut c_void,
    redirect_records: HANDLE,
    /// Bitmask representing which L2 values are set.
    current_l2_metadata_values: u32,
    /// L2 layer Flags;
    l2_flags: u32,
    ethernet_mac_header_size: u32,
    wifi_operation_mode: u32,
    v_switch_source_port_id: u32,
    v_switch_source_nic_index: u16,
    v_switch_destination_port_id: u32,
    v_switch_packet_context: HANDLE,
    sub_process_tag: *mut c_void,
    // Reserved for system use.
    reserved1: u64,
}

impl FwpsIncomingMetadataValues {
    pub(crate) fn has_field(&self, field: u32) -> bool {
        self.current_metadata_values & field == field
    }

    pub(crate) fn get_flow_handle(&self) -> Option<u64> {
        if self.has_field(FWPS_METADATA_FIELD_FLOW_HANDLE) {
            return Some(self.flow_handle);
        }

        None
    }

    pub(crate) fn get_process_id(&self) -> Option<u64> {
        if self.has_field(FWPS_METADATA_FIELD_PROCESS_ID) {
            return Some(self.process_id);
        }

        None
    }

    /// Copies the WFP process-path blob into an owned UTF-8 string.
    ///
    /// # Safety
    ///
    /// When the process-path metadata bit is set, `self.process_path` and its
    /// non-empty data buffer must be readable native WFP metadata that remains
    /// live for this call.
    pub(crate) unsafe fn get_process_path(&self) -> Option<String> {
        if !self.has_field(FWPS_METADATA_FIELD_PROCESS_PATH) || self.process_path.is_null() {
            return None;
        }

        // SAFETY: the caller guarantees that the advertised WFP blob is readable
        // for this call; the null case was rejected above.
        let path = unsafe { &*self.process_path };
        if path.size == 0 {
            return None;
        }
        // Process paths are UTF-16. Reject an odd byte count rather than silently
        // truncating malformed metadata, and never pass a null non-empty buffer to
        // widestring's raw-slice constructor.
        if path.size % 2 != 0 || path.data.is_null() {
            return None;
        }

        // SAFETY: the WFP blob contract supplied by the caller guarantees that
        // `data` is readable for exactly `size` bytes. The checks above convert
        // that even byte count to the corresponding number of UTF-16 code units.
        if let Ok(path16) =
            unsafe { U16CString::from_ptr(path.data as *const u16, path.size as usize / 2) }
        {
            if let Ok(path) = path16.to_string() {
                return Some(path);
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

    pub(crate) fn get_parent_endpoint_handle(&self) -> Option<u64> {
        if self.has_field(FWPS_METADATA_FIELD_PARENT_ENDPOINT_HANDLE) {
            return Some(self.parent_endpoint_handle);
        }

        None
    }

    pub(crate) fn get_remote_scope_id(&self) -> Option<SCOPE_ID> {
        if self.has_field(FWPS_METADATA_FIELD_REMOTE_SCOPE_ID) {
            return Some(self.remote_scope_id);
        }

        None
    }

    /// Size of the IP header for this indication, as reported by WFP.
    ///
    /// At the inbound packet layers the net buffer starts past the IP header, and
    /// this is how far back it has to be retreated to reach it. It reflects any
    /// IP options actually present, unlike the fixed IPV4_HEADER_LEN.
    pub(crate) fn get_ip_header_size(&self) -> Option<u32> {
        if self.has_field(FWPS_METADATA_FIELD_IP_HEADER_SIZE) {
            return Some(self.ip_header_size);
        }

        None
    }

    pub(crate) fn get_transport_header_size(&self) -> Option<u32> {
        if self.has_field(FWPS_METADATA_FIELD_TRANSPORT_HEADER_SIZE) {
            return Some(self.transport_header_size);
        }

        None
    }

    pub(crate) fn get_compartment_id(&self) -> Option<u32> {
        if self.has_field(FWPS_METADATA_FIELD_COMPARTMENT_ID) {
            return Some(self.compartment_id);
        }

        None
    }

    /// Returns the direction of the packet that triggered ALE reauthorization.
    /// The backing field is not valid for ordinary authorization indications.
    pub(crate) fn get_packet_direction(&self) -> Option<PacketDirection> {
        if self.has_field(FWPS_METADATA_FIELD_PACKET_DIRECTION) {
            return PacketDirection::from_raw(self.packet_direction);
        }

        None
    }

    pub(crate) fn is_fragment_data(&self) -> bool {
        if self.has_field(FWPS_METADATA_FIELD_FRAGMENT_DATA) {
            return self.fragment_metadata.fragment_offset != 0;
        }

        false
    }

    /// Borrows the WFP transport-control-data buffer for this metadata lifetime.
    ///
    /// # Safety
    ///
    /// When the transport-control-data metadata bit is set, every non-null,
    /// non-empty `control_data` range must be readable for the lifetime of the
    /// returned slice and must not be mutated while that slice is borrowed.
    pub(crate) unsafe fn get_control_data(&self) -> Option<&[u8]> {
        if self.has_field(FWPS_METADATA_FIELD_TRANSPORT_CONTROL_DATA) {
            if self.control_data.is_null() || self.control_data_length == 0 {
                return None;
            }
            // SAFETY: the caller supplies the validity and immutability contract;
            // null and zero-length representations were rejected above.
            return Some(unsafe {
                core::slice::from_raw_parts(self.control_data, self.control_data_length as usize)
            });
        }

        None
    }
}

/// Native `FWPS_DISCARD_MODULE0` storage. Keep this integer-backed because the
/// value is supplied by WFP and newer kernels may add enum values.
type FwpsDiscardModule0 = i32;

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

// FWPS_INCOMING_METADATA_VALUES0 is supplied by WFP and read directly by the
// classify path. Keep its complete x64 layout pinned to the WDK, including
// fields this driver currently ignores, so a later field access cannot inherit
// an unnoticed shift in the tail.
#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, offset_of, size_of};

    assert!(size_of::<FwpsDiscardModule0>() == 4);
    assert!(align_of::<FwpsDiscardModule0>() == 4);
    assert!(size_of::<FwpsDiscardMetadata0>() == 16);
    assert!(align_of::<FwpsDiscardMetadata0>() == 8);
    assert!(offset_of!(FwpsDiscardMetadata0, discard_module) == 0);
    assert!(offset_of!(FwpsDiscardMetadata0, discard_reason) == 4);
    assert!(offset_of!(FwpsDiscardMetadata0, filter_id) == 8);

    assert!(size_of::<FwpsInboundFragmentMetadata0>() == 12);
    assert!(align_of::<FwpsInboundFragmentMetadata0>() == 4);
    assert!(offset_of!(FwpsInboundFragmentMetadata0, fragment_identification) == 0);
    assert!(offset_of!(FwpsInboundFragmentMetadata0, fragment_offset) == 4);
    assert!(offset_of!(FwpsInboundFragmentMetadata0, fragment_length) == 8);

    assert!(size_of::<FwpsIncomingMetadataValues>() == 280);
    assert!(align_of::<FwpsIncomingMetadataValues>() == 8);
    assert!(offset_of!(FwpsIncomingMetadataValues, current_metadata_values) == 0);
    assert!(offset_of!(FwpsIncomingMetadataValues, flags) == 4);
    assert!(offset_of!(FwpsIncomingMetadataValues, reserved) == 8);
    assert!(offset_of!(FwpsIncomingMetadataValues, discard_metadata) == 16);
    assert!(offset_of!(FwpsIncomingMetadataValues, flow_handle) == 32);
    assert!(offset_of!(FwpsIncomingMetadataValues, ip_header_size) == 40);
    assert!(offset_of!(FwpsIncomingMetadataValues, transport_header_size) == 44);
    assert!(offset_of!(FwpsIncomingMetadataValues, process_path) == 48);
    assert!(offset_of!(FwpsIncomingMetadataValues, token) == 56);
    assert!(offset_of!(FwpsIncomingMetadataValues, process_id) == 64);
    assert!(offset_of!(FwpsIncomingMetadataValues, source_interface_index) == 72);
    assert!(offset_of!(FwpsIncomingMetadataValues, destination_interface_index) == 76);
    assert!(offset_of!(FwpsIncomingMetadataValues, compartment_id) == 80);
    assert!(offset_of!(FwpsIncomingMetadataValues, fragment_metadata) == 84);
    assert!(offset_of!(FwpsIncomingMetadataValues, path_mtu) == 96);
    assert!(offset_of!(FwpsIncomingMetadataValues, completion_handle) == 104);
    assert!(offset_of!(FwpsIncomingMetadataValues, transport_endpoint_handle) == 112);
    assert!(offset_of!(FwpsIncomingMetadataValues, remote_scope_id) == 120);
    assert!(offset_of!(FwpsIncomingMetadataValues, control_data) == 128);
    assert!(offset_of!(FwpsIncomingMetadataValues, control_data_length) == 136);
    assert!(offset_of!(FwpsIncomingMetadataValues, packet_direction) == 140);
    assert!(offset_of!(FwpsIncomingMetadataValues, header_include_header) == 144);
    assert!(offset_of!(FwpsIncomingMetadataValues, header_include_header_length) == 152);
    assert!(offset_of!(FwpsIncomingMetadataValues, destination_prefix) == 156);
    assert!(offset_of!(FwpsIncomingMetadataValues, frame_length) == 188);
    assert!(offset_of!(FwpsIncomingMetadataValues, parent_endpoint_handle) == 192);
    assert!(offset_of!(FwpsIncomingMetadataValues, icmp_id_and_sequence) == 200);
    assert!(offset_of!(FwpsIncomingMetadataValues, local_redirect_target_pid) == 204);
    assert!(offset_of!(FwpsIncomingMetadataValues, original_destination) == 208);
    assert!(offset_of!(FwpsIncomingMetadataValues, redirect_records) == 216);
    assert!(offset_of!(FwpsIncomingMetadataValues, current_l2_metadata_values) == 224);
    assert!(offset_of!(FwpsIncomingMetadataValues, l2_flags) == 228);
    assert!(offset_of!(FwpsIncomingMetadataValues, ethernet_mac_header_size) == 232);
    assert!(offset_of!(FwpsIncomingMetadataValues, wifi_operation_mode) == 236);
    assert!(offset_of!(FwpsIncomingMetadataValues, v_switch_source_port_id) == 240);
    assert!(offset_of!(FwpsIncomingMetadataValues, v_switch_source_nic_index) == 244);
    assert!(offset_of!(FwpsIncomingMetadataValues, v_switch_destination_port_id) == 248);
    assert!(offset_of!(FwpsIncomingMetadataValues, v_switch_packet_context) == 256);
    assert!(offset_of!(FwpsIncomingMetadataValues, sub_process_tag) == 264);
    assert!(offset_of!(FwpsIncomingMetadataValues, reserved1) == 272);
};

#[cfg(test)]
mod tests {
    use super::{FwpsIncomingMetadataValues, PacketDirection};
    use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::{
        FWPS_METADATA_FIELD_PARENT_ENDPOINT_HANDLE, FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE,
        FWP_DIRECTION_INBOUND, FWP_DIRECTION_MAX, FWP_DIRECTION_OUTBOUND,
    };

    #[test]
    fn endpoint_handles_require_their_individual_metadata_bits() {
        // Every field in this native metadata mirror permits an all-zero
        // representation; populate only the values exercised by this test.
        let mut metadata: FwpsIncomingMetadataValues = unsafe { core::mem::zeroed() };
        metadata.transport_endpoint_handle = 0x1111;
        metadata.parent_endpoint_handle = 0x2222;

        assert_eq!(metadata.get_transport_endpoint_handle(), None);
        assert_eq!(metadata.get_parent_endpoint_handle(), None);

        metadata.current_metadata_values = FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE;
        assert_eq!(metadata.get_transport_endpoint_handle(), Some(0x1111));
        assert_eq!(metadata.get_parent_endpoint_handle(), None);

        metadata.current_metadata_values = FWPS_METADATA_FIELD_PARENT_ENDPOINT_HANDLE;
        assert_eq!(metadata.get_transport_endpoint_handle(), None);
        assert_eq!(metadata.get_parent_endpoint_handle(), Some(0x2222));

        metadata.current_metadata_values = FWPS_METADATA_FIELD_TRANSPORT_ENDPOINT_HANDLE
            | FWPS_METADATA_FIELD_PARENT_ENDPOINT_HANDLE;
        assert_eq!(metadata.get_transport_endpoint_handle(), Some(0x1111));
        assert_eq!(metadata.get_parent_endpoint_handle(), Some(0x2222));
    }

    #[test]
    fn packet_direction_accepts_only_native_inbound_and_outbound_values() {
        assert_eq!(
            PacketDirection::from_raw(FWP_DIRECTION_OUTBOUND),
            Some(PacketDirection::Outbound)
        );
        assert_eq!(
            PacketDirection::from_raw(FWP_DIRECTION_INBOUND),
            Some(PacketDirection::Inbound)
        );
        assert_eq!(PacketDirection::from_raw(FWP_DIRECTION_MAX), None);
        assert_eq!(PacketDirection::from_raw(-1), None);
    }
}
