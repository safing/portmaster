use crate::{
    ffi::{FwpsCompleteOperation0, FwpsPendOperation0},
    utils::check_ntstatus,
};

use super::{
    classify::ClassifyOut,
    layer::{Layer, Value, ValueType},
    metadata::FwpsIncomingMetadataValues,
    packet::TransportPacketList,
    stream_data::StreamCalloutIoPacket,
    FilterEngine,
};
use alloc::string::{String, ToString};
use core::{ffi::c_void, ptr::NonNull};
use windows_sys::Win32::{
    Foundation::HANDLE,
    NetworkManagement::WindowsFilteringPlatform::FWP_CONDITION_FLAG_IS_REAUTHORIZE,
    Networking::WinSock::SCOPE_ID,
};

pub enum ClassifyDefer {
    Initial(HANDLE, Option<TransportPacketList>),
    Reauthorization(usize, Option<TransportPacketList>),
}

impl ClassifyDefer {
    pub fn complete(
        self,
        filter_engine: &mut FilterEngine,
    ) -> Result<Option<TransportPacketList>, String> {
        unsafe {
            match self {
                ClassifyDefer::Initial(context, packet_list) => {
                    FwpsCompleteOperation0(context, core::ptr::null_mut());
                    return Ok(packet_list);
                }
                ClassifyDefer::Reauthorization(_callout_id, packet_list) => {
                    // There is no way to reset single filter. If another request for filter reset is trigger at the same time it will fail.
                    filter_engine.reset_all_filters()?;
                    return Ok(packet_list);
                }
            }
        }
    }

    // pub fn add_net_buffer(&mut self, nbl: NetBufferList) {
    //     if let Some(packet_list) = match self {
    //         ClassifyDefer::Initial(_, packet_list) => packet_list,
    //         ClassifyDefer::Reauthorization(_, packet_list) => packet_list,
    //     } {
    //         packet_list.net_buffer_list_queue.push(nbl);
    //     }
    // }
}

pub struct CalloutData<'a> {
    pub layer: Layer,
    pub(crate) callout_id: usize,
    pub(crate) values: &'a [Value],
    pub(crate) metadata: *const FwpsIncomingMetadataValues,
    pub(crate) classify_out: *mut ClassifyOut,
    pub(crate) layer_data: *mut c_void,
}

impl<'a> CalloutData<'a> {
    pub fn get_value_type(&self, index: usize) -> ValueType {
        self.values[index].value_type
    }

    pub fn get_value_u8(&'a self, index: usize) -> u8 {
        unsafe {
            return self.values[index].value.uint8;
        };
    }

    pub fn get_value_u16(&'a self, index: usize) -> u16 {
        unsafe {
            return self.values[index].value.uint16;
        };
    }

    pub fn get_value_u32(&'a self, index: usize) -> u32 {
        unsafe {
            return self.values[index].value.uint32;
        };
    }

    pub fn get_value_byte_array16(&'a self, index: usize) -> &[u8; 16] {
        unsafe {
            return self.values[index].value.byte_array16.as_ref().unwrap();
        };
    }

    pub fn get_process_id(&self) -> Option<u64> {
        unsafe { (*self.metadata).get_process_id() }
    }

    pub fn get_process_path(&self) -> Option<String> {
        unsafe {
            return (*self.metadata).get_process_path();
        }
    }

    pub fn get_transport_endpoint_handle(&self) -> Option<u64> {
        unsafe {
            return (*self.metadata).get_transport_endpoint_handle();
        }
    }

    pub fn get_remote_scope_id(&self) -> Option<SCOPE_ID> {
        unsafe {
            return (*self.metadata).get_remote_scope_id();
        }
    }

    pub fn get_control_data(&self) -> Option<NonNull<[u8]>> {
        unsafe {
            return (*self.metadata).get_control_data();
        }
    }

    pub fn get_layer_data(&self) -> *mut c_void {
        return self.layer_data;
    }

    pub fn get_stream_callout_packet(&self) -> Option<&mut StreamCalloutIoPacket> {
        match self.layer {
            Layer::StreamV4 | Layer::StreamV4Discard | Layer::StreamV6 | Layer::StreamV6Discard => unsafe {
                (self.layer_data as *mut StreamCalloutIoPacket).as_mut()
            },
            _ => None,
        }
    }

    pub fn is_fragment_data(&self) -> bool {
        unsafe { (*self.metadata).is_fragment_data() }
    }

    pub fn pend_operation(
        &mut self,
        packet_list: Option<TransportPacketList>,
    ) -> Result<ClassifyDefer, String> {
        unsafe {
            let mut completion_context: HANDLE = core::ptr::null_mut();
            if let Some(completion_handle) = (*self.metadata).get_completion_handle() {
                let status = FwpsPendOperation0(completion_handle, &mut completion_context);
                check_ntstatus(status)?;

                return Ok(ClassifyDefer::Initial(completion_context, packet_list));
            }

            Err("callout not supported".to_string())
        }
    }

    pub fn pend_filter_rest(&mut self, packet_list: Option<TransportPacketList>) -> ClassifyDefer {
        ClassifyDefer::Reauthorization(self.callout_id, packet_list)
    }

    pub fn action_permit(&mut self) {
        unsafe {
            (*self.classify_out).action_permit();
            (*self.classify_out).clear_absorb_flag();
        }
    }

    pub fn action_continue(&mut self) {
        unsafe {
            (*self.classify_out).action_continue();
            (*self.classify_out).clear_absorb_flag();
        }
    }

    pub fn action_block(&mut self) {
        unsafe {
            (*self.classify_out).action_block();
            (*self.classify_out).clear_absorb_flag();
        }
    }

    pub fn action_none(&mut self) {
        unsafe {
            (*self.classify_out).set_none();
            (*self.classify_out).clear_absorb_flag();
        }
    }

    pub fn block_and_absorb(&mut self) {
        unsafe {
            (*self.classify_out).action_block();
            (*self.classify_out).set_absorb();
        }
    }
    pub fn clear_write_flag(&mut self) {
        unsafe {
            (*self.classify_out).clear_write_flag();
        }
    }

    pub fn is_reauthorize(&self, flags_index: usize) -> bool {
        self.get_value_u32(flags_index) & FWP_CONDITION_FLAG_IS_REAUTHORIZE > 0
    }

    pub fn get_callout_id(&self) -> usize {
        self.callout_id
    }
}
