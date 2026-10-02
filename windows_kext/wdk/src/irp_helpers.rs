use core::ffi::c_void;

use windows_sys::{
    Wdk::{
        Foundation::{IO_STACK_LOCATION, IRP},
        Storage::FileSystem::IO_NO_INCREMENT,
        System::SystemServices::IofCompleteRequest,
    },
    Win32::Foundation::{
        NTSTATUS, STATUS_CANCELLED, STATUS_END_OF_FILE, STATUS_NOT_IMPLEMENTED, STATUS_SUCCESS,
        STATUS_TIMEOUT,
    },
};

/// Opaque access to an I/O-manager-owned IRP.
///
/// The pinned windows-rs `IRP` type has weaker alignment than the native WDK
/// object. Keeping the pointer raw prevents safe Rust code from allocating,
/// copying, or retaining a reference to that generated representation. The I/O
/// manager owns the object before completion and may reclaim it as soon as
/// `IofCompleteRequest` returns.
struct IrpPtr {
    ptr: *mut IRP,
}

impl IrpPtr {
    /// Creates an opaque wrapper for an IRP supplied to a WDM dispatch routine.
    ///
    /// # Safety
    ///
    /// `ptr` must either be null or point to a live, native-aligned IRP owned by
    /// the I/O manager for the entire request operation.
    unsafe fn new(ptr: *mut IRP) -> Option<Self> {
        (!ptr.is_null()).then_some(Self { ptr })
    }

    /// Returns the request's current I/O stack location.
    ///
    /// # Safety
    ///
    /// While `self.ptr` is non-null, the caller must uphold the live native IRP
    /// contract established by [`IrpPtr::new`].
    unsafe fn current_stack_location(&self) -> *mut IO_STACK_LOCATION {
        if self.ptr.is_null() {
            return core::ptr::null_mut();
        }

        // SAFETY: the caller guarantees a live native IRP. The null state created
        // immediately before completion was rejected above.
        unsafe {
            (*self.ptr)
                .Tail
                .Overlay
                .Anonymous2
                .Anonymous
                .CurrentStackLocation
        }
    }

    /// Returns the IRP's buffered-I/O system buffer without dereferencing it.
    ///
    /// # Safety
    ///
    /// While `self.ptr` is non-null, the caller must uphold the live native IRP
    /// contract established by [`IrpPtr::new`].
    unsafe fn system_buffer(&self) -> *mut u8 {
        if self.ptr.is_null() {
            return core::ptr::null_mut();
        }

        // SAFETY: the caller guarantees a live native IRP. Reading this union field
        // is valid for the buffered request types accepted by the public wrappers.
        unsafe { (*self.ptr).AssociatedIrp.SystemBuffer.cast() }
    }

    /// Publishes status and transfers the IRP back to the I/O manager exactly
    /// once. Clear the local raw pointer before the call because the IRP can be
    /// reclaimed during completion.
    fn complete(&mut self, status: NTSTATUS, information: usize) -> NTSTATUS {
        let irp = core::mem::replace(&mut self.ptr, core::ptr::null_mut());
        if irp.is_null() {
            return status;
        }

        unsafe {
            (*irp).IoStatus.Information = information;
            (*irp).IoStatus.Anonymous.Status = status;
            IofCompleteRequest(irp, IO_NO_INCREMENT as i8);
        }
        status
    }
}

/// Converts a buffered-I/O pointer and length into a usable range.
///
/// A zero-byte request is allowed to carry a null `SystemBuffer`. Rust slices,
/// however, require a non-null pointer even when empty, so request wrappers keep
/// a raw pointer and normalize every null pointer to an empty range.
///
/// # Safety
///
/// If `buffer` is non-null and `length` is non-zero, it must identify a live,
/// writable allocation of at least `length` bytes that remains valid until the
/// request wrapper using the returned range is completed.
unsafe fn normalize_buffer(buffer: *mut u8, length: u32) -> (*mut u8, usize) {
    if buffer.is_null() || length == 0 {
        (core::ptr::null_mut(), 0)
    } else {
        (buffer, length as usize)
    }
}

/// Wraps an IRP_MJ_CREATE request (triggered when user-space opens the device handle).
pub struct CreateRequest {
    irp: IrpPtr,
}

impl CreateRequest {
    /// # Safety
    ///
    /// `irp` must satisfy the WDM dispatch contract documented by [`IrpPtr::new`].
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        Some(Self {
            irp: unsafe {
                // SAFETY: this constructor forwards its documented WDM dispatch
                // contract directly to the opaque IRP wrapper.
                IrpPtr::new(irp)?
            },
        })
    }

    /// Returns the PID of the process that opened the device handle.
    /// Safe to call here because IRP_MJ_CREATE always runs in the context
    /// of the initiating process, never in an arbitrary system thread.
    pub fn get_requestor_pid(&self) -> u32 {
        unsafe { crate::ffi::PsGetCurrentProcessId() as u32 }
    }

    /// Returns the kernel file object that identifies this particular open.
    /// The pointer is used only as an opaque identity token by the driver.
    pub fn get_file_object(&self) -> *mut c_void {
        unsafe {
            let irp_sp = self.irp.current_stack_location();
            if irp_sp.is_null() {
                core::ptr::null_mut()
            } else {
                (*irp_sp).FileObject.cast()
            }
        }
    }

    pub fn complete(&mut self) -> NTSTATUS {
        const FILE_OPENED: usize = 1;
        self.irp.complete(STATUS_SUCCESS, FILE_OPENED)
    }

    pub fn fail(&mut self, status: NTSTATUS) -> NTSTATUS {
        self.irp.complete(status, 0)
    }
}

/// Wraps an IRP_MJ_CLEANUP request (triggered when user-space closes the last handle).
pub struct CleanupRequest {
    irp: IrpPtr,
}

impl CleanupRequest {
    /// # Safety
    ///
    /// `irp` must satisfy the WDM dispatch contract documented by [`IrpPtr::new`].
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        Some(Self {
            irp: unsafe {
                // SAFETY: this constructor forwards its documented WDM dispatch
                // contract directly to the opaque IRP wrapper.
                IrpPtr::new(irp)?
            },
        })
    }

    pub fn get_file_object(&self) -> *mut c_void {
        unsafe {
            let irp_sp = self.irp.current_stack_location();
            if irp_sp.is_null() {
                core::ptr::null_mut()
            } else {
                (*irp_sp).FileObject.cast()
            }
        }
    }

    pub fn complete(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_SUCCESS, 0)
    }

    pub fn fail(&mut self, status: NTSTATUS) -> NTSTATUS {
        self.irp.complete(status, 0)
    }
}

/// Wraps an IRP_MJ_CLOSE request.
///
/// The driver supplies a raw WDM dispatch table for its WDF control device, so
/// CLOSE must be completed by the same table rather than falling back to KMDF's
/// device dispatch after driver-owned teardown has begun.
pub struct CloseRequest {
    irp: IrpPtr,
}

impl CloseRequest {
    /// # Safety
    ///
    /// `irp` must satisfy the WDM dispatch contract documented by [`IrpPtr::new`].
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        Some(Self {
            irp: unsafe {
                // SAFETY: this constructor forwards its documented WDM dispatch
                // contract directly to the opaque IRP wrapper.
                IrpPtr::new(irp)?
            },
        })
    }

    pub fn complete(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_SUCCESS, 0)
    }
}

pub struct ReadRequest {
    irp: IrpPtr,
    buffer: *mut u8,
    buffer_len: usize,
    fill_index: usize,
}

impl ReadRequest {
    /// # Safety
    ///
    /// `irp` must be a live buffered IRP_MJ_READ request supplied by the I/O
    /// manager. Its stack location and system buffer must remain valid until the
    /// request is completed.
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        // SAFETY: this constructor forwards its documented buffered-read IRP
        // contract directly to the opaque wrapper.
        let irp = unsafe { IrpPtr::new(irp)? };
        // SAFETY: the IRP is still owned by this uncompleted wrapper.
        let irp_sp = unsafe { irp.current_stack_location() };
        let length = if irp_sp.is_null() {
            0
        } else {
            // SAFETY: the constructor contract identifies an IRP_MJ_READ stack
            // entry, so the Read member of the Parameters union is initialized.
            unsafe { (*irp_sp).Parameters.Read.Length }
        };
        let (buffer, buffer_len) = unsafe {
            // SAFETY: The constructor's IRP contract guarantees that its buffered
            // system allocation covers the length reported by this stack entry.
            normalize_buffer(irp.system_buffer(), length)
        };

        Some(Self {
            irp,
            buffer,
            buffer_len,
            fill_index: 0,
        })
    }

    pub fn free_space(&self) -> usize {
        self.buffer_len.saturating_sub(self.fill_index)
    }

    pub fn complete(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_SUCCESS, self.fill_index)
    }

    pub fn fail(&mut self, status: NTSTATUS) -> NTSTATUS {
        self.irp.complete(status, 0)
    }

    pub fn end_of_file(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_END_OF_FILE, self.fill_index)
    }

    pub fn timeout(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_TIMEOUT, 0)
    }

    pub fn cancelled(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_CANCELLED, 0)
    }

    pub fn write(&mut self, bytes: &[u8]) -> usize {
        let bytes_to_write = core::cmp::min(bytes.len(), self.free_space());
        if bytes_to_write == 0 {
            return 0;
        }

        unsafe {
            core::ptr::copy_nonoverlapping(
                bytes.as_ptr(),
                self.buffer.add(self.fill_index),
                bytes_to_write,
            );
        }
        self.fill_index += bytes_to_write;
        bytes_to_write
    }
}

pub struct WriteRequest {
    irp: IrpPtr,
    buffer: *const u8,
    buffer_len: usize,
    information: usize,
}

impl WriteRequest {
    /// # Safety
    ///
    /// `irp` must be a live buffered IRP_MJ_WRITE request supplied by the I/O
    /// manager. Its stack location and system buffer must remain valid until the
    /// request is completed.
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        // SAFETY: this constructor forwards its documented buffered-write IRP
        // contract directly to the opaque wrapper.
        let irp = unsafe { IrpPtr::new(irp)? };
        // SAFETY: the IRP is still owned by this uncompleted wrapper.
        let irp_sp = unsafe { irp.current_stack_location() };
        let length = if irp_sp.is_null() {
            0
        } else {
            // SAFETY: the constructor contract identifies an IRP_MJ_WRITE stack
            // entry, so the Write member of the Parameters union is initialized.
            unsafe { (*irp_sp).Parameters.Write.Length }
        };
        let (buffer, buffer_len) = unsafe {
            // SAFETY: The constructor's IRP contract guarantees that its buffered
            // system allocation covers the length reported by this stack entry.
            normalize_buffer(irp.system_buffer(), length)
        };

        Some(Self {
            irp,
            buffer,
            buffer_len,
            information: 0,
        })
    }

    pub fn get_buffer(&self) -> &[u8] {
        if self.buffer_len == 0 {
            &[]
        } else {
            unsafe { core::slice::from_raw_parts(self.buffer, self.buffer_len) }
        }
    }

    pub fn mark_all_as_read(&mut self) {
        self.information = self.buffer_len;
    }

    pub fn complete(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_SUCCESS, self.information)
    }

    pub fn fail(&mut self, status: NTSTATUS) -> NTSTATUS {
        self.irp.complete(status, 0)
    }

    pub fn fail_with_bytes_consumed(
        &mut self,
        status: NTSTATUS,
        bytes_consumed: usize,
    ) -> NTSTATUS {
        self.irp.complete(status, bytes_consumed.min(self.buffer_len))
    }
}

pub struct DeviceControlRequest {
    irp: IrpPtr,
    buffer: *mut u8,
    buffer_len: usize,
    fill_index: usize,
    control_code: u32,
}

// The pinned windows-rs revision drops WDK's POINTER_ALIGNMENT annotations
// from the three ULONG fields. Keep the complete native x64 member here and
// view it through the correctly sized enclosing Parameters union.
// See https://github.com/microsoft/windows-rs/issues/2805.
#[repr(C)]
struct DeviceIOControlParams {
    output_buffer_length: u32,
    _padding1: u32,
    input_buffer_length: u32,
    _padding2: u32,
    io_control_code: u32,
    _padding3: u32,
    _type3_input_buffer: *mut c_void,
}

#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, offset_of, size_of};

    assert!(size_of::<DeviceIOControlParams>() == 32);
    assert!(align_of::<DeviceIOControlParams>() == 8);
    assert!(offset_of!(DeviceIOControlParams, output_buffer_length) == 0);
    assert!(offset_of!(DeviceIOControlParams, input_buffer_length) == 8);
    assert!(offset_of!(DeviceIOControlParams, io_control_code) == 16);
    assert!(offset_of!(DeviceIOControlParams, _type3_input_buffer) == 24);
};

// IRPs and stack locations are allocated by the I/O manager, never by Rust.
// The pinned bindings give IRP a weaker alignment (8 instead of the WDK's 16),
// so this module stores only raw pointers to IRPs. Pin every offset it
// dereferences, including the full 32-byte Parameters union.
#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, offset_of, size_of};
    use windows_sys::Wdk::Foundation::IO_STACK_LOCATION_0;

    assert!(size_of::<IRP>() == 208);
    assert!(align_of::<IRP>() == 8);
    assert!(offset_of!(IRP, AssociatedIrp) == 24);
    assert!(offset_of!(IRP, AssociatedIrp.SystemBuffer) == 24);
    assert!(offset_of!(IRP, IoStatus) == 48);
    assert!(offset_of!(IRP, IoStatus.Anonymous.Status) == 48);
    assert!(offset_of!(IRP, IoStatus.Information) == 56);
    assert!(offset_of!(IRP, Tail) == 120);
    assert!(offset_of!(IRP, Tail.Overlay.Anonymous2.Anonymous.CurrentStackLocation) == 184);

    assert!(size_of::<IO_STACK_LOCATION>() == 72);
    assert!(align_of::<IO_STACK_LOCATION>() == 8);
    assert!(size_of::<IO_STACK_LOCATION_0>() == 32);
    assert!(offset_of!(IO_STACK_LOCATION, Parameters) == 8);
    assert!(offset_of!(IO_STACK_LOCATION, Parameters.Read.Length) == 8);
    assert!(offset_of!(IO_STACK_LOCATION, Parameters.Write.Length) == 8);
    assert!(offset_of!(IO_STACK_LOCATION, FileObject) == 48);
};

impl DeviceControlRequest {
    /// # Safety
    ///
    /// `irp` must be a live METHOD_BUFFERED IRP_MJ_DEVICE_CONTROL request
    /// supplied by the I/O manager. Its stack location and system buffer must
    /// remain valid until the request is completed.
    pub unsafe fn new(irp: *mut IRP) -> Option<Self> {
        // SAFETY: this constructor forwards its documented METHOD_BUFFERED IOCTL
        // IRP contract directly to the opaque wrapper.
        let irp = unsafe { IrpPtr::new(irp)? };
        // SAFETY: the IRP is still owned by this uncompleted wrapper.
        let irp_sp = unsafe { irp.current_stack_location() };
        let (output_buffer_length, control_code) = if irp_sp.is_null() {
            (0, 0)
        } else {
            // SAFETY: the constructor contract identifies an IRP_MJ_DEVICE_CONTROL
            // stack entry. The native Parameters storage is live, aligned, and its
            // DeviceIoControl member has the pinned layout asserted above.
            let device_io = unsafe {
                &*core::ptr::addr_of!((*irp_sp).Parameters).cast::<DeviceIOControlParams>()
            };
            (device_io.output_buffer_length, device_io.io_control_code)
        };
        let (buffer, buffer_len) = unsafe {
            // SAFETY: The constructor's METHOD_BUFFERED IRP contract guarantees
            // that SystemBuffer covers the advertised output length.
            normalize_buffer(irp.system_buffer(), output_buffer_length)
        };

        Some(Self {
            irp,
            buffer,
            buffer_len,
            fill_index: 0,
            control_code,
        })
    }

    pub fn write(&mut self, bytes: &[u8]) -> usize {
        let bytes_to_write = core::cmp::min(bytes.len(), self.free_space());
        if bytes_to_write == 0 {
            return 0;
        }

        unsafe {
            core::ptr::copy_nonoverlapping(
                bytes.as_ptr(),
                self.buffer.add(self.fill_index),
                bytes_to_write,
            );
        }
        self.fill_index += bytes_to_write;
        bytes_to_write
    }

    pub fn write_exact(&mut self, bytes: &[u8]) -> bool {
        // Fixed-size IOCTL responses must be all-or-nothing. In particular, do
        // not advance Information after copying only the prefix of a response.
        if self.free_space() < bytes.len() {
            return false;
        }

        self.write(bytes) == bytes.len()
    }

    pub fn complete(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_SUCCESS, self.fill_index)
    }

    pub fn fail(&mut self, status: NTSTATUS) -> NTSTATUS {
        self.irp.complete(status, 0)
    }

    pub fn not_implemented(&mut self) -> NTSTATUS {
        self.irp.complete(STATUS_NOT_IMPLEMENTED, 0)
    }

    pub fn get_control_code(&self) -> u32 {
        self.control_code
    }

    pub fn free_space(&self) -> usize {
        self.buffer_len.saturating_sub(self.fill_index)
    }
}

#[cfg(test)]
mod tests {
    use super::{DeviceControlRequest, IrpPtr};

    fn device_control_request(buffer: &mut [u8]) -> DeviceControlRequest {
        DeviceControlRequest {
            // These tests exercise only buffer writes. No completion method may
            // be called with this inert IRP pointer.
            irp: IrpPtr {
                ptr: core::ptr::null_mut(),
            },
            buffer: if buffer.is_empty() {
                core::ptr::null_mut()
            } else {
                buffer.as_mut_ptr()
            },
            buffer_len: buffer.len(),
            fill_index: 0,
            control_code: 0,
        }
    }

    #[test]
    fn exact_ioctl_write_rejects_short_buffer_without_partial_output() {
        for output_len in 0..4 {
            let mut output = [0xAA; 3];
            let mut request = device_control_request(&mut output[..output_len]);

            assert!(!request.write_exact(&[1, 2, 3, 4]));
            assert_eq!(request.fill_index, 0);
            assert_eq!(output, [0xAA; 3]);
        }
    }

    #[test]
    fn exact_ioctl_write_reports_complete_output_only() {
        let mut output = [0xAA; 6];
        let mut request = device_control_request(&mut output);

        assert!(request.write_exact(&[1, 2, 3, 4]));
        assert_eq!(request.fill_index, 4);
        assert_eq!(request.free_space(), 2);
        assert_eq!(output, [1, 2, 3, 4, 0xAA, 0xAA]);
    }
}
