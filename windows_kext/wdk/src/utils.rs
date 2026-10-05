use alloc::string::{String, ToString};
use ntstatus::ntstatus::NtStatus;
use windows_sys::Win32::Foundation::STATUS_SUCCESS;

use crate::ffi;

pub fn check_ntstatus(status: i32) -> Result<(), String> {
    if status == STATUS_SUCCESS {
        return Ok(());
    }

    let Some(status) = NtStatus::from_u32(status as u32) else {
        return Err("UNKNOWN_ERROR_CODE".to_string());
    };

    return Err(status.to_string());
}

pub fn get_system_timestamp_ms() -> u64 {
    // 100 nano seconds units -> device by 10 -> micro seconds -> divide by 1000 -> milliseconds
    unsafe { ffi::pm_QuerySystemTime() / 10_000 }
}

#[allow(non_snake_case)]
extern "system" {
    fn KeBugCheckEx(
        BugCheckCode: u32,
        BugCheckParameter1: usize,
        BugCheckParameter2: usize,
        BugCheckParameter3: usize,
        BugCheckParameter4: usize,
    ) -> !;
}

/// Bug check code used for a fatal driver error (Rust panic): the driver's
/// pool tag characters "PMrs" in big-endian order, so the hex code shown by
/// the debugger (0x504D7273) reads as "PMrs".
pub const PANIC_BUG_CHECK_CODE: u32 = u32::from_be_bytes(*b"PMrs");

/// Stops the system with the driver's panic bug check code. Does not
/// allocate, so it is safe to call when the panic itself is a failed
/// allocation. The bug check parameters shown by the debugger are:
/// 1 = pointer to the source file path (print with `da <P1> L<P2>`),
/// 2 = its length, 3 = the line number, 4 = pointer to the NUL-terminated
/// panic message (print with `da <P4>`), 0 when there is none.
pub fn bug_check(file: &str, line: u32, message: &str) -> ! {
    let message_ptr = if message.is_empty() {
        0
    } else {
        message.as_ptr() as usize
    };
    unsafe {
        KeBugCheckEx(
            PANIC_BUG_CHECK_CODE,
            file.as_ptr() as usize,
            file.len(),
            line as usize,
            message_ptr,
        )
    }
}
