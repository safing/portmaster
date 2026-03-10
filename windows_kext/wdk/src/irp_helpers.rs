use windows_sys::{
    Wdk::{
        Foundation::{IO_STACK_LOCATION_0_4, IRP},
        Storage::FileSystem::IO_NO_INCREMENT,
        System::SystemServices::IofCompleteRequest,
    },
    Win32::Foundation::{
        NTSTATUS, STATUS_END_OF_FILE, STATUS_NOT_IMPLEMENTED, STATUS_SUCCESS, STATUS_TIMEOUT,
    },
};

/// Wraps an IRP_MJ_CREATE request (triggered when user-space opens the device handle).
pub struct CreateRequest<'a> {
    irp: &'a mut IRP,
}

impl CreateRequest<'_> {
    pub fn new(irp: &mut IRP) -> CreateRequest<'_> {
        CreateRequest { irp }
    }

    /// Returns the PID of the process that opened the device handle.
    /// Safe to call here because IRP_MJ_CREATE always runs in the context
    /// of the initiating process, never in an arbitrary system thread.
    pub fn get_requestor_pid(&self) -> u32 {
        unsafe { crate::ffi::PsGetCurrentProcessId() as u32 }
    }

    pub fn complete(&mut self) {
        // FILE_OPENED (1): indicates the device was opened (not created/superseded).
        const FILE_OPENED: usize = 1;
        self.irp.IoStatus.Information = FILE_OPENED;
        self.irp.IoStatus.Anonymous.Status = STATUS_SUCCESS;
        unsafe { IofCompleteRequest(self.irp, IO_NO_INCREMENT as i8) };
    }

    pub fn get_status(&self) -> NTSTATUS {
        unsafe { self.irp.IoStatus.Anonymous.Status }
    }
}

/// Wraps an IRP_MJ_CLEANUP request (triggered when user-space closes the last handle).
pub struct CleanupRequest<'a> {
    irp: &'a mut IRP,
}

impl CleanupRequest<'_> {
    pub fn new(irp: &mut IRP) -> CleanupRequest<'_> {
        CleanupRequest { irp }
    }

    pub fn complete(&mut self) {
        self.irp.IoStatus.Information = 0;
        self.irp.IoStatus.Anonymous.Status = STATUS_SUCCESS;
        unsafe { IofCompleteRequest(self.irp, IO_NO_INCREMENT as i8) };
    }

    pub fn get_status(&self) -> NTSTATUS {
        unsafe { self.irp.IoStatus.Anonymous.Status }
    }
}

pub struct ReadRequest<'a> {
    irp: &'a mut IRP,
    buffer: &'a mut [u8],
    fill_index: usize,
}

impl ReadRequest<'_> {
    pub fn new(irp: &mut IRP) -> ReadRequest<'_> {
        unsafe {
            let irp_sp = irp.Tail.Overlay.Anonymous2.Anonymous.CurrentStackLocation;
            let device_io = (*irp_sp).Parameters.Read;

            let system_buffer = irp.AssociatedIrp.SystemBuffer;
            let buffer = core::slice::from_raw_parts_mut(
                system_buffer as *mut u8,
                device_io.Length as usize,
            );
            ReadRequest {
                irp,
                buffer,
                fill_index: 0,
            }
        }
    }

    pub fn free_space(&self) -> usize {
        self.buffer.len() - self.fill_index
    }

    pub fn complete(&mut self) {
        self.irp.IoStatus.Information = self.fill_index;
        self.irp.IoStatus.Anonymous.Status = STATUS_SUCCESS;
    }

    pub fn end_of_file(&mut self) {
        self.irp.IoStatus.Information = self.fill_index;
        self.irp.IoStatus.Anonymous.Status = STATUS_END_OF_FILE;
    }

    pub fn timeout(&mut self) {
        self.irp.IoStatus.Anonymous.Status = STATUS_TIMEOUT;
    }

    pub fn get_status(&self) -> NTSTATUS {
        unsafe { self.irp.IoStatus.Anonymous.Status }
    }

    pub fn write(&mut self, bytes: &[u8]) -> usize {
        let mut bytes_to_write: usize = bytes.len();

        // Check if we have enough space
        if bytes_to_write > self.free_space() {
            bytes_to_write = self.free_space();
        }

        for i in 0..bytes_to_write {
            self.buffer[self.fill_index + i] = bytes[i];
        }
        self.fill_index += bytes_to_write;

        bytes_to_write
    }
}

pub struct WriteRequest<'a> {
    irp: &'a mut IRP,
    buffer: &'a mut [u8],
}

impl WriteRequest<'_> {
    pub fn new(irp: &mut IRP) -> WriteRequest<'_> {
        unsafe {
            let irp_sp = irp.Tail.Overlay.Anonymous2.Anonymous.CurrentStackLocation;
            let device_io = (*irp_sp).Parameters.Write;

            let system_buffer = irp.AssociatedIrp.SystemBuffer;
            let buffer = core::slice::from_raw_parts_mut(
                system_buffer as *mut u8,
                device_io.Length as usize,
            );
            WriteRequest { irp, buffer }
        }
    }

    pub fn get_buffer(&self) -> &[u8] {
        self.buffer
    }

    pub fn mark_all_as_read(&mut self) {
        self.irp.IoStatus.Information = self.buffer.len();
    }

    pub fn complete(&mut self) {
        self.irp.IoStatus.Anonymous.Status = STATUS_SUCCESS;
    }

    pub fn get_status(&self) -> NTSTATUS {
        unsafe { self.irp.IoStatus.Anonymous.Status }
    }
}

pub struct DeviceControlRequest<'a> {
    irp: &'a mut IRP,
    buffer: &'a mut [u8],
    fill_index: usize,
    control_code: u32,
}

// Windows-rs version of the struct is incorrect (18.01.2024).
// TODO: Use the official version when fixed.
// For future reference: https://github.com/microsoft/windows-rs/issues/2805
#[repr(C)]
struct DeviceIOControlParams {
    output_buffer_length: u32,
    _padding1: u32,
    input_buffer_length: u32,
    _padding2: u32,
    io_control_code: u32,
    _padding3: u32,
}

impl DeviceControlRequest<'_> {
    pub fn new(irp: &mut IRP) -> DeviceControlRequest<'_> {
        unsafe {
            let irp_sp = irp.Tail.Overlay.Anonymous2.Anonymous.CurrentStackLocation;
            // Use the struct directly when replaced with proper version.
            let device_io: DeviceIOControlParams =
                core::mem::transmute::<IO_STACK_LOCATION_0_4, DeviceIOControlParams>(
                    (*irp_sp).Parameters.DeviceIoControl,
                );

            let system_buffer = irp.AssociatedIrp.SystemBuffer;
            let buffer = core::slice::from_raw_parts_mut(
                system_buffer as *mut u8,
                device_io.output_buffer_length as usize,
            );
            DeviceControlRequest {
                irp,
                buffer,
                fill_index: 0,
                control_code: device_io.io_control_code,
            }
        }
    }

    pub fn get_buffer(&self) -> &[u8] {
        self.buffer
    }
    pub fn write(&mut self, bytes: &[u8]) -> usize {
        let mut bytes_to_write: usize = bytes.len();

        // Check if we have enough space
        if bytes_to_write > self.free_space() {
            bytes_to_write = self.free_space();
        }

        for i in 0..bytes_to_write {
            self.buffer[self.fill_index + i] = bytes[i];
        }
        self.fill_index += bytes_to_write;

        bytes_to_write
    }

    pub fn complete(&mut self) {
        self.irp.IoStatus.Information = self.buffer.len();
        self.irp.IoStatus.Anonymous.Status = STATUS_SUCCESS;
        unsafe { IofCompleteRequest(self.irp, IO_NO_INCREMENT as i8) };
    }

    pub fn not_implemented(&mut self) {
        self.irp.IoStatus.Anonymous.Status = STATUS_NOT_IMPLEMENTED;
        unsafe { IofCompleteRequest(self.irp, IO_NO_INCREMENT as i8) };
    }

    pub fn get_status(&self) -> NTSTATUS {
        unsafe { self.irp.IoStatus.Anonymous.Status }
    }

    pub fn get_control_code(&self) -> u32 {
        self.control_code
    }

    pub fn free_space(&self) -> usize {
        self.buffer.len() - self.fill_index
    }
}
