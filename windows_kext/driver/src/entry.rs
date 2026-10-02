use crate::common::ControlCode;
use crate::device;
use alloc::boxed::Box;
use core::{
    cell::UnsafeCell,
    marker::PhantomData,
    sync::atomic::{AtomicBool, AtomicPtr, Ordering},
};
use num_traits::FromPrimitive;
use wdk::irp_helpers::{
    CleanupRequest, CloseRequest, CreateRequest, DeviceControlRequest, ReadRequest, WriteRequest,
};
use wdk::{err, info, interface, rw_spin_lock::RwSpinLock};
use windows_sys::Wdk::{
    Foundation::IRP,
    System::SystemServices::{
        ExAcquireRundownProtection, ExInitializeRundownProtection,
        ExReInitializeRundownProtection, ExReleaseRundownProtection, ExRundownCompleted,
        ExWaitForRundownProtectionRelease, EX_RUNDOWN_REF, EX_RUNDOWN_REF_0,
    },
};
use windows_sys::Win32::Foundation::{
    NTSTATUS, STATUS_BUFFER_TOO_SMALL, STATUS_DEVICE_NOT_READY, STATUS_INVALID_DEVICE_STATE,
    STATUS_INVALID_PARAMETER, STATUS_SHARING_VIOLATION, STATUS_SUCCESS,
};

static VERSION: [u8; 4] = include!("../../kextinterface/version.txt");

/// Global device pointer.
///
/// We use `AtomicPtr` to ensure thread safety.
/// - **Safety**: Prevents data races and acts as a compiler barrier against dangerous optimizations
///   (e.g., load hoisting), ensuring concurrent callouts see a valid, up-to-date pointer.
/// - **Performance**: Negligible overhead. On x64, `Acquire` is free (same as a normal load).
///   On ARM64, it uses efficient hardware-supported load-acquire instructions.
static DEVICE: AtomicPtr<device::Device> = AtomicPtr::new(core::ptr::null_mut());

/// Serializes user-mode dispatch routines with driver teardown.
///
/// The control device is a singleton and the driver accepts only one file object,
/// so a lightweight gate is sufficient. A dispatch increments the active count
/// only while unload is not closing the gate. Unload drains ordinary dispatches,
/// runs the existing shutdown path to wake blocked reads, and then waits for those
/// reads before destroying the WDFDEVICE-backed Device allocation.
struct DispatchGate {
    /// Serializes the short admission/closure transition. It is held only
    /// while counters and admission flags are updated; readers never wait for
    /// Device work while holding it.
    admission_lock: RwSpinLock<()>,
    /// Counts the instructions in every admitted preprocess callback, including
    /// callbacks that arrive after admission has closed and only complete their
    /// IRP. This is separate from the Device-access counters below.
    dispatch_lifetime: UnsafeCell<EX_RUNDOWN_REF>,
    lifetime_initialized: AtomicBool,
    unloading: AtomicBool,
    active: core::sync::atomic::AtomicU32,
    non_read: core::sync::atomic::AtomicU32,
    reads_closed: AtomicBool,
    reads: core::sync::atomic::AtomicU32,
    session_busy: AtomicBool,
}

// The rundown object is accessed only through the interlocked kernel APIs, and
// lifecycle transitions are serialized by DriverEntry/DriverUnload. The other
// fields already provide their own synchronization.
unsafe impl Sync for DispatchGate {}

impl DispatchGate {
    const fn empty_rundown_ref() -> EX_RUNDOWN_REF {
        EX_RUNDOWN_REF {
            Anonymous: EX_RUNDOWN_REF_0 { Count: 0 },
        }
    }

    const fn new() -> Self {
        Self {
            admission_lock: RwSpinLock::new(()),
            dispatch_lifetime: UnsafeCell::new(Self::empty_rundown_ref()),
            lifetime_initialized: AtomicBool::new(false),
            unloading: AtomicBool::new(false),
            active: core::sync::atomic::AtomicU32::new(0),
            non_read: core::sync::atomic::AtomicU32::new(0),
            reads_closed: AtomicBool::new(false),
            reads: core::sync::atomic::AtomicU32::new(0),
            session_busy: AtomicBool::new(false),
        }
    }

    /// Reopens the gate for a new driver instance. The gate is static because
    /// dispatch routines may outlive the stack frame of DriverEntry, so it must
    /// be explicitly reset after a previous unload before a service restart.
    fn reopen(&self) {
        let was_initialized = {
            let _admission_guard = self.acquire_admission();
            debug_assert_eq!(self.active.load(Ordering::Acquire), 0);
            debug_assert_eq!(self.non_read.load(Ordering::Acquire), 0);
            debug_assert_eq!(self.reads.load(Ordering::Acquire), 0);
            debug_assert!(!self.session_busy.load(Ordering::Acquire));

            // Keep callbacks out while the rundown reference is reinitialized.
            // The reference APIs below require IRQL <= APC_LEVEL, so this flag
            // must be published before releasing the spin lock rather than
            // reinitializing the reference while that lock is held at DISPATCH_LEVEL.
            let was_initialized = self.lifetime_initialized.load(Ordering::Acquire);
            self.lifetime_initialized.store(false, Ordering::Release);
            self.reads_closed.store(true, Ordering::Release);
            self.unloading.store(true, Ordering::Release);
            was_initialized
        };

        // DriverEntry runs at PASSIVE_LEVEL. ExInitialize/ExReInitialize have an
        // APC-level limit and therefore must not be called while admission_lock is
        // held (RwSpinLock raises the current IRQL to DISPATCH_LEVEL).
        unsafe {
            if was_initialized {
                // The previous unload waited for and completed this reference
                // before the static gate can be reused.
                ExReInitializeRundownProtection(self.dispatch_lifetime.get());
            } else {
                ExInitializeRundownProtection(self.dispatch_lifetime.get());
            }
        }

        {
            let _admission_guard = self.acquire_admission();
            self.reads_closed.store(false, Ordering::Release);
            self.unloading.store(false, Ordering::Release);
            // Publish the usable rundown only after all admission flags describe
            // the new instance. enter() observes this flag with Acquire.
            self.lifetime_initialized.store(true, Ordering::Release);
        }
    }

    /// Acquires code-lifetime rundown before inspecting admission state. The
    /// caller keeps this guard through either Device work or the rejected-IRP
    /// completion path.
    fn enter(&self, is_read: bool) -> Option<DispatchGuard<'_>> {
        let lifetime = self.acquire_lifetime()?;
        // Admission and closure use the same short lock. This makes the counter
        // a reliable snapshot for cleanup: a dispatch that observed an open
        // gate is counted before cleanup can close it.
        let _admission_guard = self.acquire_admission();
        let admitted = !self.unloading.load(Ordering::Acquire)
            && !(is_read && self.reads_closed.load(Ordering::Acquire));

        if admitted {
            self.active.fetch_add(1, Ordering::AcqRel);
            if is_read {
                self.reads.fetch_add(1, Ordering::AcqRel);
            } else {
                self.non_read.fetch_add(1, Ordering::AcqRel);
            }
        }

        Some(DispatchGuard {
            gate: self,
            is_read,
            admitted,
            _lifetime: lifetime,
        })
    }

    fn acquire_lifetime(&self) -> Option<DispatchLifetimeGuard<'_>> {
        if !self.lifetime_initialized.load(Ordering::Acquire) {
            return None;
        }
        let acquired = unsafe { ExAcquireRundownProtection(self.dispatch_lifetime.get()) } != 0;
        acquired.then_some(DispatchLifetimeGuard {
            rundown: self.dispatch_lifetime.get(),
            _gate: PhantomData,
        })
    }

    /// Acquires the short admission lock used to serialize dispatch entry with
    /// cleanup and unload closure. The lock is never held during Device work.
    fn acquire_admission(&self) -> wdk::rw_spin_lock::RwLockWriteGuard<'_, ()> {
        self.admission_lock.write_lock()
    }

    /// Closes admission for all new dispatches.
    fn close(&self) {
        let _admission_guard = self.acquire_admission();
        self.reads_closed.store(true, Ordering::Release);
        self.unloading.store(true, Ordering::Release);
    }

    /// Waits until no ordinary dispatch can mutate Device.
    ///
    /// Reads are deliberately excluded here: a read may be blocked in the
    /// cancellable queue wait and must be released before the final all-dispatch
    /// drain below.
    fn wait_for_non_read(&self) {
        while self.non_read.load(Ordering::Acquire) != 0 {
            wdk::utils::sleep_ms(1);
        }
    }

    /// Returns whether the driver is closing admission for unload.
    fn is_unloading(&self) -> bool {
        let _admission_guard = self.acquire_admission();
        self.unloading.load(Ordering::Acquire)
    }

    /// Prevents a new read from racing session cancellation/reset.
    fn close_reads(&self) {
        let _admission_guard = self.acquire_admission();
        self.reads_closed.store(true, Ordering::Release);
    }

    /// Allows reads for the newly accepted file object.
    fn open_reads(&self) {
        let _admission_guard = self.acquire_admission();
        self.reads_closed.store(false, Ordering::Release);
    }

    fn wait_for_reads(&self) {
        while self.reads.load(Ordering::Acquire) != 0 {
            wdk::utils::sleep_ms(1);
        }
    }

    /// Serializes CREATE/CLEANUP session transitions. These routines run at
    /// PASSIVE_LEVEL, so sleeping is preferable to holding a spin lock while
    /// another dispatch routine drains the active readers.
    fn acquire_session(&self) -> SessionGuard<'_> {
        while self
            .session_busy
            .compare_exchange(false, true, Ordering::Acquire, Ordering::Relaxed)
            .is_err()
        {
            wdk::utils::sleep_ms(1);
        }

        SessionGuard { gate: self }
    }

    fn wait_for_all(&self) {
        while self.active.load(Ordering::Acquire) != 0 {
            wdk::utils::sleep_ms(1);
        }
    }

    /// Closes the dispatch rundown and waits for rejected callbacks that were
    /// already executing their final IRP completion path.
    fn wait_for_lifetime(&self) {
        if !self.lifetime_initialized.load(Ordering::Acquire) {
            return;
        }
        unsafe {
            ExWaitForRundownProtectionRelease(self.dispatch_lifetime.get());
            ExRundownCompleted(self.dispatch_lifetime.get());
        }
    }
}

struct DispatchLifetimeGuard<'a> {
    rundown: *mut EX_RUNDOWN_REF,
    _gate: PhantomData<&'a DispatchGate>,
}

impl Drop for DispatchLifetimeGuard<'_> {
    fn drop(&mut self) {
        unsafe {
            ExReleaseRundownProtection(self.rundown);
        }
    }
}

struct DispatchGuard<'a> {
    gate: &'a DispatchGate,
    is_read: bool,
    admitted: bool,
    _lifetime: DispatchLifetimeGuard<'a>,
}

impl DispatchGuard<'_> {
    fn is_admitted(&self) -> bool {
        self.admitted
    }
}

impl Drop for DispatchGuard<'_> {
    fn drop(&mut self) {
        if !self.admitted {
            return;
        }
        if self.is_read {
            self.gate.reads.fetch_sub(1, Ordering::AcqRel);
        } else {
            self.gate.non_read.fetch_sub(1, Ordering::AcqRel);
        }
        self.gate.active.fetch_sub(1, Ordering::AcqRel);
    }
}

struct SessionGuard<'a> {
    gate: &'a DispatchGate,
}

impl Drop for SessionGuard<'_> {
    fn drop(&mut self) {
        self.gate.session_busy.store(false, Ordering::Release);
    }
}

static DISPATCH_GATE: DispatchGate = DispatchGate::new();

/// Closes read admission, wakes blocked readers, and waits until every admitted
/// read has returned. Queue rundown must not race an active `KeRemoveQueue`.
///
/// This is also used by the user-issued shutdown command, which can run while a
/// separate ReadFile is blocked; DriverUnload performs the same steps before it
/// starts reclaiming Device state.
pub(crate) fn close_and_wait_for_reads(device: &device::Device) {
    DISPATCH_GATE.close_reads();
    device.cancel_read_waiters();
    DISPATCH_GATE.wait_for_reads();
}

/// Loads the published Device pointer.
///
/// This is only a publication/load operation; it does not keep the allocation
/// alive. WFP classify callers must hold the callback barrier guard acquired by
/// the common trampoline, and user-mode dispatch callers must hold a
/// DispatchGate guard, for the entire duration of their Device access. Unload
/// closes both admission paths and waits for their guards before swapping and
/// dropping the pointer.
pub fn get_device() -> Option<&'static device::Device> {
    // Acquire pairs with the Release store in driver_entry and the AcqRel swap in driver_unload.
    unsafe { DEVICE.load(Ordering::Acquire).as_ref() }
}

// DriverEntry is the entry point of the driver (main function). Will be called when driver is loaded.
// Name should not be changed
///
/// # Safety
///
/// `driver_object` and `registry_path` must be the live kernel objects supplied
/// by the I/O manager for this DriverEntry invocation and remain valid for the
/// duration required by the WDM initialization contract.
#[export_name = "DriverEntry"]
pub unsafe extern "system" fn driver_entry(
    driver_object: *mut windows_sys::Wdk::Foundation::DRIVER_OBJECT,
    registry_path: *mut windows_sys::Win32::Foundation::UNICODE_STRING,
) -> windows_sys::Win32::Foundation::NTSTATUS {
    info!("Starting initialization...");
    if !wdk::callback_barrier::CALLBACK_BARRIER.prepare() {
        err!("driver_entry: callback barrier is still in use");
        return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
    }
    DISPATCH_GATE.reopen();

    // Initialize driver object.
    // SAFETY: The I/O manager invokes DriverEntry at PASSIVE_LEVEL and supplies
    // both raw arguments for this call. The registered callbacks use the exact
    // WDF ABI and remain in the loaded driver image until teardown completes.
    let driver = match unsafe {
        interface::init_driver_object(
            driver_object,
            registry_path,
            "PortmasterKext",
            driver_unload,
            driver_create,
            driver_cleanup,
            driver_close,
            driver_read,
            driver_write,
            device_control,
        )
    } {
        Ok(driver) => driver,
        Err(status) => {
            // No WFP callbacks should remain admitted if initialization aborts
            // before the DriverUnload pointer is installed.
            DISPATCH_GATE.close();
            DISPATCH_GATE.wait_for_all();
            DISPATCH_GATE.wait_for_lifetime();
            wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
            err!("driver_entry: failed to initialize driver: {}", status);
            return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
        }
    };

    // Initialize device.
    let device = match device::Device::new(&driver) {
        Ok(device) => Box::new(device),
        Err(err) => {
            // Device::new may have registered WFP callbacks before a later
            // initialization step failed.  Close admission even on this error
            // path so no callback can outlive the temporary FilterEngine state.
            DISPATCH_GATE.close();
            DISPATCH_GATE.wait_for_all();
            DISPATCH_GATE.wait_for_lifetime();
            wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
            wdk::err!("filed to initialize device: {}", err);
            return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
        }
    };
    // Publish the complete allocation before committing WFP filters. A successful
    // FWPM transaction can invoke classify immediately, and every callout resolves
    // its state through this pointer.
    let device = Box::into_raw(device);
    DEVICE.store(device, Ordering::Release);

    if let Err(err) = unsafe { &*device }.start_filtering() {
        // commit() rolls back all runtime/FWPM registrations before returning.
        // Drain callbacks that executed the PREPARED bypass path before removing
        // the publication and allowing DriverEntry to fail.
        DISPATCH_GATE.close();
        wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
        DISPATCH_GATE.wait_for_all();
        DISPATCH_GATE.wait_for_lifetime();
        let published = DEVICE.swap(core::ptr::null_mut(), Ordering::AcqRel);
        if !published.is_null() {
            unsafe {
                drop(Box::from_raw(published));
            }
        }
        wdk::err!("failed to activate WFP filters: {}", err);
        return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
    }

    if !wdk::callback_barrier::CALLBACK_BARRIER.activate() {
        // No callback was granted Device access, but committed WFP state and any
        // PREPARED bypass callback must still be synchronously drained.
        DISPATCH_GATE.close();
        unsafe { &*device }.begin_unload();
        unsafe { &*device }.shutdown();
        unsafe { &*device }.prepare_unload();
        wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
        DISPATCH_GATE.wait_for_all();
        DISPATCH_GATE.wait_for_lifetime();
        let published = DEVICE.swap(core::ptr::null_mut(), Ordering::AcqRel);
        if !published.is_null() {
            unsafe {
                drop(Box::from_raw(published));
            }
        }
        wdk::err!("driver_entry: failed to activate callback barrier");
        return windows_sys::Win32::Foundation::STATUS_FAILED_DRIVER_ENTRY;
    }

    // I/O has remained disabled since WdfDeviceCreate. Enable it only after the
    // singleton, all WFP state, and every callback dependency are visible.
    driver.finish_initialization();

    STATUS_SUCCESS
}

// driver_unload function is called when service delete is called from user-space.
unsafe extern "system" fn driver_unload(_driver: windows_sys::Win32::Foundation::HANDLE) {
    info!("Unloading driver");

    // Close user-mode dispatch and WFP classify admission before touching
    // Device.  The classify barrier is closed first, but flow-delete admission
    // remains open while prepare_unload asks WFP to remove the contexts that
    // invoke flowDeleteFn.
    DISPATCH_GATE.close();
    wdk::callback_barrier::CALLBACK_BARRIER.close_classify_and_wait();

    // Signal the queue before waiting for non-read dispatches. CLEANUP itself
    // can be one of those dispatches and may be waiting for an admitted read;
    // delaying cancellation until after that wait would deadlock unload.
    if let Some(device) = get_device() {
        device.cancel_read_waiters();
    }
    DISPATCH_GATE.wait_for_non_read();
    // The cancellation flag is already set. Drain admitted reads before any
    // Device teardown so no user dispatch can touch the object while its WFP
    // state or queue is being reclaimed.
    DISPATCH_GATE.wait_for_reads();

    if let Some(device) = get_device() {
        // No classify callback can create another pend or flow context now.
        // Mark flow-context shutdown, then resolve every driver-owned ALE pend
        // while the runtime callouts are still registered: completion triggers
        // reauthorization, which must safely pass through the closed classify
        // barrier rather than targeting an unregistered callback.
        device.begin_unload();
        device.shutdown();

        // Drain flow contexts and unregister every runtime callout while the
        // event queue and the rest of Device are still usable. Flow-delete
        // callbacks are the only WFP callbacks still admitted in this phase and
        // are counted by the second half of the common callback barrier.
        device.prepare_unload();

        // No flow context or callback may remain before any Device field is
        // reclaimed. This also closes the defensive late-callback path before
        // FilterEngine/Injector teardown starts.
        wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
        DISPATCH_GATE.wait_for_all();
    } else {
        wdk::callback_barrier::CALLBACK_BARRIER.close_all_and_wait();
        DISPATCH_GATE.wait_for_all();
    }

    // The lifetime rundown also covers callbacks that observed a closed gate and
    // only complete their IRP. Do this before the Device pointer is swapped, so
    // no preprocess callback can still execute while its backing image/state is
    // being reclaimed.
    DISPATCH_GATE.wait_for_lifetime();

    // Null the global pointer only after every user dispatch and WFP callback
    // has drained. prepare_unload already removed all runtime callouts and
    // closed their dynamic FWPM session; FilterEngine::drop is an idempotent
    // last-resort cleanup path.
    let ptr = DEVICE.swap(core::ptr::null_mut(), Ordering::AcqRel);
    if !ptr.is_null() {
        unsafe {
            drop(Box::from_raw(ptr));
        }
    }
}

/// driver_create is triggered when user-space opens a handle to the device (CreateFile).
unsafe extern "system" fn driver_create(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    // Acquire code-lifetime protection before inspecting the I/O-manager-owned
    // IRP. WDF supplies a non-null IRP, but keeping the defensive null check
    // after admission also covers the entire rejection path during teardown.
    let dispatch_guard = DISPATCH_GATE.enter(false);
    // SAFETY: KMDF invokes this preprocess callback with the live IRP_MJ_CREATE
    // request whose ownership remains with this dispatch routine until completion.
    let Some(mut create_request) = (unsafe { CreateRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    let Some(dispatch_guard) = dispatch_guard else {
        return create_request.fail(STATUS_DEVICE_NOT_READY);
    };
    if !dispatch_guard.is_admitted() {
        return create_request.fail(STATUS_DEVICE_NOT_READY);
    }
    if !wdk::utils::is_passive_level() {
        return create_request.fail(STATUS_INVALID_DEVICE_STATE);
    }
    let Some(device) = get_device() else {
        return create_request.fail(STATUS_DEVICE_NOT_READY);
    };
    let _session_guard = DISPATCH_GATE.acquire_session();

    if DISPATCH_GATE.is_unloading() {
        return create_request.fail(STATUS_DEVICE_NOT_READY);
    }

    let pid = create_request.get_requestor_pid();
    let file_object = create_request.get_file_object();
    if file_object.is_null()
        || device
            .owner_file_object
            .compare_exchange(
                core::ptr::null_mut(),
                file_object,
                Ordering::AcqRel,
                Ordering::Acquire,
            )
            .is_err()
    {
        crate::warn!("Rejecting additional device open from PID {}", pid);
        return create_request.fail(STATUS_SHARING_VIOLATION);
    }

    device.owner_pid.store(pid, Ordering::Release);
    // A previous owner may have canceled the shared read wait. Reset it only
    // after the new file object owns the session, then reopen read admission.
    device.reset_read_cancellation();
    DISPATCH_GATE.open_reads();
    info!("Device opened by PID {}", pid);
    create_request.complete()
}

/// driver_cleanup is triggered when user-space closes the last handle to the device.
unsafe extern "system" fn driver_cleanup(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    let dispatch_guard = DISPATCH_GATE.enter(false);
    // SAFETY: KMDF invokes this preprocess callback with the live IRP_MJ_CLEANUP
    // request whose ownership remains with this dispatch routine until completion.
    let Some(mut cleanup_request) = (unsafe { CleanupRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    let Some(dispatch_guard) = dispatch_guard else {
        return cleanup_request.complete();
    };
    if !dispatch_guard.is_admitted() {
        return cleanup_request.complete();
    }
    if !wdk::utils::is_passive_level() {
        return cleanup_request.fail(STATUS_INVALID_DEVICE_STATE);
    }
    let Some(device) = get_device() else {
        return cleanup_request.complete();
    };

    {
        let _session_guard = DISPATCH_GATE.acquire_session();
        let file_object = cleanup_request.get_file_object();
        if !file_object.is_null() && device.owner_file_object.load(Ordering::Acquire) == file_object
        {
            // Stop new reads before signaling the wait. Reads that passed the
            // admission check are counted by DispatchGate and are drained below.
            close_and_wait_for_reads(device);
            // The session gate keeps CREATE from reopening read admission between
            // the zero-reader observation and this reset. A replacement handle
            // must begin at a complete record boundary.
            if let Err(err) = device.clear_read_leftover() {
                err!("failed to clear read stream during cleanup: {}", err);
            }

            // Keep the owner pointer published until every old read has left;
            // this prevents a replacement CREATE from resetting the event too
            // early. The session gate serializes this transition with CREATE.
            if device
                .owner_file_object
                .compare_exchange(
                    file_object,
                    core::ptr::null_mut(),
                    Ordering::AcqRel,
                    Ordering::Acquire,
                )
                .is_ok()
            {
                let old_pid = device.owner_pid.swap(0, Ordering::AcqRel);
                info!("Device closed by PID {}", old_pid);
            }
        }
    }

    cleanup_request.complete()
}

/// IRP_MJ_CLOSE is a separate lifetime phase from CLEANUP. Complete it through
/// the driver's own dispatch table so a late file-object deletion never falls
/// back into KMDF after the WDFDEVICE has begun teardown.
unsafe extern "system" fn driver_close(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    let dispatch_guard = DISPATCH_GATE.enter(false);
    // SAFETY: KMDF invokes this preprocess callback with the live IRP_MJ_CLOSE
    // request whose ownership remains with this dispatch routine until completion.
    let Some(mut close_request) = (unsafe { CloseRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    if dispatch_guard
        .as_ref()
        .is_some_and(|guard| !guard.is_admitted())
    {
        return close_request.complete();
    }
    close_request.complete()
}

// driver_read event triggered from user-space on file.Read.
unsafe extern "system" fn driver_read(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    let dispatch_guard = DISPATCH_GATE.enter(true);
    // SAFETY: KMDF invokes this preprocess callback with a live buffered
    // IRP_MJ_READ request that remains owned here until completion.
    let Some(mut read_request) = (unsafe { ReadRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    let Some(dispatch_guard) = dispatch_guard else {
        return read_request.end_of_file();
    };
    if !dispatch_guard.is_admitted() {
        return read_request.end_of_file();
    }
    if !wdk::utils::is_passive_level() {
        return read_request.fail(STATUS_INVALID_DEVICE_STATE);
    }
    let Some(device) = get_device() else {
        return read_request.complete();
    };

    device.read(&mut read_request)
}

/// driver_write event triggered from user-space on file.Write.
unsafe extern "system" fn driver_write(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    let dispatch_guard = DISPATCH_GATE.enter(false);
    // SAFETY: KMDF invokes this preprocess callback with a live buffered
    // IRP_MJ_WRITE request that remains owned here until completion.
    let Some(mut write_request) = (unsafe { WriteRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    let Some(dispatch_guard) = dispatch_guard else {
        return write_request.complete();
    };
    if !dispatch_guard.is_admitted() {
        return write_request.complete();
    }
    if !wdk::utils::is_passive_level() {
        return write_request.fail(STATUS_INVALID_DEVICE_STATE);
    }
    let Some(device) = get_device() else {
        return write_request.complete();
    };

    match device.write(&write_request) {
        Ok(()) => {
            // Report the complete input as consumed only when every command in the
            // buffer has been parsed and applied successfully.
            write_request.mark_all_as_read();
            write_request.complete()
        }
        Err(error) => {
            // Commands before the failing record have already taken effect. Preserve
            // their exact byte count in IoStatus.Information for the caller.
            write_request.fail_with_bytes_consumed(error.status, error.bytes_processed)
        }
    }
}

/// device_control event triggered from user-space on file.deviceIOControl.
unsafe extern "system" fn device_control(
    _wdf_device: windows_sys::Win32::Foundation::HANDLE,
    irp: *mut IRP,
) -> NTSTATUS {
    let dispatch_guard = DISPATCH_GATE.enter(false);
    // SAFETY: KMDF invokes this preprocess callback with a live METHOD_BUFFERED
    // IRP_MJ_DEVICE_CONTROL request that remains owned here until completion.
    let Some(mut control_request) = (unsafe { DeviceControlRequest::new(irp) }) else {
        return STATUS_INVALID_PARAMETER;
    };
    let Some(dispatch_guard) = dispatch_guard else {
        return control_request.not_implemented();
    };
    if !dispatch_guard.is_admitted() {
        return control_request.not_implemented();
    }
    if !wdk::utils::is_passive_level() {
        return control_request.fail(STATUS_INVALID_DEVICE_STATE);
    }
    let Some(device) = get_device() else {
        return control_request.complete();
    };

    let Some(control_code): Option<ControlCode> =
        FromPrimitive::from_u32(control_request.get_control_code())
    else {
        wdk::info!("Unknown IOCTL code: {}", control_request.get_control_code());
        return control_request.not_implemented();
    };

    wdk::info!("IOCTL: {}", control_code);

    match control_code {
        ControlCode::Version => {
            if !control_request.write_exact(&VERSION) {
                return control_request.fail(STATUS_BUFFER_TOO_SMALL);
            }
        }
        ControlCode::ShutdownRequest => device.shutdown(),
    };

    control_request.complete()
}
