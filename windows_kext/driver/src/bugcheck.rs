use core::panic::PanicInfo;

/// Customer-defined bug check emitted when Rust reaches its panic handler.
///
/// The high bits encode an error with the customer flag set; facility 0x04d is
/// reserved here for Portmaster and reason 1 identifies a Rust panic.
const PORTMASTER_DRIVER_PANIC: u32 = 0xE04D_0001;

extern "system" {
    // The WDK declares KeBugCheckEx as DECLSPEC_NORETURN. The pinned windows-sys
    // binding loses that annotation and returns (), so declare the native import
    // as `!` here instead of adding an unreachable fallback spin loop.
    #[link_name = "KeBugCheckEx"]
    fn ke_bug_check_ex(
        bug_check_code: u32,
        bug_check_parameter1: usize,
        bug_check_parameter2: usize,
        bug_check_parameter3: usize,
        bug_check_parameter4: usize,
    ) -> !;
}

/// Terminates the system with enough allocation-free panic-site data for a dump.
///
/// Parameters in the resulting bug check are:
///
/// 1. address of the static UTF-8 source-file name;
/// 2. source-file name length in bytes;
/// 3. source line;
/// 4. source column.
///
/// Do not format or enqueue the panic message here. A panic can occur in the
/// allocator or while a driver lock is held, so logging could recurse or deadlock
/// before the dump is captured. The panic location and kernel stack are sufficient
/// to identify the failing operation.
#[cold]
#[inline(never)]
pub(crate) fn panic_to_bugcheck(info: &PanicInfo<'_>) -> ! {
    let (file_address, file_length, line, column) = match info.location() {
        Some(location) => {
            let file = location.file();
            (
                file.as_ptr() as usize,
                file.len(),
                location.line() as usize,
                location.column() as usize,
            )
        }
        None => (0, 0, 0, 0),
    };

    unsafe {
        ke_bug_check_ex(
            PORTMASTER_DRIVER_PANIC,
            file_address,
            file_length,
            line,
            column,
        )
    }
}
