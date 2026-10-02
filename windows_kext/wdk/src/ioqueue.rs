use core::{
    cell::UnsafeCell,
    ffi::c_void,
    fmt::Display,
    marker::PhantomData,
    mem::MaybeUninit,
    pin::Pin,
    sync::atomic::{AtomicBool, Ordering},
};

use crate::{dbg, rw_spin_lock::RwSpinLock};
use alloc::boxed::Box;
use windows_sys::{
    Wdk::Foundation::KQUEUE,
    Win32::{
        Foundation::{STATUS_ABANDONED, STATUS_TIMEOUT, STATUS_USER_APC},
        System::Kernel::LIST_ENTRY,
    },
};

// IOQueue owns the resident KQUEUE storage that KeInitializeQueue writes.
#[cfg(target_pointer_width = "64")]
const _: () = {
    assert!(core::mem::size_of::<LIST_ENTRY>() == 16);
    assert!(core::mem::align_of::<LIST_ENTRY>() == 8);
    assert!(core::mem::size_of::<KQUEUE>() == 64);
    assert!(core::mem::align_of::<KQUEUE>() == 8);
    assert!(core::mem::offset_of!(Entry<()>, list) == 0);
};

#[derive(Debug)]
pub enum Status {
    Uninitialized,
    Timeout,
    UserAPC,
    Abandoned,
    Cancelled,
    InvalidResult,
}

impl Display for Status {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Status::Uninitialized => write!(f, "Uninitialized"),
            Status::Timeout => write!(f, "Timeout"),
            Status::UserAPC => write!(f, "UserAPC"),
            Status::Abandoned => write!(f, "Abandoned"),
            Status::Cancelled => write!(f, "Cancelled"),
            Status::InvalidResult => write!(f, "InvalidResult"),
        }
    }
}

// KPROCESSOR_MODE is a CCHAR typedef in the WDK, not a native C enum.
#[repr(i8)]
pub enum KprocessorMode {
    KernelMode = 0,
    UserMode = 1,
}

// #[link(name = "NtosKrnl", kind = "static")]
extern "system" {
    /*
    KeInitializeQueue
        [out] Queue
        Pointer to a KQUEUE structure for which the caller must provide resident storage in nonpaged pool. This structure is defined as follows:

        [in] Count
        The maximum number of threads for which the waits on the queue object can be satisfied concurrently. If this parameter is not supplied, the number of processors in the machine is used.
    */
    fn KeInitializeQueue(queue: *mut KQUEUE, count: u32);
    /*
    KeInsertQueue returns the previous signal state of the given Queue. If it was set to zero (that is, not signaled) before KeInsertQueue was called, KeInsertQueue returns zero, meaning that no entries were queued. If it was nonzero (signaled), KeInsertQueue returns the number of entries that were queued before KeInsertQueue was called.
    */
    fn KeInsertQueue(queue: *mut KQUEUE, list_entry: *mut c_void) -> i32;
    /*
    KeRemoveQueue returns one of the following:
        A pointer to a dequeued entry from the given queue object, if one is available
        STATUS_TIMEOUT, if the given Timeout interval expired before an entry became available
        STATUS_USER_APC, if a user-mode APC was delivered in the context of the calling thread
        STATUS_ABANDONED, if the queue has been run down
    */
    fn KeRemoveQueue(
        queue: *mut KQUEUE,
        waitmode: KprocessorMode,
        timeout: *const i64,
    ) -> *mut LIST_ENTRY;

    // If the queue is empty, KeRundownQueue returns NULL; otherwise, it returns the address of the first entry in the queue.
    fn KeRundownQueue(queue: *mut KQUEUE) -> *mut LIST_ENTRY;
}

#[repr(C)]
struct Entry<T> {
    list: LIST_ENTRY, // Internal use
    entry: T,
}

pub struct IOQueue<T> {
    // The address of the value should not change.
    kernel_queue: Pin<Box<UnsafeCell<KQUEUE>>>,
    // Set while a handle cleanup or driver unload is releasing blocked reads.
    // Reads poll the KQUEUE with a short timeout and observe this flag.
    cancelled: AtomicBool,
    // Serializes insert and rundown. `initialized` alone cannot prevent an
    // insertion that passed its load from racing KeRundownQueue.
    lifecycle_lock: RwSpinLock<()>,
    initialized: AtomicBool,
    _type: PhantomData<T>, // 0 size variable. Required for the generic to work properly. Compiler limitation.
}

unsafe impl<T: Send> Sync for IOQueue<T> {}
unsafe impl<T: Send> Send for IOQueue<T> {}

impl<T> IOQueue<T> {
    /// Make sure `rundown` is called on exit, if `drop()` is not called for queue.
    pub fn new() -> Self {
        unsafe {
            let kernel_queue = Box::pin(UnsafeCell::new(MaybeUninit::zeroed().assume_init()));
            KeInitializeQueue(kernel_queue.get(), 1);

            Self {
                kernel_queue,
                cancelled: AtomicBool::new(false),
                lifecycle_lock: RwSpinLock::new(()),
                initialized: AtomicBool::new(true),
                _type: PhantomData,
            }
        }
    }

    /// Pushes new entry of any type.
    pub fn push(&self, entry: T) -> Result<(), Status> {
        let kqueue = self.kernel_queue.get();
        // Allocate entry.
        let list_entry = Box::new(Entry {
            list: LIST_ENTRY {
                Flink: core::ptr::null_mut(),
                Blink: core::ptr::null_mut(),
            },
            entry,
        });
        let raw_ptr = Box::into_raw(list_entry);

        // Serialize this initialized check with rundown. Without the lock, a
        // producer can observe true, lose the CPU while KeRundownQueue drains,
        // then insert into a queue that will never be consumed or freed.
        let result = {
            let _lifecycle_guard = self.lifecycle_lock.write_lock();
            if self.initialized.load(Ordering::Acquire) {
                unsafe { KeInsertQueue(kqueue, raw_ptr as *mut c_void) }
            } else {
                -1
            }
        };
        // There is no documentation that rundown queue will return error. This is here just for good measures.
        // It is unlikely to happen and not critical.
        if result >= 0 {
            return Ok(());
        }

        // Reclaim outside the spin lock so an arbitrary T destructor never runs
        // at the lock-raised DISPATCH_LEVEL.
        _ = unsafe { Box::from_raw(raw_ptr) };
        return Err(Status::Uninitialized);
    }

    /// Marks all current and future waits as canceled. The flag remains set
    /// until a new owner calls `reset_cancellation`.
    pub fn cancel_waiters(&self) {
        self.cancelled.store(true, Ordering::Release);
    }

    /// Clears the cancellation state for a new owner session.
    ///
    /// The caller must first close read admission and wait until all readers
    /// have returned. This method must run at PASSIVE_LEVEL.
    pub fn reset_cancellation(&self) {
        self.cancelled.store(false, Ordering::Release);
    }

    /// Returns an Element or a status.
    fn pop_internal(&self, timeout: Option<&i64>) -> Result<T, Status> {
        unsafe {
            let kqueue = self.kernel_queue.get();
            let timeout = timeout.map_or(core::ptr::null(), core::ptr::from_ref);
            // Check if initialized.
            if self.initialized.load(Ordering::Acquire) {
                // Pop and check the return value.
                let list_entry =
                    KeRemoveQueue(kqueue, KprocessorMode::KernelMode, timeout) as *mut Entry<T>;
                let result = list_entry as usize;
                match result {
                    status
                        if status == STATUS_TIMEOUT as u32 as usize
                            || status == STATUS_TIMEOUT as isize as usize =>
                    {
                        return Err(Status::Timeout)
                    }
                    status
                        if status == STATUS_USER_APC as u32 as usize
                            || status == STATUS_USER_APC as isize as usize =>
                    {
                        return Err(Status::UserAPC)
                    }
                    status
                        if status == STATUS_ABANDONED as u32 as usize
                            || status == STATUS_ABANDONED as isize as usize =>
                    {
                        return Err(Status::Abandoned)
                    }
                    0 => return Err(Status::InvalidResult),
                    // A status cast to a pointer can be either zero-extended or
                    // sign-extended by the native implementation; the guards above
                    // accept both complete representations. Reject every remaining
                    // low value without truncating a valid x64 kernel address to its
                    // low 32 bits.
                    status if status <= u32::MAX as usize => {
                        return Err(Status::InvalidResult)
                    }
                    _ => {
                        let list_entry = Box::from_raw(list_entry);
                        return Ok(list_entry.entry);
                    }
                }
            }
        }

        Err(Status::Uninitialized)
    }

    /// Returns element or a status. Waits until element is pushed or the queue is interrupted.
    pub fn wait_and_pop(&self) -> Result<T, Status> {
        // No timeout.
        self.pop_internal(None)
    }

    /// Waits for an entry while remaining responsive to the owning handle's
    /// cleanup. `KeRemoveQueue` has no cancellation object, so use a bounded
    /// relative wait and check the cancellation flag between waits. This queue
    /// remains synchronous; handle cleanup, rather than `CancelIoEx`, owns the
    /// cancellation transition.
    pub fn wait_and_pop_cancellable(&self) -> Result<T, Status> {
        // Ten milliseconds bounds the time spent in the dispatch routine after
        // IRP_MJ_CLEANUP signals cancellation without creating a permanent
        // polling event or a cancel-routine race for this active IRP.
        const WAIT_SLICE_MS: i64 = 10;

        loop {
            if self.cancelled.load(Ordering::Acquire) {
                return Err(Status::Cancelled);
            }
            if !self.initialized.load(Ordering::Acquire) {
                return Err(Status::Uninitialized);
            }

            match self.pop_timeout(WAIT_SLICE_MS) {
                Ok(entry) => {
                    // Cleanup may race the dequeue. Do not deliver an entry to
                    // a handle after its cancellation has been observed.
                    if self.cancelled.load(Ordering::Acquire) {
                        return Err(Status::Cancelled);
                    }
                    return Ok(entry);
                }
                Err(Status::Timeout) => continue,
                Err(status) => return Err(status),
            }
        }
    }

    /// Returns element or a status. Does not wait.
    pub fn pop(&self) -> Result<T, Status> {
        let timeout: i64 = 0;
        self.pop_internal(Some(&timeout))
    }

    /// Returns element or a status. Waits the specified timeout.
    pub fn pop_timeout(&self, timeout: i64) -> Result<T, Status> {
        let timeout_ptr = timeout.saturating_mul(-10_000);
        self.pop_internal(Some(&timeout_ptr))
    }

    /// Removes all elements and frees all the memory. The object can't be used after this function is called.
    pub fn rundown(&self) {
        self.cancel_waiters();
        unsafe {
            let kqueue = self.kernel_queue.get();
            if kqueue.is_null() {
                return;
            }

            // Exclude insertions from the initialized transition through the
            // native list detach. Producers that arrive later observe false and
            // reclaim their own allocation instead of touching the dead queue.
            let list_entries = {
                let _lifecycle_guard = self.lifecycle_lock.write_lock();
                if self.initialized.swap(false, Ordering::AcqRel) {
                    KeRundownQueue(kqueue)
                } else {
                    core::ptr::null_mut()
                }
            };

            // KeRundownQueue returns a detached circular list. Destructors are
            // arbitrary T code, so traverse and free it only after restoring the
            // caller's original IRQL on release of lifecycle_lock.
            if !list_entries.is_null() {
                let mut entry = list_entries;
                loop {
                    let next = (*entry).Flink;
                    dbg!("discarding entry");
                    let _ = Box::from_raw(entry as *mut Entry<T>);
                    if core::ptr::eq(next, list_entries) {
                        break;
                    }
                    entry = next;
                }
            }
        }
    }
}

impl<T> Drop for IOQueue<T> {
    fn drop(&mut self) {
        // Reinitialize queue.
        self.rundown();
        unsafe {
            let ptr = self.kernel_queue.get();
            if !ptr.is_null() {
                *ptr = MaybeUninit::zeroed().assume_init();
            }
        }
    }
}
