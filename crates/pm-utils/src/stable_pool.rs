//! [`StablePool`] ported from Go `base/utils/stablepool.go`.
//!
//! A predictable alternative to `sync.Pool`: items are never dropped
//! automatically and are handed out FIFO so a fixed set of values is used
//! evenly. Unlike `sync.Pool`, `Get` always returns a stored item if one is
//! available, otherwise it calls `new`.

use std::sync::{Mutex, MutexGuard};

struct PoolState<T> {
    pool: Vec<Option<T>>,
    count: usize,
    get_index: usize,
    put_index: usize,
}

/// A thread-safe, FIFO, non-shrinking pool of temporary objects.
pub struct StablePool<T> {
    state: Mutex<PoolState<T>>,
    new: Option<Box<dyn Fn() -> T + Send + Sync>>,
}

impl<T> Default for StablePool<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> StablePool<T> {
    /// Creates an empty pool without a factory.
    pub fn new() -> StablePool<T> {
        StablePool {
            state: Mutex::new(PoolState {
                pool: Vec::new(),
                count: 0,
                get_index: 0,
                put_index: 0,
            }),
            new: None,
        }
    }

    /// Creates a pool whose `Get` calls `new` when no item is available.
    pub fn with_new<F>(new: F) -> StablePool<T>
    where
        F: Fn() -> T + Send + Sync + 'static,
    {
        StablePool {
            new: Some(Box::new(new)),
            ..Self::new()
        }
    }

    fn lock_state(&self) -> MutexGuard<'_, PoolState<T>> {
        self.state
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
    }

    /// Adds `x` to the pool.
    pub fn put(&self, x: T) {
        let mut state = self.lock_state();

        // Grow the backing storage when it is full or uninitialised.
        if state.count == state.pool.len() {
            state.pool.push(Some(x));
            state.count += 1;
            state.put_index = state.count;
            return;
        }

        let len = state.pool.len();
        let start = state.put_index % len;
        // Walk at most one full ring, starting at `put_index`. This is Go's
        // `for i := putIndex; i != putIndex-1; i = (i+1) % len` loop expressed
        // with unsigned indices (Go's `stopAt` can be -1).
        for step in 0..len {
            let index = (start + step) % len;
            if state.pool[index].is_none() {
                state.pool[index] = Some(x);
                state.count += 1;
                state.put_index = index + 1;
                return;
            }
        }
    }

    /// Returns the next available item, removing it from the pool.
    ///
    /// If the pool is empty, the factory passed to [`StablePool::with_new`] is
    /// called; otherwise `None` is returned.
    pub fn get(&self) -> Option<T> {
        let mut state = self.lock_state();

        if state.count == 0 {
            return self.new.as_ref().map(|factory| factory());
        }

        let len = state.pool.len();
        let start = state.get_index % len;
        for step in 0..len {
            let index = (start + step) % len;
            if let Some(item) = state.pool[index].take() {
                state.count -= 1;
                state.get_index = index + 1;
                return Some(item);
            }
        }

        self.new.as_ref().map(|factory| factory())
    }

    /// Returns the number of items currently held.
    pub fn size(&self) -> usize {
        self.lock_state().count
    }

    /// Returns the maximum number of items the pool has ever held.
    pub fn max(&self) -> usize {
        self.lock_state().pool.len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::{Arc, Barrier};

    #[test]
    fn get_is_fifo_and_pool_never_shrinks() {
        let pool = StablePool::new();
        for value in 0..100 {
            pool.put(value);
        }
        assert_eq!(pool.size(), 100);
        assert_eq!(pool.max(), 100);
        for expected in 0..100 {
            assert_eq!(pool.get(), Some(expected));
        }
        assert_eq!(pool.size(), 0);
        assert_eq!(pool.max(), 100);
    }

    #[test]
    fn put_reuses_free_slots_in_fifo_order() {
        let pool = StablePool::new();
        pool.put(1);
        pool.put(2);
        assert_eq!(pool.get(), Some(1));
        pool.put(3);
        assert_eq!(pool.size(), 2);
        assert_eq!(pool.max(), 2);
        assert_eq!(pool.get(), Some(2));
        assert_eq!(pool.get(), Some(3));
        assert_eq!(pool.get(), None);
    }

    #[test]
    fn factory_is_called_only_when_empty() {
        let created = Arc::new(AtomicUsize::new(0));
        let counter = Arc::clone(&created);
        let pool = StablePool::with_new(move || {
            counter.fetch_add(1, Ordering::SeqCst);
            42
        });
        assert_eq!(pool.get(), Some(42));
        assert_eq!(created.load(Ordering::SeqCst), 1);
        pool.put(7);
        assert_eq!(pool.get(), Some(7));
        assert_eq!(created.load(Ordering::SeqCst), 1);
        assert_eq!(pool.get(), Some(42));
        assert_eq!(created.load(Ordering::SeqCst), 2);
    }

    #[test]
    fn real_world_simulation_stays_bounded() {
        let created = Arc::new(AtomicUsize::new(0));
        let counter = Arc::clone(&created);
        let pool = Arc::new(StablePool::with_new(move || {
            counter.fetch_add(1, Ordering::SeqCst)
        }));

        for _ in 0..20 {
            let barrier = Arc::new(Barrier::new(21));
            let mut handles = Vec::new();
            for worker in 0..20 {
                let pool = Arc::clone(&pool);
                let barrier = Arc::clone(&barrier);
                handles.push(std::thread::spawn(move || {
                    barrier.wait();
                    let value = pool.get().unwrap();
                    if worker % 10 > 0 {
                        pool.put(value);
                    }
                }));
            }
            barrier.wait();
            for handle in handles {
                handle.join().unwrap();
            }
        }
        assert!(pool.max() <= 20, "pool grew beyond the worker count");
        assert!(created.load(Ordering::SeqCst) <= 200);
    }

    #[test]
    fn concurrent_put_get_never_loses_items() {
        let pool = Arc::new(StablePool::new());
        let barrier = Arc::new(Barrier::new(9));
        let mut handles = Vec::new();
        for _ in 0..8 {
            let pool = Arc::clone(&pool);
            let barrier = Arc::clone(&barrier);
            handles.push(std::thread::spawn(move || {
                barrier.wait();
                for value in 0..1000 {
                    pool.put(value);
                }
            }));
        }
        barrier.wait();
        for handle in handles {
            handle.join().unwrap();
        }
        assert_eq!(pool.size(), 8000);
        let mut received = 0;
        while pool.get().is_some() {
            received += 1;
        }
        assert_eq!(received, 8000);
    }
}
