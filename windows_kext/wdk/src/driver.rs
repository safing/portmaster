use windows_sys::{
    Wdk::Foundation::{DEVICE_OBJECT, DRIVER_DISPATCH, DRIVER_OBJECT, DRIVER_UNLOAD},
    Win32::Foundation::HANDLE,
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

    pub fn set_driver_unload(&mut self, driver_unload: DRIVER_UNLOAD) {
        if let Some(driver) = unsafe { self.driver_object.as_mut() } {
            driver.DriverUnload = driver_unload
        }
    }

    pub fn set_read_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(windows_sys::Wdk::System::SystemServices::IRP_MJ_READ, mj_fn);
    }

    pub fn set_write_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_WRITE,
            mj_fn,
        );
    }

    pub fn set_create_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CREATE,
            mj_fn,
        );
    }

    pub fn set_device_control_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_DEVICE_CONTROL,
            mj_fn,
        );
    }

    pub fn set_close_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CLOSE,
            mj_fn,
        );
    }

    pub fn set_cleanup_fn(&mut self, mj_fn: DRIVER_DISPATCH) {
        self.set_major_fn(
            windows_sys::Wdk::System::SystemServices::IRP_MJ_CLEANUP,
            mj_fn,
        );
    }

    fn set_major_fn(&mut self, fn_index: u32, mj_fn: DRIVER_DISPATCH) {
        if let Some(driver) = unsafe { self.driver_object.as_mut() } {
            driver.MajorFunction[fn_index as usize] = mj_fn
        }
    }
}
