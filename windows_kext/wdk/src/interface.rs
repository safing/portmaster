use crate::{
    alloc::borrow::ToOwned,
    driver::Driver,
    ffi::{
        pm_GetDeviceObject, pm_InitDriverObject, pm_WdfObjectGetTypedContextWorker,
        WdfObjectAttributes, WdfObjectContextTypeInfo,
    },
    utils::check_ntstatus,
};
use alloc::ffi::CString;
use alloc::format;
use alloc::string::String;
use widestring::U16CString;
use windows_sys::{
    Wdk::{
        Foundation::{DEVICE_OBJECT, DRIVER_OBJECT},
        System::SystemServices::DbgPrint,
    },
    Win32::Foundation::{HANDLE, INVALID_HANDLE_VALUE, UNICODE_STRING},
};

// Debug
pub fn dbg_print(str: String) {
    if let Ok(c_str) = CString::new(str) {
        unsafe {
            DbgPrint(c_str.as_ptr() as _);
        }
    }
}

pub fn init_driver_object(
    driver_object: *mut DRIVER_OBJECT,
    registry_path: *mut UNICODE_STRING,
    driver_name: &str,
    object_attributes: *mut WdfObjectAttributes,
) -> Result<Driver, String> {
    let win_driver_path = format!("\\Device\\{}", driver_name);
    let dos_driver_path = format!("\\??\\{}", driver_name);

    let mut wdf_driver_handle = INVALID_HANDLE_VALUE;
    let mut wdf_device_handle = INVALID_HANDLE_VALUE;

    let Ok(win_driver) = U16CString::from_str(win_driver_path) else {
        return Err("Invalid argument win_driver_path".to_owned());
    };
    let Ok(dos_driver) = U16CString::from_str(dos_driver_path) else {
        return Err("Invalid argument dos_driver_path".to_owned());
    };

    unsafe {
        let status = pm_InitDriverObject(
            driver_object,
            registry_path,
            &mut wdf_driver_handle,
            &mut wdf_device_handle,
            win_driver.as_ptr(),
            dos_driver.as_ptr(),
            object_attributes,
            empty_wdf_driver_unload,
        );

        check_ntstatus(status)?;

        return Ok(Driver::new(
            driver_object,
            wdf_driver_handle,
            wdf_device_handle,
        ));
    }
}

pub fn get_device_context_from_wdf_device<T>(
    wdf_device: HANDLE,
    type_info: &'static WdfObjectContextTypeInfo,
) -> *mut T {
    unsafe {
        return core::mem::transmute(pm_WdfObjectGetTypedContextWorker(wdf_device, type_info));
    }
}

pub(crate) fn wdf_device_wdm_get_device_object(wdf_device: HANDLE) -> *mut DEVICE_OBJECT {
    unsafe {
        return pm_GetDeviceObject(wdf_device);
    }
}

pub fn get_device_context_from_device_object<'a, T>(
    device_object: &mut DEVICE_OBJECT,
) -> Result<&'a mut T, ()> {
    unsafe {
        if let Some(context) = device_object.DeviceExtension.as_mut() {
            return Ok(core::mem::transmute(context));
        }
    }

    return Err(());
}

/// Empty unload event
extern "C" fn empty_wdf_driver_unload(_driver: HANDLE) {}
