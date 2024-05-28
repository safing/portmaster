use core::cell::UnsafeCell;

use windows_sys::Wdk::System::SystemServices::{
    ExAcquireSpinLockExclusive, ExAcquireSpinLockShared, ExReleaseSpinLockExclusive,
    ExReleaseSpinLockShared,
};

/// A reader-writer spin lock implementation.
///
/// This lock allows multiple readers to access the data simultaneously,
/// but only one writer can access the data at a time. It uses a spin loop
/// to wait for the lock to become available.
pub struct RwSpinLock {
    data: UnsafeCell<i32>,
}

impl RwSpinLock {
    /// Creates a new `RwSpinLock` with the default initial value.
    pub const fn default() -> Self {
        Self {
            data: UnsafeCell::new(0),
        }
    }

    /// Acquires a read lock on the `RwSpinLock`.
    ///
    /// This method blocks until a read lock can be acquired.
    /// Returns a `RwLockGuard` that represents the acquired read lock.
    pub fn read_lock(&self) -> RwLockGuard {
        let irq = unsafe { ExAcquireSpinLockShared(self.data.get()) };
        RwLockGuard {
            data: &self.data,
            exclusive: false,
            old_irq: irq,
        }
    }

    /// Acquires a write lock on the `RwSpinLock`.
    ///
    /// This method blocks until a write lock can be acquired.
    /// Returns a `RwLockGuard` that represents the acquired write lock.
    pub fn write_lock(&self) -> RwLockGuard {
        let irq = unsafe { ExAcquireSpinLockExclusive(self.data.get()) };
        RwLockGuard {
            data: &self.data,
            exclusive: true,
            old_irq: irq,
        }
    }
}

/// Represents a guard for a read-write lock.
pub struct RwLockGuard<'a> {
    data: &'a UnsafeCell<i32>,
    exclusive: bool,
    old_irq: u8,
}

impl<'a> Drop for RwLockGuard<'a> {
    /// Releases the acquired spin lock when the `RwLockGuard` goes out of scope.
    ///
    /// If the lock was acquired exclusively, it releases the spin lock using `ExReleaseSpinLockExclusive`.
    /// If the lock was acquired shared, it releases the spin lock using `ExReleaseSpinLockShared`.
    fn drop(&mut self) {
        unsafe {
            if self.exclusive {
                ExReleaseSpinLockExclusive(self.data.get(), self.old_irq);
            } else {
                ExReleaseSpinLockShared(self.data.get(), self.old_irq);
            }
        }
    }
}
