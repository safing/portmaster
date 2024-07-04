use crate::common::ControlCode;
use crate::device;
use alloc::boxed::Box;
use num_traits::FromPrimitive;
use wdk::irp_helpers::{DeviceControlRequest, ReadRequest, WriteRequest};
use wdk::{err, info, interface};
use windows_sys::Wdk::Foundation::{DEVICE_OBJECT, DRIVER_OBJECT, IRP};
use windows_sys::Win32::Foundation::{NTSTATUS, STATUS_SUCCESS};

static VERSION: [u8; 4] = include!("../../kextinterface/version.txt");

static mut DEVICE: *mut device::Device = core::ptr::null_mut();
pub fn get_device() -> Option<&'static mut device::Device> {
    return unsafe { DEVICE.as_mut() };
}

// DriverEntry is the entry point of the driver (main function). Will be called when driver is loaded.
// Name should not be changed
#[export_name = "DriverEntry"]
pub extern "system" fn driver_entry(
    driver_object: *mut windows_sys::Wdk::Foundation::DRIVER_OBJECT,
    registry_path: *mut windows_sys::Win32::Foundation::UNICODE_STRING,
) -> windows_sys::Win32::Foundation::NTSTATUS {
    info!("Starting initialization...");

    // Initialize driver object.
    let mut driver = match interface::init_driver_object(
        driver_object,
        registry_path,
        "PortmasterKext",
        core::ptr::null_mut(),
    ) {
        Ok(driver) => driver,
        Err(status) => {
            err!("driver_entry: failed to initialize driver: {}", status);
            return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
        }
    };

    // Set driver functions.
    driver.set_driver_unload(Some(driver_unload));
    driver.set_read_fn(Some(driver_read));
    driver.set_write_fn(Some(driver_write));
    driver.set_device_control_fn(Some(device_control));

    // Initialize device.
    unsafe {
        let device = match device::Device::new(&driver) {
            Ok(device) => Box::new(device),
            Err(err) => {
                wdk::err!("filed to initialize device: {}", err);
                return -1;
            }
        };
        DEVICE = Box::into_raw(device);
    }

    STATUS_SUCCESS
}

// driver_unload function is called when service delete is called from user-space.
unsafe extern "system" fn driver_unload(_object: *const DRIVER_OBJECT) {
    info!("Unloading complete");
    unsafe {
        if !DEVICE.is_null() {
            _ = Box::from_raw(DEVICE);
        }
    }
}

// driver_read event triggered from user-space on file.Read.
unsafe extern "system" fn driver_read(
    _device_object: *const DEVICE_OBJECT,
    irp: *mut IRP,
) -> NTSTATUS {
    let mut read_request = ReadRequest::new(irp.as_mut().unwrap());
    let Some(device) = get_device() else {
        read_request.complete();

        return read_request.get_status();
    };

    device.read(&mut read_request);
    read_request.get_status()
}

/// driver_write event triggered from user-space on file.Write.
unsafe extern "system" fn driver_write(
    _device_object: *const DEVICE_OBJECT,
    irp: *mut IRP,
) -> NTSTATUS {
    let mut write_request = WriteRequest::new(irp.as_mut().unwrap());
    let Some(device) = get_device() else {
        write_request.complete();
        return write_request.get_status();
    };

    device.write(&mut write_request);

    write_request.mark_all_as_read();
    write_request.complete();
    write_request.get_status()
}

/// device_control event triggered from user-space on file.deviceIOControl.
unsafe extern "system" fn device_control(
    _device_object: *const DEVICE_OBJECT,
    irp: *mut IRP,
) -> NTSTATUS {
    let mut control_request = DeviceControlRequest::new(irp.as_mut().unwrap());
    let Some(device) = get_device() else {
        control_request.complete();
        return control_request.get_status();
    };

    let Some(control_code): Option<ControlCode> =
        FromPrimitive::from_u32(control_request.get_control_code())
    else {
        wdk::info!("Unknown IOCTL code: {}", control_request.get_control_code());
        control_request.not_implemented();
        return control_request.get_status();
    };

    wdk::info!("IOCTL: {}", control_code);

    match control_code {
        ControlCode::Version => {
            control_request.write(&VERSION);
        }
        ControlCode::ShutdownRequest => device.shutdown(),
    };

    control_request.complete();
    control_request.get_status()
}
