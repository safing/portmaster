use alloc::boxed::Box;
use core::{
    cell::UnsafeCell,
    ops::{Deref, DerefMut},
};
use windows_sys::{
    Wdk::{
        Foundation::{FAST_MUTEX, KEVENT},
        System::SystemServices::FM_LOCK_BIT,
    },
    Win32::System::Kernel::{SynchronizationEvent, EVENT_TYPE},
};

// #[link(name = "NtosKrnl", kind = "static")]
extern "C" {
    fn KeInitializeEvent(event: *mut KEVENT, event_type: EVENT_TYPE, state: bool);

    /// The ExAcquireFastMutex routine acquires the given fast mutex with APCs to the current thread disabled.
    fn ExAcquireFastMutex(kmutex: *mut FAST_MUTEX);

    /// The ExTryToAcquireFastMutex routine acquires the given fast mutex, if possible, with APCs to the current thread disabled.
    fn ExTryToAcquireFastMutex(kmutex: *mut FAST_MUTEX) -> bool;

    // The ExReleaseFastMutex routine releases ownership of a fast mutex that was acquired with ExAcquireFastMutex or ExTryToAcquireFastMutex.
    fn ExReleaseFastMutex(kmutex: *mut FAST_MUTEX);
}

/// The ExInitializeFastMutex routine initializes a fast mutex variable, used to synchronize mutually exclusive access by a set of threads to a shared resource.
/// ExInitializeFastMutex must be called before any calls to other ExXxxFastMutex routines occur.
#[allow(non_snake_case)]
unsafe fn ExInitializeFastMutex(kmutex: *mut FAST_MUTEX) {
    core::ptr::write_volatile(&mut (*kmutex).Count, FM_LOCK_BIT as i32);
    // (*kmutex).Count = FM_LOCK_BIT as i32;

    (*kmutex).Owner = core::ptr::null_mut();
    (*kmutex).Contention = 0;
    KeInitializeEvent(&mut (*kmutex).Event, SynchronizationEvent, false)
}

pub struct FastMutex<T> {
    kmutex: UnsafeCell<Option<*mut FAST_MUTEX>>,
    val: UnsafeCell<T>,
}

impl<T> FastMutex<T> {
    pub const fn default(val: T) -> Self {
        Self {
            kmutex: UnsafeCell::new(None),
            val: UnsafeCell::new(val),
        }
    }

    pub fn init(&self) {
        let mutex = Box::into_raw(Box::new(unsafe {
            MaybeUninit::zeroed().assume_init();
        }));
        unsafe {
            ExInitializeFastMutex(mutex);
            *self.kmutex.get() = Some(mutex);
        }
    }

    pub fn deinit(&self) {
        unsafe {
            let opt = &mut (*self.kmutex.get());
            if let Some(mutex) = opt {
                _ = Box::from_raw(mutex);
            }
            opt.take();
        }
    }

    pub fn lock(&self) -> Result<LockGuard<T>, ()> {
        unsafe {
            if let Some(mutex) = *self.kmutex.get() {
                ExAcquireFastMutex(mutex);
                return Ok(LockGuard::new(self));
            }
        }

        return Err(());
    }

    pub fn try_lock(&self) -> Option<LockGuard<T>> {
        unsafe {
            if let Some(mutex) = *self.kmutex.get() {
                ExTryToAcquireFastMutex(mutex);
                return Some(LockGuard::new(self));
            }
        }
        return None;
    }

    fn get<'a>(&self) -> *mut T {
        self.val.get()
    }

    fn unlock(&self) {
        unsafe {
            if let Some(mutex) = *self.kmutex.get() {
                ExReleaseFastMutex(mutex);
            } else {
                panic!("Mutex not initialized");
            }
        }
    }
}

impl<T> Drop for FastMutex<T> {
    fn drop(&mut self) {
        self.deinit();
    }
}

pub struct LockGuard<'a, T> {
    mutex: &'a FastMutex<T>,
}

impl<'a, T> LockGuard<'a, T> {
    fn new(mutex: &'a FastMutex<T>) -> Self {
        return LockGuard { mutex };
    }
}

impl<'a, T> Drop for LockGuard<'a, T> {
    fn drop(&mut self) {
        self.mutex.unlock();
    }
}

impl<'a, T> Deref for LockGuard<'a, T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        unsafe { &*self.mutex.get() }
    }
}

impl<'a, T> DerefMut for LockGuard<'a, T> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        unsafe { &mut *self.mutex.get() }
    }
}
