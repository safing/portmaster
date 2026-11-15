#![cfg_attr(not(test), no_std)]
#![no_main]
#![allow(clippy::needless_return)]

extern crate alloc;

mod ale_callouts;
mod array_holder;
mod bandwidth;
mod callouts;
mod common;
mod connection;
mod connection_cache;
mod connection_map;
mod device;
mod entry;
mod id_cache;
pub mod logger;
mod packet_callouts;
mod packet_util;
mod stream_callouts;

use wdk::allocator::WindowsAllocator;

// For consistent behavior during development and production only release mode should be used.
// Certain behavior of the compiler will change and this can result in errors and different behavior in debug and release mode.
#[cfg(debug_assertions)]
compile_error!("Must be built in release mode to ensure consistent behavior and prevent optimization-related issues. Use `cargo build --release`.");

#[cfg(not(test))]
use core::panic::PanicInfo;

// Declaration of the global memory allocator
#[global_allocator]
static HEAP: WindowsAllocator = WindowsAllocator {};

#[no_mangle]
pub extern "system" fn _DllMainCRTStartup() {}

#[cfg(not(test))]
#[panic_handler]
fn panic(info: &PanicInfo) -> ! {
    use core::fmt::Write;
    use core::sync::atomic::{AtomicBool, Ordering};

    // Nothing on this path may allocate: the panic may itself be a failed
    // allocation. Spinning here instead would pin this CPU at the current
    // IRQL and end in a misleading DPC_WATCHDOG_VIOLATION minutes later; a
    // bug check with the driver's own code stops the system immediately and
    // points the crash dump at the panic location.
    let (file, line) = match info.location() {
        Some(location) => (location.file(), location.line()),
        None => ("", 0),
    };

    // Formatting the message is the only step below that can panic again,
    // and under memory pressure several CPUs can panic at the same time. In
    // both cases skip the formatting and report the location alone; the
    // empty message marks that degraded path. Reading the location above is
    // a plain field access and cannot panic.
    static IN_PANIC: AtomicBool = AtomicBool::new(false);
    if IN_PANIC.swap(true, Ordering::Relaxed) {
        wdk::utils::bug_check(file, line, "");
    }

    // Format the message into a stack buffer: the crashing thread's stack is
    // part of even a small crash dump, and a formatted message keeps details
    // that exist only at runtime (an allocation failure reports its size
    // here, while its location points into Rust's alloc library). The buffer
    // is zeroed and the writer leaves the last byte untouched, so the
    // message the debugger prints is NUL terminated.
    let mut buffer = [0u8; 192];
    let mut writer = TruncatingWriter {
        buffer: &mut buffer,
        written: 0,
    };
    let _ = write!(writer, "{}", info.message());
    let written = writer.written;
    let message = core::str::from_utf8(&buffer[..written]).unwrap_or("");
    wdk::utils::bug_check(file, line, message);
}

#[cfg(not(test))]
struct TruncatingWriter<'a> {
    buffer: &'a mut [u8],
    written: usize,
}

#[cfg(not(test))]
impl core::fmt::Write for TruncatingWriter<'_> {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        // Keep the last byte free as the NUL terminator and truncate on a
        // character boundary, so the written bytes stay valid UTF-8.
        let space = (self.buffer.len() - 1).saturating_sub(self.written);
        let mut len = s.len().min(space);
        while !s.is_char_boundary(len) {
            len -= 1;
        }
        self.buffer[self.written..self.written + len].copy_from_slice(&s.as_bytes()[..len]);
        self.written += len;
        Ok(())
    }
}
