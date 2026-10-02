use core::ffi::c_void;
use windows_sys::Win32::Foundation::HANDLE;

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
    _driver_handle: HANDLE,
    device_handle: HANDLE,
    device_object: *mut c_void,
}

impl Driver {
    /// Wraps the handles created for this driver's KMDF control device.
    ///
    /// # Safety
    ///
    /// Both handles must be live KMDF objects of the expected types, and
    /// `device_handle` must continue to identify its WDFDEVICE for every use of
    /// the returned wrapper. The caller must follow KMDF's teardown ordering.
    pub(crate) unsafe fn new(driver_handle: HANDLE, device_handle: HANDLE) -> Driver {
        Driver {
            _driver_handle: driver_handle,
            device_handle,
            // SAFETY: The caller guarantees that this is a live WDFDEVICE.
            device_object: unsafe { interface::wdf_device_wdm_get_device_object(device_handle) },
        }
    }

    /// Enables delivery to the control device after all driver-owned state has
    /// been initialized and published.
    pub fn finish_initialization(&self) {
        unsafe {
            crate::ffi::pm_FinishControlDeviceInitialization(self.device_handle);
        }
    }

    pub fn get_device_object(&self) -> *mut c_void {
        return self.device_object;
    }

}
