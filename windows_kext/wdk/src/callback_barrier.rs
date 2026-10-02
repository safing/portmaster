//! Coordinates WFP callback admission with driver teardown.
//!
//! The WFP filter engine can invoke a callback concurrently with
//! `DriverUnload`. A raw `AtomicPtr<Device>` only publishes the pointer; it does
//! not keep the allocation or the driver's code alive after a callback has
//! loaded it. Use kernel rundown protection to make callback admission and
//! destruction one lifetime protocol.

use core::{
    cell::UnsafeCell,
    marker::PhantomData,
    sync::atomic::{AtomicU64, Ordering},
};

use windows_sys::Wdk::System::SystemServices::{
    ExAcquireRundownProtection, ExInitializeRundownProtection, ExReInitializeRundownProtection,
    ExReleaseRundownProtection, ExRundownCompleted, ExWaitForRundownProtectionRelease,
    EX_RUNDOWN_REF, EX_RUNDOWN_REF_0,
};

const NEVER_STARTED: u8 = 0;
const PREPARED: u8 = 1;
const OPEN: u8 = 2;
const CLASSIFY_CLOSING: u8 = 3;
const CLASSIFY_CLOSED: u8 = 4;
const DRAINING: u8 = 5;
const CLOSED: u8 = 6;
const STARTING: u8 = 7;

// Keep the lifecycle phase and its generation in one atomic word. A single
// snapshot prevents a delayed callback from observing an old phase together
// with a new generation when the static barrier is reused.
const GENERATION_MASK: u64 = u64::MAX >> 8;

const fn state_token(generation: u64, phase: u8) -> u64 {
    (generation & GENERATION_MASK) << 8 | phase as u64
}

const fn state_generation(token: u64) -> u64 {
    token >> 8
}

const fn state_phase(token: u64) -> u8 {
    (token & 0xff) as u8
}

// EX_RUNDOWN_REF is allocated directly inside CallbackBarrier and initialized
// by the kernel, so a generated-size regression would be an immediate overwrite.
#[cfg(target_pointer_width = "64")]
const _: () = {
    assert!(core::mem::size_of::<EX_RUNDOWN_REF>() == 8);
    assert!(core::mem::align_of::<EX_RUNDOWN_REF>() == 8);
};

/// A callback lifetime barrier for the singleton driver instance.
///
/// `callback_lifetime` counts every callback from its first instruction until
/// return, including callbacks that intentionally bypass Device during startup
/// or teardown. The other two references control access to Device: classify
/// admission closes first, while flow-delete admission stays open until unload
/// has removed every associated flow context.
pub struct CallbackBarrier {
    callback_lifetime: UnsafeCell<EX_RUNDOWN_REF>,
    classify: UnsafeCell<EX_RUNDOWN_REF>,
    flow_delete: UnsafeCell<EX_RUNDOWN_REF>,
    // The high bits of this word identify the driver instance; the low byte is
    // the lifecycle phase for that instance.
    state: AtomicU64,
}

// The EX_RUNDOWN_REF values are accessed only through the interlocked kernel
// rundown APIs. Callback admission is safe from any CPU at <= DISPATCH_LEVEL;
// lifecycle transitions are serialized by DriverEntry/DriverUnload.
unsafe impl Sync for CallbackBarrier {}

impl CallbackBarrier {
    const fn empty_rundown_ref() -> EX_RUNDOWN_REF {
        EX_RUNDOWN_REF {
            Anonymous: EX_RUNDOWN_REF_0 { Count: 0 },
        }
    }

    const fn new() -> Self {
        Self {
            callback_lifetime: UnsafeCell::new(Self::empty_rundown_ref()),
            classify: UnsafeCell::new(Self::empty_rundown_ref()),
            flow_delete: UnsafeCell::new(Self::empty_rundown_ref()),
            state: AtomicU64::new(state_token(0, NEVER_STARTED)),
        }
    }

    /// Initializes the rundown objects without granting Device access.
    ///
    /// Runtime callouts can execute while their FWPM transaction is being built.
    /// A callback in PREPARED state holds `callback_lifetime` but takes the
    /// bypass path, so construction rollback can wait for its code to return
    /// without exposing a partially initialized Device.
    pub fn prepare(&self) -> bool {
        loop {
            let token = self.state.load(Ordering::Acquire);
            let phase = state_phase(token);
            let generation = state_generation(token);
            match phase {
                NEVER_STARTED | CLOSED => {}
                STARTING => {
                    core::hint::spin_loop();
                    continue;
                }
                _ => return false,
            }

            if self
                .state
                .compare_exchange(
                    token,
                    state_token(generation, STARTING),
                    Ordering::AcqRel,
                    Ordering::Acquire,
                )
                .is_err()
            {
                continue;
            }

            unsafe {
                if phase == NEVER_STARTED {
                    ExInitializeRundownProtection(self.callback_lifetime.get());
                    ExInitializeRundownProtection(self.classify.get());
                    ExInitializeRundownProtection(self.flow_delete.get());
                } else {
                    // close_all_and_wait completes all three references before it
                    // publishes CLOSED, which is the prerequisite for reinitialization.
                    ExReInitializeRundownProtection(self.callback_lifetime.get());
                    ExReInitializeRundownProtection(self.classify.get());
                    ExReInitializeRundownProtection(self.flow_delete.get());
                }
            }

            // Publish a new generation only after all old rundown references have
            // been completed and reinitialized. A callback delayed across
            // unload/reload will acquire either no reference or the new reference,
            // and the generation check in enter_* rejects the latter.
            let next_generation = generation.wrapping_add(1) & GENERATION_MASK;
            self.state
                .store(state_token(next_generation, PREPARED), Ordering::Release);
            return true;
        }
    }

    /// Grants callbacks access to the fully published Device.
    pub fn activate(&self) -> bool {
        let token = self.state.load(Ordering::Acquire);
        if state_phase(token) != PREPARED {
            return false;
        }

        let generation = state_generation(token);
        self.state
            .compare_exchange(
                token,
                state_token(generation, OPEN),
                Ordering::AcqRel,
                Ordering::Acquire,
            )
            .is_ok()
    }

    /// Admits a callback that needs only the driver's code lifetime, not Device
    /// state. This covers FWPS notify callbacks and transport-injection DPCs,
    /// which may still return while teardown is draining their native owners.
    pub fn enter_callback(&self) -> Option<CallbackAdmission<'_>> {
        let initial_token = self.state.load(Ordering::Acquire);
        let initial_phase = state_phase(initial_token);
        if !matches!(
            initial_phase,
            PREPARED | OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED
        ) {
            return None;
        }
        let generation = state_generation(initial_token);
        let lifetime = self.acquire(&self.callback_lifetime)?;
        let current_token = self.state.load(Ordering::Acquire);
        if state_generation(current_token) != generation {
            return None;
        }

        matches!(
            state_phase(current_token),
            PREPARED | OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED
        )
        .then_some(CallbackAdmission {
            _lifetime: lifetime,
        })
    }


    /// Admits a classify callback and states whether it may touch Device.
    ///
    /// This must be the first operation in the WFP classify trampoline, before
    /// reading filter context or any allocation owned by Device. The state is
    /// checked both before and after taking callback-lifetime rundown: teardown
    /// can begin between those operations, and a callback that observes that
    /// transition must not acquire Device admission.
    pub fn enter_classify(&self) -> Option<ClassifyAdmission<'_>> {
        let initial_token = self.state.load(Ordering::Acquire);
        let initial_phase = state_phase(initial_token);
        if !matches!(
            initial_phase,
            PREPARED | OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED
        ) {
            // In particular, do not admit a new callback after final draining
            // starts. A callback that read the old state may still race through
            // the acquisition below; the second state check handles that finite
            // set of already-started entries.
            return None;
        }
        let generation = state_generation(initial_token);

        let lifetime = self.acquire(&self.callback_lifetime)?;
        let current_token = self.state.load(Ordering::Acquire);
        if state_generation(current_token) != generation {
            // The static barrier was reused for a newer driver instance while
            // this callback was delayed. It may have acquired the new generation's
            // rundown reference, but it must never acquire Device access from it.
            return None;
        }

        match state_phase(current_token) {
            OPEN => {
                let device_access = self.acquire(&self.classify);
                let active = device_access.is_some()
                    && state_phase(self.state.load(Ordering::Acquire)) == OPEN;
                Some(ClassifyAdmission {
                    active,
                    _lifetime: lifetime,
                    _device_access: device_access,
                })
            }
            PREPARED | CLASSIFY_CLOSING | CLASSIFY_CLOSED => Some(ClassifyAdmission {
                active: false,
                _lifetime: lifetime,
                _device_access: None,
            }),
            // A final close may have completed while this callback was being
            // scheduled. Release the lifetime guard before returning so the
            // closer can finish; no driver-owned pointer was touched.
            NEVER_STARTED | STARTING | DRAINING | CLOSED => None,
            _ => None,
        }
    }

    /// Admits a flow-delete callback and states whether it may touch Device.
    ///
    /// Flow-delete admission remains available after classify admission closes,
    /// because `prepare_unload` removes associated flow contexts through WFP and
    /// those removals invoke this callback. Final draining closes this admission
    /// as well.
    pub fn enter_flow_delete(&self) -> Option<FlowDeleteAdmission<'_>> {
        let initial_token = self.state.load(Ordering::Acquire);
        let initial_phase = state_phase(initial_token);
        if !matches!(
            initial_phase,
            PREPARED | OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED
        ) {
            return None;
        }
        let generation = state_generation(initial_token);

        let lifetime = self.acquire(&self.callback_lifetime)?;
        let current_token = self.state.load(Ordering::Acquire);
        if state_generation(current_token) != generation {
            // See the corresponding classify path. Do not let a delayed callback
            // interpret an old flow context using the new Device instance.
            return None;
        }

        match state_phase(current_token) {
            OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED => {
                let device_access = self.acquire(&self.flow_delete);
                let state = state_phase(self.state.load(Ordering::Acquire));
                let active = device_access.is_some()
                    && matches!(state, OPEN | CLASSIFY_CLOSING | CLASSIFY_CLOSED);
                Some(FlowDeleteAdmission {
                    active,
                    _lifetime: lifetime,
                    _device_access: device_access,
                })
            }
            PREPARED => Some(FlowDeleteAdmission {
                active: false,
                _lifetime: lifetime,
                _device_access: None,
            }),
            NEVER_STARTED | STARTING | DRAINING | CLOSED => None,
            _ => None,
        }
    }

    fn acquire<'a>(
        &'a self,
        rundown: &'a UnsafeCell<EX_RUNDOWN_REF>,
    ) -> Option<CallbackGuard<'a>> {
        let rundown = rundown.get();
        let acquired = unsafe { ExAcquireRundownProtection(rundown) } != 0;
        acquired.then_some(CallbackGuard {
            rundown,
            _barrier: PhantomData,
        })
    }

    /// Closes Device admission for classify callbacks and waits for every active
    /// classify operation. Callback-code lifetime admission remains open until
    /// runtime callout unregistration has succeeded.
    ///
    /// The caller must be at PASSIVE_LEVEL because rundown waits can block.
    pub fn close_classify_and_wait(&self) {
        loop {
            let token = self.state.load(Ordering::Acquire);
            let generation = state_generation(token);
            match state_phase(token) {
                PREPARED | OPEN => {
                    let closing = state_token(generation, CLASSIFY_CLOSING);
                    if self
                        .state
                        .compare_exchange(
                            token,
                            closing,
                            Ordering::AcqRel,
                            Ordering::Acquire,
                        )
                        .is_err()
                    {
                        continue;
                    }

                    unsafe {
                        ExWaitForRundownProtectionRelease(self.classify.get());
                        ExRundownCompleted(self.classify.get());
                    }
                    self.state.store(
                        state_token(generation, CLASSIFY_CLOSED),
                        Ordering::Release,
                    );
                    return;
                }
                STARTING | CLASSIFY_CLOSING | DRAINING => core::hint::spin_loop(),
                CLASSIFY_CLOSED | CLOSED | NEVER_STARTED => return,
                _ => return,
            }
        }
    }

    /// Closes every callback class and waits until no callback can execute code
    /// against the retiring driver instance. Runtime callouts must already have
    /// been unregistered before this final transition.
    ///
    /// The caller must be at PASSIVE_LEVEL.
    pub fn close_all_and_wait(&self) {
        let (previous_phase, generation) = loop {
            let token = self.state.load(Ordering::Acquire);
            let phase = state_phase(token);
            let generation = state_generation(token);
            match phase {
                PREPARED | OPEN | CLASSIFY_CLOSED => {
                    if self
                        .state
                        .compare_exchange(
                            token,
                            state_token(generation, DRAINING),
                            Ordering::AcqRel,
                            Ordering::Acquire,
                        )
                        .is_ok()
                    {
                        break (phase, generation);
                    }
                }
                STARTING | CLASSIFY_CLOSING | DRAINING => {
                    core::hint::spin_loop();
                }
                NEVER_STARTED | CLOSED => return,
                _ => return,
            }
        };

        unsafe {
            if matches!(previous_phase, PREPARED | OPEN) {
                ExWaitForRundownProtectionRelease(self.classify.get());
                ExRundownCompleted(self.classify.get());
            }

            ExWaitForRundownProtectionRelease(self.flow_delete.get());
            ExRundownCompleted(self.flow_delete.get());

            // Close code-lifetime admission last. Bypass callbacks never touch
            // Device, but their instructions must return before the image unloads.
            ExWaitForRundownProtectionRelease(self.callback_lifetime.get());
            ExRundownCompleted(self.callback_lifetime.get());
        }
        self.state
            .store(state_token(generation, CLOSED), Ordering::Release);
    }
}

pub struct CallbackAdmission<'a> {
    _lifetime: CallbackGuard<'a>,
}

pub struct ClassifyAdmission<'a> {
    active: bool,
    _lifetime: CallbackGuard<'a>,
    _device_access: Option<CallbackGuard<'a>>,
}

impl ClassifyAdmission<'_> {
    pub fn is_active(&self) -> bool {
        self.active
    }
}

pub struct FlowDeleteAdmission<'a> {
    active: bool,
    _lifetime: CallbackGuard<'a>,
    _device_access: Option<CallbackGuard<'a>>,
}

impl FlowDeleteAdmission<'_> {
    pub fn is_active(&self) -> bool {
        self.active
    }
}

/// One kernel rundown acquisition held until callback return.
struct CallbackGuard<'a> {
    rundown: *mut EX_RUNDOWN_REF,
    _barrier: PhantomData<&'a CallbackBarrier>,
}

impl Drop for CallbackGuard<'_> {
    fn drop(&mut self) {
        unsafe {
            ExReleaseRundownProtection(self.rundown);
        }
    }
}

/// The callback barrier for the one Device owned by this driver image.
pub static CALLBACK_BARRIER: CallbackBarrier = CallbackBarrier::new();
