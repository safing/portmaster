//! [`OnceAgain`] ported from Go `base/utils/onceagain.go`.
//!
//! This is a fork of Go's `sync.Once`: the action runs once "in flight" and the
//! object is automatically reused after every waiter has left. As in Go, this
//! is intentionally racy under heavy use: new callers may be bundled into the
//! current run instead of triggering another one.

use std::sync::atomic::{AtomicBool, AtomicI32, Ordering};
use std::sync::Mutex;

/// Like `sync.Once`, but reusable after all waiters have returned.
pub struct OnceAgain {
    done: AtomicBool,
    waiters: AtomicI32,
    mutex: Mutex<()>,
}

impl Default for OnceAgain {
    fn default() -> Self {
        Self::new()
    }
}

impl OnceAgain {
    /// Creates an unused instance.
    pub fn new() -> OnceAgain {
        OnceAgain {
            done: AtomicBool::new(false),
            waiters: AtomicI32::new(0),
            mutex: Mutex::new(()),
        }
    }

    /// Calls `f` only for the first caller of the current round.
    ///
    /// Every caller blocks until the action has finished. A panic in `f` still
    /// releases all waiters and marks the round complete, just like Go's
    /// `defer` based implementation.
    pub fn do_it<F: FnOnce()>(&self, f: F) {
        if !self.done.load(Ordering::Acquire) {
            self.do_slow(f);
        }
    }

    fn do_slow<F: FnOnce()>(&self, f: F) {
        self.waiters.fetch_add(1, Ordering::SeqCst);

        // The last waiter resets `done`, mirroring Go's deferred waiters check.
        struct WaiterGuard<'a>(&'a OnceAgain);
        impl Drop for WaiterGuard<'_> {
            fn drop(&mut self) {
                if self.0.waiters.fetch_sub(1, Ordering::SeqCst) == 1 {
                    self.0.done.store(false, Ordering::Release);
                }
            }
        }
        let _waiter = WaiterGuard(self);

        let _guard = self
            .mutex
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        // The guard is held for the whole action, like Go's `defer m.Unlock()`.
        // `_done` is declared after it, so it is dropped first and sets
        // `done = 1` before the mutex is released; `_waiter` drops last and
        // resets `done` when the final waiter leaves.
        if !self.done.load(Ordering::SeqCst) {
            struct DoneGuard<'a>(&'a OnceAgain);
            impl Drop for DoneGuard<'_> {
                fn drop(&mut self) {
                    self.0.done.store(true, Ordering::Release);
                }
            }
            let _done = DoneGuard(self);
            f();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;
    use std::sync::{Arc, Barrier};
    use std::time::Duration;

    #[test]
    fn concurrent_callers_never_run_the_action_concurrently() {
        let once = Arc::new(OnceAgain::new());
        let calls = Arc::new(AtomicUsize::new(0));
        let inside = Arc::new(AtomicBool::new(false));
        let barrier = Arc::new(Barrier::new(9));
        let mut handles = Vec::new();

        for _ in 0..8 {
            let once = Arc::clone(&once);
            let calls = Arc::clone(&calls);
            let inside = Arc::clone(&inside);
            let barrier = Arc::clone(&barrier);
            handles.push(std::thread::spawn(move || {
                barrier.wait();
                once.do_it(|| {
                    assert!(
                        !inside.swap(true, Ordering::SeqCst),
                        "concurrent execution detected"
                    );
                    calls.fetch_add(1, Ordering::SeqCst);
                    std::thread::sleep(Duration::from_millis(1));
                    inside.store(false, Ordering::SeqCst);
                });
            }));
        }
        barrier.wait();
        for handle in handles {
            handle.join().unwrap();
        }
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn action_runs_again_after_all_waiters_left() {
        let once = OnceAgain::new();
        let calls = AtomicUsize::new(0);
        for _ in 0..5 {
            once.do_it(|| {
                calls.fetch_add(1, Ordering::SeqCst);
            });
            // Give the single waiter's guard time to reset `done`.
            std::thread::sleep(Duration::from_millis(1));
        }
        assert_eq!(calls.load(Ordering::SeqCst), 5);
    }

    #[test]
    fn panic_releases_waiters_and_reuses_the_object() {
        let once = OnceAgain::new();
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            once.do_it(|| panic!("test panic"));
        }));
        assert!(result.is_err());
        let called = AtomicBool::new(false);
        once.do_it(|| called.store(true, Ordering::SeqCst));
        assert!(called.load(Ordering::SeqCst));
    }
}
