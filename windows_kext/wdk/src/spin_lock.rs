use core::{ffi::c_void, mem::MaybeUninit, ptr};

use windows_sys::Wdk::System::SystemServices::{
    KeAcquireInStackQueuedSpinLock, KeInitializeSpinLock, KeReleaseInStackQueuedSpinLock,
    KLOCK_QUEUE_HANDLE,
};

// Copy of KSPIN_LOCK_QUEUE WDK C struct
#[repr(C)]
#[allow(dead_code)]
struct KSpinLockQueue {
    next: *mut c_void, // struct _KSPIN_LOCK_QUEUE * volatile Next;
    lock: *mut c_void, // PKSPIN_LOCK volatile Lock;
}

// Copy of KLOCK_QUEUE_HANDLE WDK C struct
pub struct KLockQueueHandle {
    lock: KLOCK_QUEUE_HANDLE,
}

// Copy of KSpinLock WDK C struct
#[repr(C)]
pub struct KSpinLock {
    ptr: *mut usize,
}

impl KSpinLock {
    pub fn create() -> Self {
        unsafe {
            let p: KSpinLock = KSpinLock {
                ptr: ptr::null_mut(),
            };
            KeInitializeSpinLock(p.ptr);
            return p;
        }
    }

    pub fn lock(&mut self) -> KLockQueueHandle {
        unsafe {
            let mut handle = MaybeUninit::zeroed().assume_init();
            KeAcquireInStackQueuedSpinLock(self.ptr, &mut handle);
            KLockQueueHandle { lock: handle }
        }
    }
}

impl Drop for KLockQueueHandle {
    fn drop(&mut self) {
        unsafe {
            KeReleaseInStackQueuedSpinLock(&mut self.lock);
        }
    }
}
