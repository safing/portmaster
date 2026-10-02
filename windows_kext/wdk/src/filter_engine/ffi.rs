use crate::alloc::borrow::ToOwned;
use crate::ffi::FwpsCalloutClassifyFn;
use crate::ffi::{
    FwpsCalloutRegister3, FwpsCalloutUnregisterById0, FwpsCalloutUnregisterByKey0, FWPS_CALLOUT3,
    FWPS_FILTER3,
};
use crate::utils::check_ntstatus;
use alloc::string::String;
use ntstatus::ntstatus::NtStatus;

use core::ptr;
use core::{ffi::c_void, mem::MaybeUninit};
use widestring::U16CString;

use windows_sys::Win32::Foundation::{NTSTATUS, STATUS_SUCCESS};
use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::{
    FwpmCalloutAdd0, FwpmEngineClose0, FwpmEngineOpen0, FwpmFilterAdd0, FwpmFilterDeleteById0,
    FwpmSubLayerAdd0, FwpmSubLayerDeleteByKey0, FwpmTransactionAbort0, FwpmTransactionBegin0,
    FwpmTransactionCommit0, FWPM_CALLOUT0, FWPM_CALLOUT_FLAG_USES_PROVIDER_CONTEXT,
    FWPM_DISPLAY_DATA0, FWPM_FILTER0, FWPM_SESSION0, FWPM_SESSION_FLAG_DYNAMIC, FWPM_SUBLAYER0,
    FWP_UINT8,
};
use windows_sys::Win32::System::Rpc::RPC_C_AUTHN_WINNT;
use windows_sys::{
    core::GUID,
    Win32::Foundation::{HANDLE, INVALID_HANDLE_VALUE},
};

use super::layer::Layer;

// Management objects are zero-initialized in Rust and passed by value to FWPM,
// so both their complete x64 size and every populated field offset must match
// the WDK rather than merely sharing a compatible prefix.
#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, offset_of, size_of};
    use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::{
        FWPM_ACTION0, FWP_BYTE_BLOB, FWP_CONDITION_VALUE0, FWP_VALUE0,
    };

    assert!(size_of::<FWP_BYTE_BLOB>() == 16);
    assert!(align_of::<FWP_BYTE_BLOB>() == 8);
    assert!(offset_of!(FWP_BYTE_BLOB, size) == 0);
    assert!(offset_of!(FWP_BYTE_BLOB, data) == 8);
    assert!(size_of::<FWP_VALUE0>() == 16);
    assert!(align_of::<FWP_VALUE0>() == 8);
    assert!(offset_of!(FWP_VALUE0, r#type) == 0);
    assert!(offset_of!(FWP_VALUE0, Anonymous) == 8);
    assert!(size_of::<FWP_CONDITION_VALUE0>() == 16);
    assert!(align_of::<FWP_CONDITION_VALUE0>() == 8);
    assert!(offset_of!(FWP_CONDITION_VALUE0, r#type) == 0);
    assert!(offset_of!(FWP_CONDITION_VALUE0, Anonymous) == 8);
    assert!(size_of::<FWPM_DISPLAY_DATA0>() == 16);
    assert!(align_of::<FWPM_DISPLAY_DATA0>() == 8);
    assert!(offset_of!(FWPM_DISPLAY_DATA0, name) == 0);
    assert!(offset_of!(FWPM_DISPLAY_DATA0, description) == 8);

    assert!(size_of::<FWPM_SESSION0>() == 72);
    assert!(align_of::<FWPM_SESSION0>() == 8);
    assert!(offset_of!(FWPM_SESSION0, flags) == 32);

    assert!(size_of::<FWPM_SUBLAYER0>() == 72);
    assert!(align_of::<FWPM_SUBLAYER0>() == 8);
    assert!(offset_of!(FWPM_SUBLAYER0, subLayerKey) == 0);
    assert!(offset_of!(FWPM_SUBLAYER0, displayData) == 16);
    assert!(offset_of!(FWPM_SUBLAYER0, flags) == 32);
    assert!(offset_of!(FWPM_SUBLAYER0, providerKey) == 40);
    assert!(offset_of!(FWPM_SUBLAYER0, providerData) == 48);
    assert!(offset_of!(FWPM_SUBLAYER0, weight) == 64);

    assert!(size_of::<FWPM_CALLOUT0>() == 88);
    assert!(align_of::<FWPM_CALLOUT0>() == 8);
    assert!(offset_of!(FWPM_CALLOUT0, calloutKey) == 0);
    assert!(offset_of!(FWPM_CALLOUT0, displayData) == 16);
    assert!(offset_of!(FWPM_CALLOUT0, flags) == 32);
    assert!(offset_of!(FWPM_CALLOUT0, providerKey) == 40);
    assert!(offset_of!(FWPM_CALLOUT0, providerData) == 48);
    assert!(offset_of!(FWPM_CALLOUT0, applicableLayer) == 64);
    assert!(offset_of!(FWPM_CALLOUT0, calloutId) == 80);

    assert!(size_of::<FWPM_ACTION0>() == 20);
    assert!(align_of::<FWPM_ACTION0>() == 4);
    assert!(size_of::<FWPM_FILTER0>() == 200);
    assert!(align_of::<FWPM_FILTER0>() == 8);
    assert!(offset_of!(FWPM_FILTER0, layerKey) == 64);
    assert!(offset_of!(FWPM_FILTER0, subLayerKey) == 80);
    assert!(offset_of!(FWPM_FILTER0, weight) == 96);
    assert!(offset_of!(FWPM_FILTER0, numFilterConditions) == 112);
    assert!(offset_of!(FWPM_FILTER0, filterCondition) == 120);
    assert!(offset_of!(FWPM_FILTER0, action) == 128);
    assert!(offset_of!(FWPM_FILTER0, Anonymous) == 152);
    assert!(offset_of!(FWPM_FILTER0, filterId) == 176);
    assert!(offset_of!(FWPM_FILTER0, effectiveWeight) == 184);
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum UnregisterCalloutResult {
    Removed,
    Busy,
}

pub(crate) fn create_filter_engine() -> Result<HANDLE, String> {
    unsafe {
        let mut handle: HANDLE = INVALID_HANDLE_VALUE;
        let mut wdf_session: FWPM_SESSION0 = MaybeUninit::zeroed().assume_init();
        wdf_session.flags = FWPM_SESSION_FLAG_DYNAMIC;
        let status = FwpmEngineOpen0(
            core::ptr::null(),
            RPC_C_AUTHN_WINNT,
            core::ptr::null_mut(),
            &wdf_session,
            &mut handle,
        );
        check_ntstatus(status as i32)?;
        if handle.is_null() || handle == INVALID_HANDLE_VALUE {
            return Err("filter engine returned an invalid session handle".to_owned());
        }

        return Ok(handle);
    }
}

pub(crate) fn register_sublayer(
    filter_engine_handle: HANDLE,
    name: &str,
    description: &str,
    guid: u128,
) -> Result<(), String> {
    let Ok(name) = U16CString::from_str(name) else {
        return Err("invalid argument name".to_owned());
    };
    let Ok(description) = U16CString::from_str(description) else {
        return Err("invalid argument description".to_owned());
    };

    unsafe {
        let mut sublayer: FWPM_SUBLAYER0 = MaybeUninit::zeroed().assume_init();
        sublayer.subLayerKey = GUID::from_u128(guid);
        sublayer.displayData.name = name.as_ptr() as _;
        sublayer.displayData.description = description.as_ptr() as _;
        sublayer.flags = 0;
        sublayer.weight = 0xFFFF; // Set to Max value. Weight compared to other sublayers.

        let status = FwpmSubLayerAdd0(filter_engine_handle, &sublayer, core::ptr::null_mut());
        check_ntstatus(status as i32)?;

        return Ok(());
    }
}

pub(crate) fn unregister_sublayer(filter_engine_handle: HANDLE, guid: u128) -> Result<(), String> {
    let guid = GUID::from_u128(guid);
    unsafe {
        let status = FwpmSubLayerDeleteByKey0(filter_engine_handle, ptr::addr_of!(guid));
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}

unsafe extern "system" fn generic_notify(
    _notify_type: u32,
    _filter_key: *const GUID,
    _filter: *mut FWPS_FILTER3,
) -> NTSTATUS {
    // The notify function is also a driver-code callback. Keep the image alive
    // while WFP removes the dynamic filters and while runtime callouts are
    // unregistered. It does not need Device access or any filter contents.
    let _callback_admission = crate::callback_barrier::CALLBACK_BARRIER.enter_callback();
    STATUS_SUCCESS
}

/// Registers a runtime callout against a native WDM device object.
///
/// # Safety
///
/// `device_object` must identify a live device object belonging to this driver
/// and remain valid until the callout is unregistered. All supplied callback
/// functions and the driver image containing them must remain executable for the
/// same interval.
pub(crate) unsafe fn register_runtime_callout(
    device_object: *mut c_void,
    guid: u128,
    callout_fn: FwpsCalloutClassifyFn,
    flow_delete_fn: Option<crate::ffi::FwpsCalloutFlowDeleteNotifyFn>,
) -> Result<u32, String> {
    let s_callout = FWPS_CALLOUT3 {
        calloutKey: GUID::from_u128(guid),
        flags: 0,
        classifyFn: Some(callout_fn),
        notifyFn: Some(generic_notify),
        flowDeleteFn: flow_delete_fn,
    };

    unsafe {
        let mut callout_id: u32 = 0;
        let status = FwpsCalloutRegister3(device_object as _, &s_callout, &mut callout_id);
        check_ntstatus(status)?;
        Ok(callout_id)
    }
}

pub(crate) fn register_management_callout(
    filter_engine_handle: HANDLE,
    guid: u128,
    layer: Layer,
    name: &str,
    description: &str,
) -> Result<(), String> {
    callout_add(filter_engine_handle, guid, layer, name, description)
}

fn callout_add(
    filter_engine_handle: HANDLE,
    guid: u128,
    layer: Layer,
    name: &str,
    description: &str,
) -> Result<(), String> {
    let Ok(name) = U16CString::from_str(name) else {
        return Err("invalid argument name".to_owned());
    };
    let Ok(description) = U16CString::from_str(description) else {
        return Err("invalid argument description".to_owned());
    };
    let display_data = FWPM_DISPLAY_DATA0 {
        name: name.as_ptr() as _,
        description: description.as_ptr() as _,
    };

    unsafe {
        let mut callout: FWPM_CALLOUT0 = MaybeUninit::zeroed().assume_init();
        callout.calloutKey = GUID::from_u128(guid);
        callout.displayData = display_data;
        callout.applicableLayer = layer.get_guid();
        callout.flags = FWPM_CALLOUT_FLAG_USES_PROVIDER_CONTEXT;
        let status = FwpmCalloutAdd0(
            filter_engine_handle,
            &callout,
            core::ptr::null_mut(),
            core::ptr::null_mut(),
        );
        check_ntstatus(status as i32)?;
    };
    return Ok(());
}

pub(crate) fn unregister_callout(
    callout_id: u32,
    callout_key: u128,
) -> Result<UnregisterCalloutResult, String> {
    unsafe {
        // WFP normally returns a non-zero runtime ID, but the API contract does
        // not make zero an explicit impossible value. Keep a key-based fallback
        // so a successful registration can never become untracked merely because
        // its output ID is zero.
        let status = if callout_id != 0 {
            FwpsCalloutUnregisterById0(callout_id)
        } else {
            let key = GUID::from_u128(callout_key);
            FwpsCalloutUnregisterByKey0(ptr::addr_of!(key))
        };
        match NtStatus::try_from(status as u32) {
            Ok(NtStatus::STATUS_SUCCESS) | Ok(NtStatus::STATUS_FWP_CALLOUT_NOT_FOUND) => {
                Ok(UnregisterCalloutResult::Removed)
            }
            Ok(NtStatus::STATUS_DEVICE_BUSY) => Ok(UnregisterCalloutResult::Busy),
            _ => check_ntstatus(status).map(|()| UnregisterCalloutResult::Removed),
        }
    }
}

pub(crate) fn register_filter(
    filter_engine_handle: HANDLE,
    sublayer_guid: u128,
    name: &str,
    description: &str,
    callout_guid: u128,
    layer: Layer,
    action: u32,
    context: u64,
) -> Result<u64, String> {
    let Ok(name) = U16CString::from_str(name) else {
        return Err("invalid argument name".to_owned());
    };
    let Ok(description) = U16CString::from_str(description) else {
        return Err("invalid argument description".to_owned());
    };
    let mut filter_id: u64 = 0;
    unsafe {
        let mut filter: FWPM_FILTER0 = MaybeUninit::zeroed().assume_init();
        filter.displayData.name = name.as_ptr() as _;
        filter.displayData.description = description.as_ptr() as _;
        filter.action.r#type = action; // Says this filter's callout MUST make a block/permit decision. Also see doc excerpts below.
        filter.subLayerKey = GUID::from_u128(sublayer_guid);
        filter.weight.r#type = FWP_UINT8;
        filter.weight.Anonymous.uint8 = 15; // The weight of this filter within its sublayer

        // Ordinary permits remain overridable. Block helpers clear
        // FWPS_RIGHT_ACTION_WRITE explicitly, and the narrow Portmaster-owner
        // permit path does the same when it intentionally needs a hard permit.
        filter.flags = 0;
        filter.numFilterConditions = 0; // If you specify 0, this filter invokes its callout for all traffic in its layer
        filter.layerKey = layer.get_guid(); // This layer must match the layer that ExampleCallout is registered to
        filter.action.Anonymous.calloutKey = GUID::from_u128(callout_guid);
        filter.Anonymous.rawContext = context;
        let status = FwpmFilterAdd0(
            filter_engine_handle,
            &filter,
            core::ptr::null_mut(),
            &mut filter_id,
        );

        check_ntstatus(status as i32)?;
        if filter_id == 0 {
            return Err("WFP returned a zero filter ID".to_owned());
        }

        return Ok(filter_id);
    }
}

pub(crate) fn unregister_filter(
    filter_engine_handle: HANDLE,
    filter_id: u64,
) -> Result<(), String> {
    unsafe {
        let status = FwpmFilterDeleteById0(filter_engine_handle, filter_id);
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}

pub(crate) fn filter_engine_close(filter_engine_handle: HANDLE) -> Result<(), String> {
    unsafe {
        let status = FwpmEngineClose0(filter_engine_handle);
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}

pub(crate) fn filter_engine_transaction_begin(
    filter_engine_handle: HANDLE,
    flags: u32,
) -> Result<(), String> {
    unsafe {
        let status = FwpmTransactionBegin0(filter_engine_handle, flags);
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}

pub(crate) fn filter_engine_transaction_commit(filter_engine_handle: HANDLE) -> Result<(), String> {
    unsafe {
        let status = FwpmTransactionCommit0(filter_engine_handle);
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}

pub(crate) fn filter_engine_transaction_abort(filter_engine_handle: HANDLE) -> Result<(), String> {
    unsafe {
        let status = FwpmTransactionAbort0(filter_engine_handle);
        check_ntstatus(status as i32)?;
        return Ok(());
    }
}
