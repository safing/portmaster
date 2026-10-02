#![cfg_attr(not(test), no_std)]
#![cfg_attr(not(test), no_main)]
#![allow(clippy::needless_return)]
#![deny(unsafe_op_in_unsafe_fn)]

extern crate alloc;

// Unit tests link as a user-mode executable, where the WFP/NDIS kernel imports
// used by the other modules are unavailable. Keep the pure connection modules
// in that graph and compile the full driver graph for every non-test build.
#[cfg(not(test))]
mod ale_callouts;
mod ale_policy;
mod array_holder;
#[cfg(not(test))]
mod bandwidth;
#[cfg(not(test))]
mod bugcheck;
#[cfg(not(test))]
mod callouts;
mod common;
mod connection;
mod connection_cache;
mod connection_map;
#[cfg(not(test))]
mod device;
#[cfg(not(test))]
mod entry;
#[cfg(not(test))]
mod icmp_echo_cache;
mod id_cache;
mod ipv6_packet;
#[cfg(not(test))]
pub mod logger;
#[cfg(not(test))]
mod packet_callouts;
mod packet_metadata;
#[cfg(not(test))]
mod packet_util;
#[cfg(not(test))]
mod stream_callouts;
mod tcp_closure_cache;
mod tcp_endpoint_cache;
mod udp_endpoint_cache;
mod udp_flow_cache;

#[cfg(not(test))]
use wdk::allocator::WindowsAllocator;

// For consistent behavior during development and production only release mode should be used.
// Certain behavior of the compiler will change and this can result in errors and different behavior in debug and release mode.
#[cfg(debug_assertions)]
compile_error!("Must be built in release mode to ensure consistent behavior and prevent optimization-related issues. Use `cargo build --release`.");

#[cfg(not(test))]
use core::panic::PanicInfo;

// Declaration of the global memory allocator
#[cfg(not(test))]
#[global_allocator]
static HEAP: WindowsAllocator = WindowsAllocator {};

#[cfg(not(test))]
#[no_mangle]
pub extern "system" fn _DllMainCRTStartup() {}

#[cfg(not(test))]
#[panic_handler]
fn panic(info: &PanicInfo<'_>) -> ! {
    bugcheck::panic_to_bugcheck(info)
}
