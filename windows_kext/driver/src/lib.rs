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
    use wdk::err;

    err!("{}", info);
    loop {}
}
