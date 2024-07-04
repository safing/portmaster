#![cfg_attr(not(test), no_std)]
#![allow(clippy::needless_return)]

extern crate alloc;

pub mod allocator;
pub mod consts;
pub mod debug;
pub mod driver;
pub mod error;
pub mod filter_engine;
pub mod interface;
pub mod ioqueue;
pub mod irp_helpers;
pub mod rw_spin_lock;
pub mod spin_lock;
pub mod utils;

#[allow(dead_code)]
pub mod ffi;

// Needed by the linker for legacy reasons. Not important for rust.
#[cfg(not(test))]
#[export_name = "_fltused"]
static _FLTUSED: i32 = 0;

// Needed by the compiler but not used.
#[cfg(not(test))]
#[no_mangle]
pub extern "system" fn __CxxFrameHandler3(_: *mut u8, _: *mut u8, _: *mut u8, _: *mut u8) -> i32 {
    0
}
