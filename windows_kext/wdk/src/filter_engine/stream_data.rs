use crate::ffi::{NET_BUFFER, NET_BUFFER_LIST};
use windows_sys::Wdk::Foundation::MDL;

const FWPS_STREAM_FLAG_RECEIVE: u32 = 0x00000001;

#[repr(C)]
pub enum StreamActionType {
    None,
    NeedMoreData,
    DropConnection,
    Defer,
    AllowConnection,
    TypeMax,
}

#[repr(C)]
pub struct StreamCalloutIoPacket {
    stream_data: *mut StreamData,
    missed_bytes: usize,
    count_bytes_required: usize,
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
