use crate::ffi::{NET_BUFFER, NET_BUFFER_LIST};
use windows_sys::Wdk::Foundation::MDL;

const FWPS_STREAM_FLAG_RECEIVE: u32 = 0x00000001;

/// Native `FWPS_STREAM_ACTION_TYPE` storage.
///
/// WFP owns this structure, so the field uses the C enum's integer storage
/// rather than a closed Rust enum with invalid discriminants.
pub type StreamActionType = i32;

#[repr(C)]
pub struct StreamCalloutIoPacket {
    stream_data: *mut StreamData,
    missed_bytes: usize,
    count_bytes_required: u32,
    count_bytes_enforced: usize,
    stream_action: StreamActionType,
}

#[repr(C)]
pub struct StreamDataOffset {
    // NET_BUFFER_LIST in which offset lies.
    net_buffer_list: *mut NET_BUFFER_LIST,
    // NET_BUFFER in which offset lies.
    net_buffer: *mut NET_BUFFER,
    // MDL in which offset lies.
    mdl: *mut MDL,
    // Byte offset from the beginning of the MDL in which data lies.
    mdl_offset: u32,
    // Offset relative to the DataOffset of the NET_BUFFER.
    net_buffer_offset: u32,
    // Offset from the beginning of the entire stream buffer.
    stream_data_offset: usize,
}

#[repr(C)]
pub struct StreamData {
    flags: u32,
    data_offset: StreamDataOffset,
    data_length: usize,
    net_buffer_list_chain: *mut NET_BUFFER_LIST,
}

#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, offset_of, size_of};

    assert!(size_of::<StreamActionType>() == 4);
    assert!(align_of::<StreamActionType>() == 4);

    assert!(size_of::<StreamDataOffset>() == 40);
    assert!(align_of::<StreamDataOffset>() == 8);
    assert!(offset_of!(StreamDataOffset, net_buffer_list) == 0);
    assert!(offset_of!(StreamDataOffset, net_buffer) == 8);
    assert!(offset_of!(StreamDataOffset, mdl) == 16);
    assert!(offset_of!(StreamDataOffset, mdl_offset) == 24);
    assert!(offset_of!(StreamDataOffset, net_buffer_offset) == 28);
    assert!(offset_of!(StreamDataOffset, stream_data_offset) == 32);

    assert!(size_of::<StreamData>() == 64);
    assert!(align_of::<StreamData>() == 8);
    assert!(offset_of!(StreamData, flags) == 0);
    assert!(offset_of!(StreamData, data_offset) == 8);
    assert!(offset_of!(StreamData, data_length) == 48);
    assert!(offset_of!(StreamData, net_buffer_list_chain) == 56);

    assert!(size_of::<StreamCalloutIoPacket>() == 40);
    assert!(align_of::<StreamCalloutIoPacket>() == 8);
    assert!(offset_of!(StreamCalloutIoPacket, stream_data) == 0);
    assert!(offset_of!(StreamCalloutIoPacket, missed_bytes) == 8);
    assert!(offset_of!(StreamCalloutIoPacket, count_bytes_required) == 16);
    assert!(offset_of!(StreamCalloutIoPacket, count_bytes_enforced) == 24);
    assert!(offset_of!(StreamCalloutIoPacket, stream_action) == 32);
};

impl StreamCalloutIoPacket {
    pub fn get_data_len(&self) -> usize {
        unsafe {
            if let Some(stream_data) = self.stream_data.as_ref() {
                return stream_data.data_length;
            }
        }
        return 0;
    }

    pub fn is_receive(&self) -> bool {
        unsafe {
            if let Some(stream_data) = self.stream_data.as_ref() {
                return stream_data.flags & FWPS_STREAM_FLAG_RECEIVE > 0;
            }
        }
        return false;
    }
}
