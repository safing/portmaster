use alloc::string::String;
use ntstatus::ntstatus::NtStatus;

use crate::{ffi::FwpsFlowRemoveContext0, utils::check_ntstatus};

/// Outcome of removing a context from a WFP flow.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RemoveContextResult {
    /// WFP removed the context and called flowDeleteFn synchronously.
    Removed,
    /// WFP will call flowDeleteFn after an active classification returns.
    Pending,
    /// The flow/context was already gone, so no callback will reclaim it.
    AlreadyGone,
}

/// Requests removal of one context previously associated with a WFP flow.
pub fn remove_context(
    flow_id: u64,
    layer_id: u16,
    callout_id: u32,
) -> Result<RemoveContextResult, String> {
    let status = unsafe { FwpsFlowRemoveContext0(flow_id, layer_id, callout_id) };
    match NtStatus::try_from(status as u32) {
        Ok(NtStatus::STATUS_SUCCESS) => Ok(RemoveContextResult::Removed),
        Ok(NtStatus::STATUS_PENDING) => Ok(RemoveContextResult::Pending),
        // The flow can terminate between taking the registry snapshot and this
        // call. In that case WFP already removed the context and there is no
        // flowDeleteFn callback left to reclaim our allocation.
        Ok(NtStatus::STATUS_UNSUCCESSFUL) => Ok(RemoveContextResult::AlreadyGone),
        _ => check_ntstatus(status).map(|()| RemoveContextResult::Removed),
    }
}
