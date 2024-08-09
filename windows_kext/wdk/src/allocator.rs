extern crate alloc;

use core::alloc::{GlobalAlloc, Layout};

use alloc::alloc::handle_alloc_error;
use windows_sys::Wdk::System::SystemServices::{ExAllocatePool2, ExFreePoolWithTag};

// For reference: https://learn.microsoft.com/en-us/windows-hardware/drivers/kernel/pool_flags
#[allow(dead_code)]
#[repr(u64)]
enum PoolType {
    RequiredStartUseQuota = 0x0000000000000001,
    Uninitialized = 0x0000000000000002, // Don't zero-initialize allocation
    Session = 0x0000000000000004,       // Use session specific pool
    CacheAligned = 0x0000000000000008,  // Cache aligned allocation
    RaiseOnFailure = 0x0000000000000020, // Raise exception on failure
    NonPaged = 0x0000000000000040,      // Non paged pool NX
    NonPagedExecute = 0x0000000000000080, // Non paged pool executable
    Paged = 0x0000000000000100,         // Paged pool
    RequiredEnd = 0x0000000080000000,
    OptionalStart = 0x0000000100000000,
    OptionalEnd = 0x8000000000000000,
}

pub struct WindowsAllocator {}

unsafe impl Sync for WindowsAllocator {}

pub(crate) const POOL_TAG: u32 = u32::from_ne_bytes(*b"PMrs");

unsafe impl GlobalAlloc for WindowsAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let pool = ExAllocatePool2(PoolType::NonPaged as u64, layout.size(), POOL_TAG);
        if pool.is_null() {
            handle_alloc_error(layout);
        }

        pool as *mut u8
    }

    unsafe fn dealloc(&self, ptr: *mut u8, _: Layout) {
        ExFreePoolWithTag(ptr as _, POOL_TAG);
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        
        self.alloc(layout)
    }

    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        // SAFETY: the caller must ensure that the `new_size` does not overflow.
        // `layout.align()` comes from a `Layout` and is thus guaranteed to be valid.
        let new_layout = unsafe { Layout::from_size_align_unchecked(new_size, layout.align()) };
        // SAFETY: the caller must ensure that `new_layout` is greater than zero.
        let new_ptr = unsafe { self.alloc(new_layout) };
        if !new_ptr.is_null() {
            // SAFETY: the previously allocated block cannot overlap the newly allocated block.
            // The safety contract for `dealloc` must be upheld by the caller.
            unsafe {
                core::ptr::copy_nonoverlapping(
                    ptr,
                    new_ptr,
                    core::cmp::min(layout.size(), new_size),
                );
                self.dealloc(ptr, layout);
            }
        }
        new_ptr
    }
}
