use core::{
    cell::UnsafeCell,
    ffi::c_void,
    fmt::Display,
    marker::PhantomData,
    mem::MaybeUninit,
    pin::Pin,
    sync::atomic::{AtomicBool, Ordering},
};

use crate::dbg;
use alloc::boxed::Box;
use ntstatus::ntstatus::NtStatus;
use windows_sys::{Wdk::Foundation::KQUEUE, Win32::System::Kernel::LIST_ENTRY};

#[derive(Debug)]
pub enum Status {
    Uninitialized,
    Timeout,
    UserAPC,
    Abandoned,
}

impl Display for Status {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Status::Uninitialized => write!(f, "Uninitialized"),
            Status::Timeout => write!(f, "Timeout"),
            Status::UserAPC => write!(f, "UserAPC"),
            Status::Abandoned => write!(f, "Abandoned"),
        }
    }
}

#[repr(i8)]
pub enum KprocessorMode {
    KernelMode = 0,
    UserMode = 1,
}

// #[link(name = "NtosKrnl", kind = "static")]
extern "C" {
    /*
    KeInitializeQueue
        [out] Queue
        Pointer to a KQUEUE structure for which the caller must provide resident storage in nonpaged pool. This structure is defined as follows:

        [in] Count
        The maximum number of threads for which the waits on the queue object can be satisfied concurrently. If this parameter is not supplied, the number of processors in the machine is used.
    */
    fn KeInitializeQueue(queue: *mut KQUEUE, count: u64);
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
    initialized: AtomicBool,
    _type: PhantomData<T>, // 0 size variable. Required for the generic to work properly. Compiler limitation.
}

unsafe impl<T> Sync for IOQueue<T> {}

impl<T> IOQueue<T> {
    /// Make sure `rundown` is called on exit, if `drop()` is not called for queue.
    pub fn new() -> Self {
        unsafe {
            let kernel_queue = Box::pin(UnsafeCell::new(MaybeUninit::zeroed().assume_init()));
            KeInitializeQueue(kernel_queue.get(), 1);

            Self {
                kernel_queue,
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

        // Check if initialized.
        let result = if self.initialized.load(Ordering::Acquire) {
            unsafe { KeInsertQueue(kqueue, raw_ptr as *mut c_void) }
        } else {
            -1
        };
        // There is no documentation that rundown queue will return error. This is here just for good measures.
        // It is unlikely to happen and not critical.
        if result >= 0 {
            return Ok(());
        }

        _ = unsafe { Box::from_raw(raw_ptr) };
        return Err(Status::Uninitialized);
    }

    /// Returns an Element or a status.
    fn pop_internal(&self, timeout: *const i64) -> Result<T, Status> {
        unsafe {
            let kqueue = self.kernel_queue.get();
            // Check if initialized.
            if self.initialized.load(Ordering::Acquire) {
                // Pop and check the return value.
                let list_entry =
                    KeRemoveQueue(kqueue, KprocessorMode::KernelMode, timeout) as *mut Entry<T>;
                let error_code = NtStatus::try_from(list_entry as u32);
                match error_code {
                    Ok(NtStatus::STATUS_TIMEOUT) => return Err(Status::Timeout),
                    Ok(NtStatus::STATUS_USER_APC) => return Err(Status::UserAPC),
                    Ok(NtStatus::STATUS_ABANDONED) => return Err(Status::Abandoned),
                    _ => {
                        // The return value is a pointer.
                        let list_entry = Box::from_raw(list_entry);
                        let entry = list_entry.entry;
                        return Ok(entry);
                    }
                }
            }
        }

        Err(Status::Uninitialized)
    }

    /// Returns element or a status. Waits until element is pushed or the queue is interrupted.
    pub fn wait_and_pop(&self) -> Result<T, Status> {
        // No timeout.
        self.pop_internal(core::ptr::null())
    }

    /// Returns element or a status. Does not wait.
    pub fn pop(&self) -> Result<T, Status> {
        let timeout: i64 = 0;
        self.pop_internal(&timeout)
    }

    /// Returns element or a status. Waits the specified timeout.
    pub fn pop_timeout(&self, timeout: i64) -> Result<T, Status> {
        let timeout_ptr: i64 = timeout * -10000;
        self.pop_internal(&timeout_ptr)
    }

    /// Removes all elements and frees all the memory. The object can't be used after this function is called.
    pub fn rundown(&self) {
        unsafe {
            let kqueue = self.kernel_queue.get();
            if kqueue.is_null() {
                return;
            }

            // Check if initialized.
            if self.initialized.swap(false, Ordering::Acquire) {
                // Remove and free all elements from the queue.
                let list_entries: *mut LIST_ENTRY = KeRundownQueue(kqueue);
                if !list_entries.is_null() {
                    let mut entry = list_entries;
                    while !core::ptr::eq((*entry).Flink, list_entries) {
                        let next = (*entry).Flink;
                        dbg!("discarding entry");
                        let _ = Box::from_raw(entry as *mut Entry<T>);
                        entry = next;
                    }
                    dbg!("discarding last entry");
                    let _ = Box::from_raw(entry as *mut Entry<T>);
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
