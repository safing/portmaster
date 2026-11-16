//! [`BroadcastFlag`] ported from Go `base/utils/broadcastflag.go`.
//!
//! Go broadcasts a change by closing a channel; each listener keeps a snapshot
//! of the flag value and that channel, refreshed from the broadcaster on
//! demand. Here the channel is modelled by [`Signal`], a close-once condition
//! variable that is safe to wait on from any number of threads.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Condvar, Mutex, MutexGuard, RwLock};

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// A close-once notification primitive, equivalent to Go's closed channel.
pub struct Signal {
    inner: Arc<SignalInner>,
}

struct SignalInner {
    closed: Mutex<bool>,
    changed: Condvar,
}

impl Signal {
    /// Creates a signal that has not been triggered yet.
    pub fn new() -> Signal {
        Signal::with_state(false)
    }

    fn new_closed() -> Signal {
        Signal::with_state(true)
    }

    fn with_state(closed: bool) -> Signal {
        Signal {
            inner: Arc::new(SignalInner {
                closed: Mutex::new(closed),
                changed: Condvar::new(),
            }),
        }
    }

    /// Triggers the signal. Repeated calls are harmless.
    pub fn notify(&self) {
        let mut closed = lock(&self.inner.closed);
        *closed = true;
        drop(closed);
        self.inner.changed.notify_all();
    }

    /// Returns whether the signal has been triggered.
    pub fn is_triggered(&self) -> bool {
        *lock(&self.inner.closed)
    }

    /// Blocks until the signal is triggered, returning immediately afterwards.
    pub fn wait(&self) {
        let mut closed = lock(&self.inner.closed);
        while !*closed {
            closed = self
                .inner
                .changed
                .wait(closed)
                .unwrap_or_else(|poisoned| poisoned.into_inner());
        }
    }
}

impl Clone for Signal {
    fn clone(&self) -> Self {
        Signal {
            inner: Arc::clone(&self.inner),
        }
    }
}

impl Default for Signal {
    fn default() -> Self {
        Self::new()
    }
}

/// A simple system to broadcast a flag value to many listeners.
pub struct BroadcastFlag {
    inner: Mutex<BroadcastState>,
}

struct BroadcastState {
    // Go stores a *abool.AtomicBool here. Existing Flags keep a copy of the
    // pointer, so setting it is visible to them even after the broadcaster
    // resets its own pointer to a fresh, unset flag.
    flag: Arc<AtomicBool>,
    signal: Signal,
}

impl Default for BroadcastFlag {
    fn default() -> Self {
        Self::new()
    }
}

impl BroadcastFlag {
    /// Creates a broadcaster whose flag is initially unset.
    pub fn new() -> BroadcastFlag {
        BroadcastFlag {
            inner: Mutex::new(BroadcastState {
                flag: Arc::new(AtomicBool::new(false)),
                signal: Signal::new(),
            }),
        }
    }

    /// Creates a listener.
    ///
    /// Like Go's `NewFlag`, the listener starts *set* with an already triggered
    /// signal; call [`Flag::refresh`] to pick up the current broadcaster state.
    pub fn new_flag(self: &Arc<Self>) -> Flag {
        Flag {
            broadcaster: Arc::clone(self),
            flag: RwLock::new(Arc::new(AtomicBool::new(true))),
            signal: RwLock::new(Signal::new_closed()),
        }
    }

    /// Notifies all listeners and resets the internal flag and signal.
    pub fn notify_and_reset(&self) {
        let mut state = lock(&self.inner);
        state.flag.store(true, Ordering::SeqCst);
        state.signal.notify();
        // Reset by replacing the shared flag; old snapshots still see `true`.
        state.flag = Arc::new(AtomicBool::new(false));
        state.signal = Signal::new();
    }
}

/// Listener snapshot of a [`BroadcastFlag`].
///
/// A `Flag` is cheap to read from a single thread, like Go's `Flag` comment
/// says; the atomic flag and the `RwLock` around the signal keep it safe to
/// refresh from another thread as well.
pub struct Flag {
    broadcaster: Arc<BroadcastFlag>,
    flag: RwLock<Arc<AtomicBool>>,
    signal: RwLock<Signal>,
}

impl Flag {
    /// Waits until the broadcaster last notified listeners.
    pub fn signal(&self) -> Signal {
        self.signal
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .clone()
    }

    /// Returns whether the flag was set since the last refresh.
    pub fn is_set(&self) -> bool {
        self.flag
            .read()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .load(Ordering::SeqCst)
    }

    /// Copies the broadcaster's current flag and signal into this listener.
    pub fn refresh(&self) {
        let state = lock(&self.broadcaster.inner);
        *self
            .flag
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = Arc::clone(&state.flag);
        *self
            .signal
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = state.signal.clone();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn new_flag_starts_set_with_a_triggered_signal() {
        let broadcaster = Arc::new(BroadcastFlag::new());
        let flag = broadcaster.new_flag();
        assert!(flag.is_set());
        flag.signal().wait(); // does not block
    }

    #[test]
    fn refresh_picks_up_unset_state() {
        let broadcaster = Arc::new(BroadcastFlag::new());
        let flag = broadcaster.new_flag();
        flag.refresh();
        assert!(!flag.is_set());
    }

    #[test]
    fn notify_and_reset_wakes_all_listeners() {
        let broadcaster = Arc::new(BroadcastFlag::new());
        let first = broadcaster.new_flag();
        let second = broadcaster.new_flag();
        first.refresh();
        second.refresh();
        assert!(!first.is_set());
        assert!(!second.is_set());

        broadcaster.notify_and_reset();

        assert!(first.is_set());
        assert!(second.is_set());
        first.signal().wait();
        second.signal().wait();

        // The broadcaster resets immediately, but existing snapshots stay set
        // until they refresh again.
        let third = broadcaster.new_flag();
        third.refresh();
        assert!(!third.is_set());
        first.refresh();
        assert!(!first.is_set());
    }

    #[test]
    fn signal_waits_until_notified() {
        let signal = Signal::new();
        assert!(!signal.is_triggered());
        let waiter = Arc::clone(&signal.inner);
        let handle = std::thread::spawn(move || {
            let mut closed = lock(&waiter.closed);
            while !*closed {
                closed = waiter
                    .changed
                    .wait(closed)
                    .unwrap_or_else(|poisoned| poisoned.into_inner());
            }
        });
        std::thread::sleep(std::time::Duration::from_millis(5));
        signal.notify();
        handle.join().unwrap();
        assert!(signal.is_triggered());
        signal.wait(); // already triggered: returns immediately
        signal.notify(); // idempotent
    }
}
