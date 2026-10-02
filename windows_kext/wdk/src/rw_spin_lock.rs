use core::{
    cell::UnsafeCell,
    marker::PhantomData,
    ops::{Deref, DerefMut},
};

use windows_sys::Wdk::System::SystemServices::{
    ExAcquireSpinLockExclusive, ExAcquireSpinLockShared, ExReleaseSpinLockExclusive,
    ExReleaseSpinLockShared,
};

// EX_SPIN_LOCK is the WDK's volatile LONG. The lock storage below must remain
// exactly that size and alignment on every supported 64-bit target.
#[cfg(target_pointer_width = "64")]
const _: () = {
    assert!(core::mem::size_of::<i32>() == 4);
    assert!(core::mem::align_of::<i32>() == 4);
};

/// A reader-writer spin lock which owns the value it protects.
///
/// The lock and the protected value are one object. Callers can share a
/// `&RwSpinLock<T>` between callbacks, while references to `T` exist only for
/// the lifetime of a lock guard. This makes the synchronization boundary
/// visible to Rust instead of relying on a separate lock beside an unprotected
/// mutable field.
pub struct RwSpinLock<T = ()> {
    lock: UnsafeCell<i32>,
    value: UnsafeCell<T>,
}

impl<T> RwSpinLock<T> {
    /// Creates a lock containing `value`.
    pub const fn new(value: T) -> Self {
        Self {
            lock: UnsafeCell::new(0),
            value: UnsafeCell::new(value),
        }
    }

    /// Acquires a shared lock and returns a read-only guard.
    ///
    /// A shared reference can be exposed only when `T` is safe to share. Values
    /// that are intentionally used exclusively can still use `write_lock`.
    pub fn read_lock(&self) -> RwLockReadGuard<'_, T>
    where
        T: Sync,
    {
        let old_irq = unsafe { ExAcquireSpinLockShared(self.lock.get()) };
        RwLockReadGuard {
            lock: self,
            old_irq,
            // KIRQL is local to the CPU that acquired the spin lock. Guards
            // must not be moved to another CPU or shared across threads.
            _not_send: PhantomData,
        }
    }

    /// Acquires an exclusive lock and returns a mutable guard.
    pub fn write_lock(&self) -> RwLockWriteGuard<'_, T> {
        let old_irq = unsafe { ExAcquireSpinLockExclusive(self.lock.get()) };
        RwLockWriteGuard {
            lock: self,
            old_irq,
            // See the corresponding comment in `read_lock`.
            _not_send: PhantomData,
        }
    }
}

impl RwSpinLock<()> {
    /// Creates a lock without a separately useful protected value.
    pub const fn default() -> Self {
        Self::new(())
    }
}

impl<T: Default> Default for RwSpinLock<T> {
    fn default() -> Self {
        Self::new(T::default())
    }
}

/// Guard for a shared lock acquisition.
pub struct RwLockReadGuard<'a, T> {
    lock: &'a RwSpinLock<T>,
    old_irq: u8,
    _not_send: PhantomData<*mut ()>,
}

impl<T> Deref for RwLockReadGuard<'_, T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        // The shared lock excludes writers for the lifetime of this reference.
        unsafe { &*self.lock.value.get() }
    }
}

impl<T> Drop for RwLockReadGuard<'_, T> {
    fn drop(&mut self) {
        unsafe {
            ExReleaseSpinLockShared(self.lock.lock.get(), self.old_irq);
        }
    }
}

/// Guard for an exclusive lock acquisition.
pub struct RwLockWriteGuard<'a, T> {
    lock: &'a RwSpinLock<T>,
    old_irq: u8,
    _not_send: PhantomData<*mut ()>,
}

impl<T> Deref for RwLockWriteGuard<'_, T> {
    type Target = T;

    fn deref(&self) -> &Self::Target {
        unsafe { &*self.lock.value.get() }
    }
}

impl<T> DerefMut for RwLockWriteGuard<'_, T> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        // No other guard can access the value while this guard is alive.
        unsafe { &mut *self.lock.value.get() }
    }
}

impl<T> Drop for RwLockWriteGuard<'_, T> {
    fn drop(&mut self) {
        unsafe {
            ExReleaseSpinLockExclusive(self.lock.lock.get(), self.old_irq);
        }
    }
}

/// Compatibility name for the old lock-only write guard.
pub type RwLockGuard<'a, T = ()> = RwLockWriteGuard<'a, T>;

// Moving or sharing the lock requires ownership transfer of T. Shared access to
// a non-Sync T is still impossible: `read_lock` has its own `T: Sync` bound, while
// `write_lock` exposes only one exclusive reference. Individual guards remain
// CPU-local.
unsafe impl<T: Send> Sync for RwSpinLock<T> {}
unsafe impl<T: Send> Send for RwSpinLock<T> {}
