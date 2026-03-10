use crate::common::ControlCode;
use crate::device;
use alloc::boxed::Box;
use core::sync::atomic::{AtomicPtr, Ordering};
use num_traits::FromPrimitive;
use wdk::irp_helpers::{CleanupRequest, CreateRequest, DeviceControlRequest, ReadRequest, WriteRequest};
use wdk::{err, info, interface};
use windows_sys::Wdk::Foundation::{DEVICE_OBJECT, DRIVER_OBJECT, IRP};
use windows_sys::Win32::Foundation::{NTSTATUS, STATUS_SUCCESS};

static VERSION: [u8; 4] = include!("../../kextinterface/version.txt");

/// Global device pointer.
///
/// We use `AtomicPtr` to ensure thread safety.
/// - **Safety**: Prevents data races and acts as a compiler barrier against dangerous optimizations
///   (e.g., load hoisting), ensuring concurrent callouts see a valid, up-to-date pointer.
/// - **Performance**: Negligible overhead. On x64, `Acquire` is free (same as a normal load).
///   On ARM64, it uses efficient hardware-supported load-acquire instructions.
static DEVICE: AtomicPtr<device::Device> = AtomicPtr::new(core::ptr::null_mut());

pub fn get_device() -> Option<&'static mut device::Device> {
    // Acquire pairs with the Release store in driver_entry and the AcqRel swap in driver_unload.
    unsafe { DEVICE.load(Ordering::Acquire).as_mut() }
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
    driver.set_create_fn(Some(driver_create));
    driver.set_cleanup_fn(Some(driver_cleanup));
    driver.set_read_fn(Some(driver_read));
    driver.set_write_fn(Some(driver_write));
    driver.set_device_control_fn(Some(device_control));

    // Initialize device.
    let device = match device::Device::new(&driver) {
        Ok(device) => Box::new(device),
        Err(err) => {
            wdk::err!("filed to initialize device: {}", err);
            return -1;
        }
    };
    // Release: makes the fully-constructed Device visible to all cores that subsequently
    // perform an Acquire load.
    DEVICE.store(Box::into_raw(device), Ordering::Release);

    STATUS_SUCCESS
}

// driver_unload function is called when service delete is called from user-space.
unsafe extern "system" fn driver_unload(_object: *const DRIVER_OBJECT) {
    info!("Unloading complete");
    // Atomically null the pointer before freeing. Any core that performs an Acquire load
    // *after* this swap will see null and bail out safely. Any core that already loaded a
    // non-null pointer before this swap is protected by the OS-level serialisation:
    //   - WFP callouts: FilterEngine::drop() (field declared first in Device) calls the WFP
    //     unregister APIs which block until every in-flight classify callback has returned,
    //     so no callout thread holds a live reference by the time the memory is freed.
    //   - IRP dispatch (read/write/ioctl): the I/O Manager guarantees no dispatch routine
    //     is executing when driver_unload is called.
    // The swap is executed exactly once, on the unload path.
    let ptr = DEVICE.swap(core::ptr::null_mut(), Ordering::AcqRel);
    if !ptr.is_null() {
        unsafe { drop(Box::from_raw(ptr)); }
    }
}

/// driver_create is triggered when user-space opens a handle to the device (CreateFile).
unsafe extern "system" fn driver_create(
    _device_object: *const DEVICE_OBJECT,
    irp: *mut IRP,
) -> NTSTATUS {
    let mut create_request = CreateRequest::new(irp.as_mut().unwrap());
    if let Some(device) = get_device() {
        let pid = create_request.get_requestor_pid();
        device.owner_pid.store(pid, core::sync::atomic::Ordering::Release);
        info!("Device opened by PID {}", pid);
    }
    create_request.complete();
    create_request.get_status()
}

/// driver_cleanup is triggered when user-space closes the last handle to the device.
unsafe extern "system" fn driver_cleanup(
    _device_object: *const DEVICE_OBJECT,
    irp: *mut IRP,
) -> NTSTATUS {
    let mut cleanup_request = CleanupRequest::new(irp.as_mut().unwrap());
    if let Some(device) = get_device() {
        let old_pid = device.owner_pid.swap(0, core::sync::atomic::Ordering::Release);
        info!("Device closed by PID {}", old_pid);
    }
    cleanup_request.complete();
    cleanup_request.get_status()
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
