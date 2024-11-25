use crate::alloc::borrow::ToOwned;
use crate::ffi::FwpsCalloutClassifyFn;
use crate::ffi::{FwpsCalloutRegister3, FwpsCalloutUnregisterById0, FWPS_CALLOUT3, FWPS_FILTER2};
use crate::utils::check_ntstatus;
use alloc::string::String;

use core::mem::MaybeUninit;
use core::ptr;
use widestring::U16CString;

use windows_sys::Wdk::Foundation::DEVICE_OBJECT;
use windows_sys::Win32::Foundation::{NTSTATUS, STATUS_SUCCESS};
use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::{
    FwpmCalloutAdd0, FwpmEngineClose0, FwpmEngineOpen0, FwpmFilterAdd0, FwpmFilterDeleteById0,
    FwpmSubLayerAdd0, FwpmSubLayerDeleteByKey0, FwpmTransactionAbort0, FwpmTransactionBegin0,
    FwpmTransactionCommit0, FWPM_CALLOUT0, FWPM_CALLOUT_FLAG_USES_PROVIDER_CONTEXT,
    FWPM_DISPLAY_DATA0, FWPM_FILTER0, FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT, FWPM_SESSION0,
    FWPM_SESSION_FLAG_DYNAMIC, FWPM_SUBLAYER0, FWP_UINT8,
};
use windows_sys::Win32::System::Rpc::RPC_C_AUTHN_WINNT;
use windows_sys::{
    core::GUID,
    Win32::Foundation::{HANDLE, INVALID_HANDLE_VALUE},
};

use super::layer::Layer;

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

unsafe extern "C" fn generic_notify(
    _notify_type: u32,
    _filter_key: *const GUID,
    _filter: *mut FWPS_FILTER2,
) -> NTSTATUS {
    return STATUS_SUCCESS;
}

unsafe extern "C" fn generic_delete_notify(_layer_id: u16, _callout_id: u32, _flow_context: u64) {}

pub(crate) fn register_callout(
    device_object: *mut DEVICE_OBJECT,
    filter_engine_handle: HANDLE,
    name: &str,
    description: &str,
    guid: u128,
    layer: Layer,
    callout_fn: FwpsCalloutClassifyFn,
) -> Result<u32, String> {
    let s_callout = FWPS_CALLOUT3 {
        calloutKey: GUID::from_u128(guid),
        flags: 0,
        classifyFn: Some(callout_fn),
        notifyFn: Some(generic_notify),
        flowDeleteFn: Some(generic_delete_notify),
    };

    unsafe {
        let mut callout_id: u32 = 0;
        let status = FwpsCalloutRegister3(device_object as _, &s_callout, &mut callout_id);

        check_ntstatus(status)?;

        callout_add(filter_engine_handle, guid, layer, name, description)?;

        return Ok(callout_id);
    }
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

pub(crate) fn unregister_callout(callout_id: u32) -> Result<(), String> {
    unsafe {
        let status = FwpsCalloutUnregisterById0(callout_id);

        check_ntstatus(status as i32)?;
        return Ok(());
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
        filter.flags = FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT;
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
