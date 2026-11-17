//! Raw Win32 bindings used by the kext backend.
//!
//! This file is the **only** place in `pm-winkext` that declares foreign
//! functions. It is compiled exclusively on Windows and carries a local
//! `#[allow(unsafe_code)]`; the crate lint is `deny` (not the workspace's
//! absolute `forbid`), so the allowance is scoped to exactly this module.
//! Everything else in the crate is safe Rust.

#[cfg(windows)]
mod ffi {
    #![allow(unsafe_code)]

    use core::ffi::c_void;
    use std::ffi::OsStr;
    use std::os::windows::ffi::OsStrExt;

    use crate::{Error, Result};

    /// Win32 `HANDLE`, represented as a pointer-sized integer just like Go's
    /// `windows.Handle` (`uintptr`). This keeps [`crate::Kext`] `Send + Sync`
    /// while remaining ABI-compatible with the `extern "system"` calls below.
    pub type Handle = isize;

    /// `INVALID_HANDLE_VALUE` (`(HANDLE)-1`).
    pub const INVALID_HANDLE_VALUE: Handle = -1;

    // File access / creation flags (golang.org/x/sys/windows constants).
    pub const GENERIC_READ: u32 = 0x8000_0000;
    pub const GENERIC_WRITE: u32 = 0x4000_0000;
    pub const OPEN_EXISTING: u32 = 3;
    pub const FILE_ATTRIBUTE_NORMAL: u32 = 0x80;
    pub const FILE_FLAG_OVERLAPPED: u32 = 0x4000_0000;

    // Service control manager constants.
    pub const SC_MANAGER_ALL_ACCESS: u32 = 0x000f_003f;
    pub const SERVICE_ALL_ACCESS: u32 = 0x000f_01ff;
    pub const SERVICE_KERNEL_DRIVER: u32 = 0x0000_0001;
    pub const SERVICE_DEMAND_START: u32 = 0x0000_0003;
    pub const SERVICE_ERROR_NORMAL: u32 = 0x0000_0001;
    pub const SERVICE_CONTROL_STOP: u32 = 0x0000_0001;
    pub const SERVICE_STOPPED: u32 = 0x0000_0001;
    pub const SERVICE_RUNNING: u32 = 0x0000_0004;
    pub const SERVICE_NO_CHANGE: u32 = 0xffff_ffff;
    pub const ERROR_SERVICE_ALREADY_RUNNING: u32 = 1056;

    /// Win32 `OVERLAPPED` structure (32 bytes on x64).
    #[repr(C)]
    #[derive(Clone, Copy)]
    pub struct Overlapped {
        pub internal: usize,
        pub internal_high: usize,
        pub offset: u32,
        pub offset_high: u32,
        pub h_event: Handle,
    }

    impl Default for Overlapped {
        fn default() -> Self {
            Self {
                internal: 0,
                internal_high: 0,
                offset: 0,
                offset_high: 0,
                h_event: 0,
            }
        }
    }

    /// Win32 `SERVICE_STATUS` structure.
    #[repr(C)]
    #[derive(Clone, Copy, Default)]
    pub struct ServiceStatus {
        pub service_type: u32,
        pub current_state: u32,
        pub controls_accepted: u32,
        pub win32_exit_code: u32,
        pub service_specific_exit_code: u32,
        pub check_point: u32,
        pub wait_hint: u32,
    }

    #[link(name = "kernel32")]
    extern "system" {
        pub fn CreateFileW(
            lp_file_name: *const u16,
            dw_desired_access: u32,
            dw_share_mode: u32,
            lp_security_attributes: *mut c_void,
            dw_creation_disposition: u32,
            dw_flags_and_attributes: u32,
            h_template_file: Handle,
        ) -> Handle;
        pub fn DeviceIoControl(
            h_device: Handle,
            dw_io_control_code: u32,
            lp_in_buffer: *mut c_void,
            n_in_buffer_size: u32,
            lp_out_buffer: *mut c_void,
            n_out_buffer_size: u32,
            lp_bytes_returned: *mut u32,
            lp_overlapped: *mut Overlapped,
        ) -> i32;
        pub fn GetOverlappedResult(
            h_file: Handle,
            lp_overlapped: *mut Overlapped,
            lp_number_of_bytes_transferred: *mut u32,
            b_wait: i32,
        ) -> i32;
        pub fn ReadFile(
            h_file: Handle,
            lp_buffer: *mut c_void,
            n_number_of_bytes_to_read: u32,
            lp_number_of_bytes_read: *mut u32,
            lp_overlapped: *mut Overlapped,
        ) -> i32;
        pub fn WriteFile(
            h_file: Handle,
            lp_buffer: *const c_void,
            n_number_of_bytes_to_write: u32,
            lp_number_of_bytes_written: *mut u32,
            lp_overlapped: *mut Overlapped,
        ) -> i32;
        pub fn CloseHandle(h_object: Handle) -> i32;
        pub fn GetLastError() -> u32;
    }

    #[link(name = "advapi32")]
    extern "system" {
        pub fn OpenSCManagerW(
            lp_machine_name: *const u16,
            lp_database_name: *const u16,
            dw_desired_access: u32,
        ) -> Handle;
        pub fn OpenServiceW(
            h_sc_manager: Handle,
            lp_service_name: *const u16,
            dw_desired_access: u32,
        ) -> Handle;
        pub fn CreateServiceW(
            h_sc_manager: Handle,
            lp_service_name: *const u16,
            lp_display_name: *const u16,
            dw_desired_access: u32,
            dw_service_type: u32,
            dw_start_type: u32,
            dw_error_control: u32,
            lp_binary_path_name: *const u16,
            lp_load_order_group: *const u16,
            lpdw_tag_id: *mut u32,
            lp_dependencies: *const u16,
            lp_service_start_name: *const u16,
            lp_password: *const u16,
        ) -> Handle;
        pub fn StartServiceW(
            h_service: Handle,
            dw_num_service_args: u32,
            lp_service_arg_vectors: *const *const u16,
        ) -> i32;
        pub fn ControlService(
            h_service: Handle,
            dw_control: u32,
            lp_service_status: *mut ServiceStatus,
        ) -> i32;
        pub fn QueryServiceStatus(
            h_service: Handle,
            lp_service_status: *mut ServiceStatus,
        ) -> i32;
        pub fn DeleteService(h_service: Handle) -> i32;
        pub fn CloseServiceHandle(h_sc_object: Handle) -> i32;
    }

    /// Encodes a Rust string as a NUL-terminated UTF-16 buffer, equivalent to
    /// Go's `syscall.UTF16FromString`.
    pub fn wide(value: &str) -> Vec<u16> {
        OsStr::new(value)
            .encode_wide()
            .chain(core::iter::once(0))
            .collect()
    }

    /// Builds an [`Error`] from the last Win32 error.
    pub fn last_error() -> Error {
        Error::Io(std::io::Error::from_raw_os_error(unsafe { GetLastError() } as i32))
    }

    /// Whether a handle is NULL or `INVALID_HANDLE_VALUE`.
    #[must_use]
    pub fn is_invalid(handle: Handle) -> bool {
        handle == 0 || handle == INVALID_HANDLE_VALUE
    }

    /// Converts a BOOL-returning Win32 call into a [`Result`].
    pub fn check(result: i32) -> Result<()> {
        if result == 0 {
            Err(last_error())
        } else {
            Ok(())
        }
    }

    /// Converts a handle-returning Win32 call into a [`Result`].
    pub fn check_handle(handle: Handle) -> Result<Handle> {
        if is_invalid(handle) {
            Err(last_error())
        } else {
            Ok(handle)
        }
    }
}

#[cfg(windows)]
pub(crate) use ffi::*;
