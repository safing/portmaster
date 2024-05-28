use windows_sys::{
    Wdk::Foundation::{DEVICE_OBJECT, DRIVER_OBJECT, IRP},
    Win32::Foundation::{HANDLE, NTSTATUS},
};

use crate::{
    interface,
    irp_helpers::{ReadRequest, WriteRequest},
};

pub trait Device {
    fn new(driver: &Driver) -> Self;
    fn cleanup(&mut self);
    fn read(&mut self, read_request: &mut ReadRequest);
    fn write(&mut self, write_request: &mut WriteRequest);
    fn shutdown(&mut self);
}

pub struct Driver {
    _device_handle: HANDLE,
    driver_object: *mut DRIVER_OBJECT,
    device_object: *mut DEVICE_OBJECT,
}
unsafe impl Sync for Driver {}

// This is a workaround for current state of wdk bindings.
// TODO: replace with official version when they are correct: https://github.com/microsoft/wdkmetadata/issues/59
pub type UnloadFnType = unsafe extern "system" fn(driver_object: *const DRIVER_OBJECT);
pub type MjFnType = unsafe extern "system" fn(&mut DEVICE_OBJECT, &mut IRP) -> NTSTATUS;

impl Driver {
    pub(crate) fn new(
        driver_object: *mut DRIVER_OBJECT,
        _driver_handle: HANDLE,
        device_handle: HANDLE,
    ) -> Driver {
        return Driver {
            // driver_handle,
            _device_handle: device_handle,
            driver_object,
            device_object: interface::wdf_device_wdm_get_device_object(device_handle),
        };
    }

    pub fn get_device_object(&self) -> *mut DEVICE_OBJECT {
        return self.device_object;
    }

    pub fn get_device_object_ref(&self) -> Option<&mut DEVICE_OBJECT> {
        return unsafe { self.device_object.as_mut() };
    }

    pub fn set_driver_unload(&mut self, driver_unload: UnloadFnType) {
        if let Some(driver) = unsafe { self.driver_object.as_mut() } {
            driver.DriverUnload = Some(unsafe { core::mem::transmute(driver_unload) })
        }
    }

    pub fn set_read_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(windows_sys::Wdk::System::SystemServices::IRP_MJ_READ, mj_fn);
    }

    pub fn set_write_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_WRITE,
            mj_fn,
        );
    }

    pub fn set_create_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CREATE,
            mj_fn,
        );
    }

    pub fn set_device_control_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_DEVICE_CONTROL,
            mj_fn,
        );
    }

    pub fn set_close_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CLOSE,
            mj_fn,
        );
    }

    pub fn set_cleanup_fn(&mut self, mj_fn: MjFnType) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CLEANUP,
            mj_fn,
        );
    }

    fn set_major_fn(&mut self, fn_index: u32, mj_fn: MjFnType) {
        if let Some(driver) = unsafe { self.driver_object.as_mut() } {
            driver.MajorFunction[fn_index as usize] = Some(unsafe { core::mem::transmute(mj_fn) })
        }
    }
}
