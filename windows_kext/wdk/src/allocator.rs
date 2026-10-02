use core::alloc::{GlobalAlloc, Layout};

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

// ExAllocatePool2 guarantees 16-byte alignment for sub-page allocations on the
// 64-bit targets supported by this driver. GlobalAlloc must additionally honor
// layouts with larger alignments.
const POOL_ALIGNMENT: usize = 16;

unsafe impl GlobalAlloc for WindowsAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        if layout.size() == 0 {
            return core::ptr::null_mut();
        }

        if layout.align() <= POOL_ALIGNMENT {
            // SAFETY: `layout` has a nonzero size, and nonpaged NX pool is valid
            // for allocator calls made at any IRQL at which this driver allocates.
            return unsafe {
                ExAllocatePool2(PoolType::NonPaged as u64, layout.size(), POOL_TAG) as *mut u8
            };
        }

        // Reserve one pointer-sized header plus enough slack to align the user
        // address. Store the original pool pointer immediately before that address
        // so deallocation can return the exact allocation to ExFreePoolWithTag.
        let Some(extra) = layout
            .align()
            .checked_sub(1)
            .and_then(|slack| slack.checked_add(core::mem::size_of::<*mut u8>()))
        else {
            return core::ptr::null_mut();
        };
        let Some(allocation_size) = layout.size().checked_add(extra) else {
            return core::ptr::null_mut();
        };

        // SAFETY: `allocation_size` is nonzero and was checked for overflow. The
        // returned nonpaged allocation is released with the same pool tag below.
        let base = unsafe {
            ExAllocatePool2(PoolType::NonPaged as u64, allocation_size, POOL_TAG) as *mut u8
        };
        if base.is_null() {
            return core::ptr::null_mut();
        }

        let address = match (base as usize).checked_add(core::mem::size_of::<*mut u8>()) {
            Some(address) => address,
            None => {
                // SAFETY: `base` is the live pool allocation returned above.
                unsafe { ExFreePoolWithTag(base as _, POOL_TAG) };
                return core::ptr::null_mut();
            }
        };
        let aligned_address = match address.checked_add(layout.align() - 1) {
            Some(address) => address & !(layout.align() - 1),
            None => {
                // SAFETY: `base` is the live pool allocation returned above.
                unsafe { ExFreePoolWithTag(base as _, POOL_TAG) };
                return core::ptr::null_mut();
            }
        };
        let aligned = aligned_address as *mut u8;
        // SAFETY: `allocation_size` reserved a pointer-sized header plus the full
        // alignment slack. Therefore the header address is aligned for a pointer
        // and lies inside the live allocation immediately before `aligned`.
        unsafe {
            aligned
                .sub(core::mem::size_of::<*mut u8>())
                .cast::<*mut u8>()
                .write(base);
        }
        aligned
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        if ptr.is_null() || layout.size() == 0 {
            return;
        }

        let base = if layout.align() <= POOL_ALIGNMENT {
            ptr
        } else {
            // SAFETY: for an over-aligned layout, `alloc` stored the original
            // allocation pointer in the pointer-sized header immediately before
            // this user pointer. The GlobalAlloc contract pairs both layouts.
            unsafe {
                ptr.sub(core::mem::size_of::<*mut u8>())
                    .cast::<*mut u8>()
                    .read()
            }
        };
        // SAFETY: `base` is the exact still-live pool allocation associated with
        // `ptr`, and this is its single GlobalAlloc deallocation.
        unsafe { ExFreePoolWithTag(base as _, POOL_TAG) };
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        // ExAllocatePool2 zero-initializes unless POOL_FLAG_UNINITIALIZED is set.
        // SAFETY: this method forwards the caller's GlobalAlloc contract unchanged.
        unsafe { self.alloc(layout) }
    }

    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        let Ok(new_layout) = Layout::from_size_align(new_size, layout.align()) else {
            return core::ptr::null_mut();
        };
        // SAFETY: `new_layout` was validated above and this method forwards the
        // caller's GlobalAlloc allocation-context contract.
        let new_ptr = unsafe { self.alloc(new_layout) };
        if !new_ptr.is_null() {
            // SAFETY: the caller guarantees `ptr` names a live allocation described
            // by `layout`; `new_ptr` names a distinct allocation of `new_size`
            // bytes. The copied range is bounded by both allocations.
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
