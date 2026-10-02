use crate::{
    ffi::{
        FwpsAcquireClassifyHandle0, FwpsCompleteClassify0, FwpsCompleteOperation0,
        FwpsFlowAssociateContext0, FwpsPendClassify0, FwpsPendOperation0,
        FwpsReleaseClassifyHandle0,
    },
    utils::check_ntstatus,
};

use super::{
    classify::ClassifyOut,
    layer::{Layer, Value, ValueType},
    metadata::{FwpsIncomingMetadataValues, PacketDirection},
    packet::TransportPacketList,
    stream_data::StreamCalloutIoPacket,
};
use crate::consts::FWP_CONDITION_FLAG_IS_REASSEMBLED;
use alloc::string::{String, ToString};
use core::ffi::c_void;
use windows_sys::Win32::{
    Foundation::HANDLE,
    NetworkManagement::WindowsFilteringPlatform::FWP_CONDITION_FLAG_IS_REAUTHORIZE,
    Networking::WinSock::SCOPE_ID,
};

enum ClassifyDeferKind {
    Initial(HANDLE),
    Reauthorization,
}

/// An ALE operation whose native completion context is owned by this wrapper.
///
/// The private representation prevents safe code from manufacturing a WFP
/// completion context. Values can be created only from a live [`CalloutData`]
/// indication through [`CalloutData::pend_operation`] or
/// [`CalloutData::pend_filter_rest`].
pub struct ClassifyDefer {
    kind: ClassifyDeferKind,
    packet_list: Option<TransportPacketList>,
}

/// An asynchronous WFP classification whose native handle and output copy are
/// owned until completion.
///
/// Endpoint-closure callouts use this form rather than `FwpsPendOperation0` so
/// queued packet processing can finish before WFP shuts the endpoint down.
#[must_use = "dropping this value completes the pended classification"]
pub struct ClassifyPend {
    handle: u64,
    classify_out: ClassifyOut,
}

impl ClassifyPend {
    fn finish(&mut self) {
        if self.handle == 0 {
            return;
        }

        // SAFETY: `handle` was acquired and successfully pended by
        // `CalloutData::pend_classify`. This value owns the local handle reference
        // and the required deep copy of classifyOut, and `handle = 0` below makes
        // completion and release one-shot on every explicit or Drop path.
        unsafe {
            FwpsCompleteClassify0(self.handle, 0, &self.classify_out);
            FwpsReleaseClassifyHandle0(self.handle);
        }
        self.handle = 0;
    }

    /// Completes the pended classification and releases its acquired handle.
    pub fn complete(mut self) {
        self.finish();
    }
}

impl Drop for ClassifyPend {
    fn drop(&mut self) {
        self.finish();
    }
}

impl ClassifyDefer {
    fn initial(context: HANDLE, packet_list: Option<TransportPacketList>) -> Self {
        Self {
            kind: ClassifyDeferKind::Initial(context),
            packet_list,
        }
    }

    fn reauthorization(packet_list: Option<TransportPacketList>) -> Self {
        Self {
            kind: ClassifyDeferKind::Reauthorization,
            packet_list,
        }
    }

    /// Completes an ALE operation or returns a saved reauthorization packet.
    ///
    /// This method is callable from a classify callback, including at
    /// DISPATCH_LEVEL. Reauthorization is deliberately only represented here;
    /// the caller must invoke the WFP management operation from a
    /// PASSIVE_LEVEL path after this method returns.
    pub fn complete(self, inject_packet: bool) -> Result<Option<TransportPacketList>, String> {
        match self.kind {
            ClassifyDeferKind::Initial(context) => {
                // An inbound packet that will be injected from
                // ALE_AUTH_RECV_ACCEPT must also be supplied to
                // FwpsCompleteOperation. A denied packet is completed with a
                // null NBL and the owned clone is discarded by the caller.
                let nbl = if inject_packet {
                    self.packet_list
                        .as_ref()
                        .map(|packet| packet.net_buffer_list.nbl)
                        .unwrap_or(core::ptr::null_mut())
                } else {
                    core::ptr::null_mut()
                };
                // SAFETY: Only `CalloutData::pend_operation` can construct this
                // variant, and it stores the non-null context returned by
                // `FwpsPendOperation0`. This value owns that context until this
                // one completion call.
                unsafe { FwpsCompleteOperation0(context, nbl) };
                Ok(self.packet_list)
            }
            ClassifyDeferKind::Reauthorization => Ok(self.packet_list),
        }
    }

    /// Returns whether completing this value requires a PASSIVE_LEVEL WFP
    /// management operation to reauthorize existing flows.
    pub fn is_reauthorization(&self) -> bool {
        matches!(self.kind, ClassifyDeferKind::Reauthorization)
    }

    /// Returns the saved packet list, if this defer carries one.
    pub fn packet_list(&self) -> Option<&TransportPacketList> {
        self.packet_list.as_ref()
    }
}

pub(super) struct CalloutDataParts<'a> {
    pub layer: Layer,
    pub layer_id: u16,
    pub callout_id: u32,
    pub filter_id: u64,
    pub flow_context: u64,
    pub values: &'a [Value],
    pub metadata: &'a FwpsIncomingMetadataValues,
    pub classify_out: &'a mut ClassifyOut,
    pub classify_context: *const c_void,
    pub layer_data: *mut c_void,
}

/// Borrowed access to one WFP classify indication.
///
/// Only the validated classify trampoline can construct this type. Its native
/// pointers and borrowed views remain valid only for the callback lifetime `'a`;
/// methods that expose a raw layer-data pointer do not extend that lifetime.
pub struct CalloutData<'a> {
    layer: Layer,
    layer_id: u16,
    callout_id: u32,
    filter_id: u64,
    flow_context: u64,
    values: &'a [Value],
    metadata: &'a FwpsIncomingMetadataValues,
    classify_out: &'a mut ClassifyOut,
    classify_context: *const c_void,
    layer_data: *mut c_void,
    provisional_block: bool,
}

impl<'a> CalloutData<'a> {
    pub(super) fn from_parts(parts: CalloutDataParts<'a>) -> Self {
        Self {
            layer: parts.layer,
            layer_id: parts.layer_id,
            callout_id: parts.callout_id,
            filter_id: parts.filter_id,
            flow_context: parts.flow_context,
            values: parts.values,
            metadata: parts.metadata,
            classify_out: parts.classify_out,
            classify_context: parts.classify_context,
            layer_data: parts.layer_data,
            provisional_block: false,
        }
    }

    pub fn get_value_type(&self, index: usize) -> ValueType {
        self.values
            .get(index)
            .map(|value| value.value_type)
            .unwrap_or(ValueType::FwpEmpty)
    }

    pub fn get_value_u8(&self, index: usize) -> u8 {
        let Some(value) = self.values.get(index) else {
            return 0;
        };
        if value.value_type != ValueType::FwpUint8 {
            return 0;
        }

        unsafe { value.value.uint8 }
    }

    pub fn get_value_u16(&self, index: usize) -> u16 {
        let Some(value) = self.values.get(index) else {
            return 0;
        };
        if value.value_type != ValueType::FwpUint16 {
            return 0;
        }

        unsafe { value.value.uint16 }
    }

    pub fn get_value_u32(&self, index: usize) -> u32 {
        let Some(value) = self.values.get(index) else {
            return 0;
        };
        if value.value_type != ValueType::FwpUint32 {
            return 0;
        }

        unsafe { value.value.uint32 }
    }

    pub fn get_value_byte_array16(&self, index: usize) -> &[u8; 16] {
        static EMPTY: [u8; 16] = [0; 16];

        let Some(value) = self.values.get(index) else {
            return &EMPTY;
        };
        if value.value_type != ValueType::FwpByteArray16Type {
            return &EMPTY;
        }

        unsafe { value.value.byte_array16.as_ref().unwrap_or(&EMPTY) }
    }

    pub fn get_flow_handle(&self) -> Option<u64> {
        self.metadata.get_flow_handle()
    }

    pub fn has_flow_context(&self) -> bool {
        self.flow_context != 0
    }

    pub fn get_layer(&self) -> Layer {
        self.layer
    }

    pub fn get_layer_id(&self) -> u16 {
        self.layer_id
    }

    pub fn get_callout_id(&self) -> u32 {
        self.callout_id
    }

    /// Associates a non-zero driver-owned context with the current WFP flow.
    /// On success WFP owns the association and returns the same value to the
    /// callout's flowDeleteFn. The caller retains ownership when this fails.
    pub fn associate_flow_context(&self, flow_context: u64) -> Result<(), String> {
        if flow_context == 0 {
            return Err("flow context must not be zero".to_string());
        }
        if self.has_flow_context() {
            return Err("flow already has a context for this callout".to_string());
        }
        let Some(flow_id) = self.get_flow_handle().filter(|flow_id| *flow_id != 0) else {
            return Err("flow handle metadata is missing or zero".to_string());
        };

        let status = unsafe {
            FwpsFlowAssociateContext0(flow_id, self.layer_id, self.callout_id, flow_context)
        };
        check_ntstatus(status)
    }

    pub fn get_process_id(&self) -> Option<u64> {
        self.metadata.get_process_id()
    }

    pub fn get_process_path(&self) -> Option<String> {
        unsafe { self.metadata.get_process_path() }
    }

    pub fn get_transport_endpoint_handle(&self) -> Option<u64> {
        self.metadata.get_transport_endpoint_handle()
    }

    pub fn get_parent_endpoint_handle(&self) -> Option<u64> {
        self.metadata.get_parent_endpoint_handle()
    }

    pub fn get_remote_scope_id(&self) -> Option<SCOPE_ID> {
        self.metadata.get_remote_scope_id()
    }

    pub fn get_control_data(&self) -> Option<&[u8]> {
        unsafe { self.metadata.get_control_data() }
    }

    pub fn get_layer_data(&self) -> *mut c_void {
        return self.layer_data;
    }

    pub fn get_stream_callout_packet(&mut self) -> Option<&mut StreamCalloutIoPacket> {
        match self.layer {
            Layer::StreamV4 | Layer::StreamV4Discard | Layer::StreamV6 | Layer::StreamV6Discard => unsafe {
                (self.layer_data as *mut StreamCalloutIoPacket).as_mut()
            },
            _ => None,
        }
    }

    pub fn is_fragment_data(&self) -> bool {
        self.metadata.is_fragment_data()
    }

    /// Size of the IP header for this indication, if WFP provided it.
    pub fn get_ip_header_size(&self) -> Option<u32> {
        self.metadata.get_ip_header_size()
    }

    /// Size of the transport header for this indication, if WFP provided it.
    pub fn get_transport_header_size(&self) -> Option<u32> {
        self.metadata.get_transport_header_size()
    }

    /// Routing compartment for this indication, if WFP provided it.
    pub fn get_compartment_id(&self) -> Option<u32> {
        self.metadata.get_compartment_id()
    }

    /// Direction of the packet that triggered ALE reauthorization.
    ///
    /// WFP omits this metadata for an ordinary ALE authorization; callers must
    /// then infer direction from the connect or receive/accept layer.
    pub fn get_packet_direction(&self) -> Option<PacketDirection> {
        self.metadata.get_packet_direction()
    }

    /// Pends this complete WFP classification and owns the acquired handle until
    /// [`ClassifyPend`] is completed or dropped.
    ///
    /// Unlike [`Self::pend_operation`], this preserves the lifetime of the classify
    /// itself. WFP documents this mechanism for ALE endpoint closure so packet work
    /// already queued by a callout can finish before endpoint shutdown.
    pub fn pend_classify(&mut self) -> Result<ClassifyPend, String> {
        if self.classify_context.is_null() {
            return Err("classification context is missing".to_string());
        }
        if self.filter_id == 0 {
            return Err("classification filter ID is missing".to_string());
        }

        let mut handle = 0;
        // SAFETY: the validated callout trampoline supplied both pointers for this
        // callback. These native calls consume them synchronously; after a
        // successful pend, only the returned numeric handle and classifyOut copy
        // escape the callback lifetime.
        let status =
            unsafe { FwpsAcquireClassifyHandle0(self.classify_context.cast_mut(), 0, &mut handle) };
        check_ntstatus(status)
            .map_err(|err| alloc::format!("failed to acquire classify handle: {}", err))?;
        if handle == 0 {
            // SAFETY: acquire reported success, so its output owns one local
            // reference even though the returned identifier violates the contract.
            unsafe { FwpsReleaseClassifyHandle0(handle) };
            return Err("WFP returned a zero classify handle".to_string());
        }

        // SAFETY: the acquired handle belongs to this current classification;
        // filter_id and classify_out came from the same validated callback.
        let status = unsafe { FwpsPendClassify0(handle, self.filter_id, 0, self.classify_out) };
        if let Err(err) = check_ntstatus(status) {
            // SAFETY: pending failed, so this remains the sole acquired handle
            // reference and must be released without a completion call.
            unsafe { FwpsReleaseClassifyHandle0(handle) };
            return Err(alloc::format!("failed to pend classification: {}", err));
        }

        Ok(ClassifyPend {
            handle,
            classify_out: *self.classify_out,
        })
    }

    pub fn pend_operation(
        &mut self,
        packet_list: Option<TransportPacketList>,
    ) -> Result<ClassifyDefer, String> {
        let mut completion_context: HANDLE = core::ptr::null_mut();
        if let Some(completion_handle) = self.metadata.get_completion_handle() {
            let status = unsafe { FwpsPendOperation0(completion_handle, &mut completion_context) };
            check_ntstatus(status)?;
            if completion_context.is_null() {
                return Err("WFP returned a null completion context".to_string());
            }

            return Ok(ClassifyDefer::initial(completion_context, packet_list));
        }

        Err("callout not supported".to_string())
    }

    pub fn pend_filter_rest(&mut self, packet_list: Option<TransportPacketList>) -> ClassifyDefer {
        ClassifyDefer::reauthorization(packet_list)
    }

    pub fn action_permit(&mut self) {
        self.provisional_block = false;
        self.classify_out.action_permit();
        self.classify_out.clear_absorb_flag();
    }

    pub fn action_continue(&mut self) {
        self.provisional_block = false;
        self.classify_out.action_continue();
        self.classify_out.clear_absorb_flag();
    }

    // Block action and clear the write flag.
    // This will block the packet and prevent next filter in the chain to change the action.
    pub fn action_block_hard(&mut self) {
        self.provisional_block = false;
        self.classify_out.action_block();
        self.classify_out.clear_absorb_flag();
        // Next filter in the chain will not change the action.
        self.classify_out.clear_write_flag();
    }

    pub fn action_none(&mut self) {
        self.provisional_block = false;
        self.classify_out.set_none();
        self.classify_out.clear_absorb_flag();
    }

    /// Sets a fail-closed provisional action while the callback is still choosing
    /// whether this indication should be permitted. Unlike `block_and_absorb`, it
    /// does not clear the action-write right, so a later `action_permit` remains a
    /// valid decision for this same callback.
    pub fn set_default_block_and_absorb(&mut self) {
        self.provisional_block = true;
        self.classify_out.action_block();
        self.classify_out.set_absorb();
    }

    /// Blocks and absorbs the indication as a final decision. The write right must
    /// be cleared for clone/drop/reinject and for every other hard block, otherwise
    /// a lower-priority filter can replace this action with permit.
    pub fn block_and_absorb(&mut self) {
        self.provisional_block = false;
        self.classify_out.action_block();
        self.classify_out.set_absorb();
        self.classify_out.clear_write_flag();
    }
    pub fn clear_write_flag(&mut self) {
        self.provisional_block = false;
        self.classify_out.clear_write_flag();
    }

    pub fn is_reauthorize(&self, flags_index: usize) -> bool {
        self.get_value_u32(flags_index) & FWP_CONDITION_FLAG_IS_REAUTHORIZE != 0
    }

    /// Returns true if WFP indicated this packet as a reassembled datagram, i.e.
    /// the individual fragments have been merged back into one packet with a
    /// complete transport header.
    ///
    /// `get_value_u32` returns zero for both a missing and a mistyped field, so one
    /// bounds/type check is sufficient and neither case can look reassembled.
    pub fn is_reassembled(&self, flags_index: usize) -> bool {
        self.get_value_u32(flags_index) & FWP_CONDITION_FLAG_IS_REASSEMBLED != 0
    }
}

impl Drop for CalloutData<'_> {
    fn drop(&mut self) {
        // A classify callback starts with a provisional fail-closed action. If
        // control returns without an explicit final action, make that default
        // block hard so an early error/return cannot let a lower filter permit it.
        if self.provisional_block {
            self.classify_out.clear_write_flag();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{CalloutData, CalloutDataParts, ClassifyOut, FwpsIncomingMetadataValues, Layer};

    fn empty_metadata() -> FwpsIncomingMetadataValues {
        // The native metadata mirror consists only of integer fields, C unions and
        // pointers; an all-zero value means that no optional metadata is present.
        unsafe { core::mem::zeroed() }
    }

    fn callout_data<'a>(
        classify_out: &'a mut ClassifyOut,
        metadata: &'a FwpsIncomingMetadataValues,
    ) -> CalloutData<'a> {
        CalloutData::from_parts(CalloutDataParts {
            layer: Layer::OutboundIppacketV4,
            layer_id: 0,
            callout_id: 0,
            filter_id: 0,
            flow_context: 0,
            values: &[],
            metadata,
            classify_out,
            classify_context: core::ptr::null(),
            layer_data: core::ptr::null_mut(),
        })
    }

    #[test]
    fn final_non_absorb_block_is_hard_immediately() {
        let metadata = empty_metadata();
        let mut classify_out = ClassifyOut::test_writable();
        {
            let mut data = callout_data(&mut classify_out, &metadata);
            data.action_block_hard();
        }

        assert!(classify_out.test_is_block());
        assert!(!classify_out.test_absorbs());
        assert!(!classify_out.can_set_action());
    }

    #[test]
    fn final_absorb_block_is_hard_immediately() {
        let metadata = empty_metadata();
        let mut classify_out = ClassifyOut::test_writable();
        {
            let mut data = callout_data(&mut classify_out, &metadata);
            data.block_and_absorb();
        }

        assert!(classify_out.test_is_block());
        assert!(classify_out.test_absorbs());
        assert!(!classify_out.can_set_action());
    }

    #[test]
    fn unresolved_default_block_becomes_hard_on_return() {
        let metadata = empty_metadata();
        let mut classify_out = ClassifyOut::test_writable();
        {
            let mut data = callout_data(&mut classify_out, &metadata);
            data.set_default_block_and_absorb();
        }

        assert!(classify_out.test_is_block());
        assert!(classify_out.test_absorbs());
        assert!(!classify_out.can_set_action());
    }

    #[test]
    fn explicit_permit_replaces_default_without_becoming_hard() {
        let metadata = empty_metadata();
        let mut classify_out = ClassifyOut::test_writable();
        {
            let mut data = callout_data(&mut classify_out, &metadata);
            data.set_default_block_and_absorb();
            data.action_permit();
        }

        assert!(classify_out.test_is_permit());
        assert!(!classify_out.test_absorbs());
        assert!(classify_out.can_set_action());
    }
}
