//! Call limiters ported from Go `base/utils/call_limiter.go` and
//! `call_limiter2.go`.
//!
//! Both types bundle concurrent calls into a single execution and optionally
//! enforce a minimum pause *between* executions. They differ only in their
//! internal synchronisation strategy; the observable behaviour is the same.
//! Panics in the executed closure are propagated, but the limiter is always
//! released (Go uses `defer`, Rust uses RAII guards).

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Condvar, Mutex, MutexGuard, RwLock};
use std::time::{Duration, Instant};

fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Bundles concurrent calls and optionally limits how fast a function runs.
///
/// Set `min_pause` to [`Duration::ZERO`] to disable the pause.
pub struct CallLimiter {
    pause: Duration,
    state: Mutex<LimiterState>,
    done: Condvar,
}

struct LimiterState {
    executing: bool,
    generation: u64,
    last_exec: Option<Instant>,
}

impl CallLimiter {
    /// Creates a new call limiter.
    pub fn new(min_pause: Duration) -> CallLimiter {
        CallLimiter {
            pause: min_pause,
            state: Mutex::new(LimiterState {
                executing: false,
                generation: 0,
                last_exec: None,
            }),
            done: Condvar::new(),
        }
    }

    /// Executes `f`, bundling all concurrent callers into one execution.
    pub fn do_it<F: FnOnce()>(&self, f: F) {
        let mut state = lock(&self.state);
        if !state.executing {
            state.executing = true;
            drop(state);
            self.lead(f);
            return;
        }

        // This call arrived while another execution is in flight. Wait for
        // that generation to finish, then return without executing `f`.
        let generation = state.generation;
        while state.generation == generation {
            state = self
                .done
                .wait(state)
                .unwrap_or_else(|poisoned| poisoned.into_inner());
        }
    }

    fn lead<F: FnOnce()>(&self, f: F) {
        struct Completion<'a> {
            limiter: &'a CallLimiter,
        }
        impl Drop for Completion<'_> {
            fn drop(&mut self) {
                let mut state = lock(&self.limiter.state);
                state.last_exec = Some(Instant::now());
                state.executing = false;
                state.generation = state.generation.wrapping_add(1);
                drop(state);
                self.limiter.done.notify_all();
            }
        }
        let _completion = Completion { limiter: self };

        if !self.pause.is_zero() {
            let wait = {
                let state = lock(&self.state);
                state.last_exec.map_or(Duration::ZERO, |last| {
                    let elapsed = last.elapsed();
                    if elapsed < self.pause {
                        self.pause - elapsed
                    } else {
                        Duration::ZERO
                    }
                })
            };
            if !wait.is_zero() {
                std::thread::sleep(wait);
            }
        }

        f();
    }
}

impl Default for CallLimiter {
    fn default() -> Self {
        Self::new(Duration::ZERO)
    }
}

/// Second generation of [`CallLimiter`], ported from `call_limiter2.go`.
///
/// Uses a ticket/slot scheme instead of entry and exit locks. A caller that
/// sees an execution in progress simply waits for the slot to advance.
pub struct CallLimiter2 {
    pause: Duration,
    slot: AtomicU64,
    slot_wait: RwLock<()>,
    executing: AtomicBool,
    last_exec: Mutex<Option<Instant>>,
}

impl CallLimiter2 {
    /// Creates a new call limiter.
    pub fn new(min_pause: Duration) -> CallLimiter2 {
        CallLimiter2 {
            pause: min_pause,
            slot: AtomicU64::new(0),
            slot_wait: RwLock::new(()),
            executing: AtomicBool::new(false),
            last_exec: Mutex::new(None),
        }
    }

    /// Executes `f`, bundling all concurrent callers into one execution.
    pub fn do_it<F: FnOnce()>(&self, f: F) {
        let slot = self.slot.load(Ordering::SeqCst);

        if self
            .executing
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .is_ok()
        {
            let guard = self
                .slot_wait
                .write()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            self.wait_and_exec(f);
            drop(guard);
            return;
        }

        // Wait for the current slot to finish. The read lock blocks until the
        // executing writer leaves, and the slot check avoids spinning forever.
        while self.slot.load(Ordering::SeqCst) == slot {
            std::thread::sleep(Duration::from_micros(100));
            let read_guard = self
                .slot_wait
                .read()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            drop(read_guard);
        }
    }

    fn wait_and_exec<F: FnOnce()>(&self, f: F) {
        struct Completion<'a> {
            limiter: &'a CallLimiter2,
        }
        impl Drop for Completion<'_> {
            fn drop(&mut self) {
                *lock(&self.limiter.last_exec) = Some(Instant::now());
                // Enable the next execution first, then move to the next slot
                // so no waiter can loop forever.
                self.limiter.executing.store(false, Ordering::SeqCst);
                self.limiter.slot.fetch_add(1, Ordering::SeqCst);
            }
        }
        let _completion = Completion { limiter: self };

        if !self.pause.is_zero() {
            let wait = {
                let last_exec = lock(&self.last_exec);
                last_exec.map_or(Duration::ZERO, |last| {
                    let elapsed = last.elapsed();
                    if elapsed < self.pause {
                        self.pause - elapsed
                    } else {
                        Duration::ZERO
                    }
                })
            };
            if !wait.is_zero() {
                std::thread::sleep(wait);
            }
        }

        f();
    }
}

impl Default for CallLimiter2 {
    fn default() -> Self {
        Self::new(Duration::ZERO)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicUsize;
    use std::sync::{Arc, Barrier};

    trait RunOnce: Send + Sync {
        fn run(&self, action: Box<dyn Fn() + Send + Sync>);
    }
    impl RunOnce for CallLimiter {
        fn run(&self, action: Box<dyn Fn() + Send + Sync>) {
            self.do_it(action);
        }
    }
    impl RunOnce for CallLimiter2 {
        fn run(&self, action: Box<dyn Fn() + Send + Sync>) {
            self.do_it(action);
        }
    }

    fn bundled_once(limiter: Arc<dyn RunOnce>) -> usize {
        let calls = Arc::new(AtomicUsize::new(0));
        let release = Arc::new(AtomicBool::new(false));
        let barrier = Arc::new(Barrier::new(17));
        let mut handles = Vec::new();
        for _ in 0..16 {
            let calls = Arc::clone(&calls);
            let release = Arc::clone(&release);
            let barrier = Arc::clone(&barrier);
            let limiter = Arc::clone(&limiter);
            let action: Box<dyn Fn() + Send + Sync> = Box::new(move || {
                calls.fetch_add(1, Ordering::SeqCst);
                // Keep the execution in flight until every caller had time to
                // enter `do_it`, so all of them are bundled into this one run.
                while !release.load(Ordering::SeqCst) {
                    std::thread::yield_now();
                }
            });
            handles.push(std::thread::spawn(move || {
                barrier.wait();
                limiter.run(action);
            }));
        }
        barrier.wait();
        std::thread::sleep(Duration::from_millis(20));
        release.store(true, Ordering::SeqCst);
        for handle in handles {
            handle.join().unwrap();
        }
        calls.load(Ordering::SeqCst)
    }

    #[test]
    fn call_limiter_bundles_concurrent_calls() {
        let count = bundled_once(Arc::new(CallLimiter::default()));
        assert_eq!(count, 1, "exactly one bundled execution expected");
    }

    #[test]
    fn call_limiter2_bundles_concurrent_calls() {
        let count = bundled_once(Arc::new(CallLimiter2::default()));
        assert_eq!(count, 1, "exactly one bundled execution expected");
    }

    #[test]
    fn call_limiter_enforces_minimum_pause() {
        let pause = Duration::from_millis(30);
        let limiter = CallLimiter::new(pause);
        limiter.do_it(|| {});
        let start = Instant::now();
        limiter.do_it(|| {});
        assert!(
            start.elapsed() >= pause,
            "second call returned before the pause elapsed"
        );
    }

    #[test]
    fn call_limiter2_enforces_minimum_pause() {
        let pause = Duration::from_millis(30);
        let limiter = CallLimiter2::new(pause);
        limiter.do_it(|| {});
        let start = Instant::now();
        limiter.do_it(|| {});
        assert!(
            start.elapsed() >= pause,
            "second call returned before the pause elapsed"
        );
    }

    #[test]
    fn call_limiter_releases_after_panic() {
        let limiter = CallLimiter::default();
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            limiter.do_it(|| panic!("test panic"));
        }));
        assert!(result.is_err());
        // A second call must not deadlock after the first one panicked.
        let called = AtomicBool::new(false);
        limiter.do_it(|| called.store(true, Ordering::SeqCst));
        assert!(called.load(Ordering::SeqCst));
    }

    #[test]
    fn call_limiter2_releases_after_panic() {
        let limiter = CallLimiter2::default();
        let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            limiter.do_it(|| panic!("test panic"));
        }));
        assert!(result.is_err());
        let called = AtomicBool::new(false);
        limiter.do_it(|| called.store(true, Ordering::SeqCst));
        assert!(called.load(Ordering::SeqCst));
    }

    #[test]
    fn call_limiter2_runs_new_wave_after_slot_advances() {
        let limiter = CallLimiter2::default();
        let calls = Arc::new(AtomicUsize::new(0));
        for _ in 0..4 {
            let counter = Arc::clone(&calls);
            limiter.do_it(move || {
                counter.fetch_add(1, Ordering::SeqCst);
            });
        }
        assert_eq!(calls.load(Ordering::SeqCst), 4);
    }

    #[test]
    fn zero_pause_executes_immediately() {
        let limiter = CallLimiter::new(Duration::ZERO);
        let start = Instant::now();
        limiter.do_it(|| {});
        assert!(start.elapsed() < Duration::from_millis(100));
    }
}
