//! High-performance, lock-reduced bounded ring buffer.
//!
//! Features:
//! - Cache-line padding (`#[repr(align(64))]`) for head and tail to eliminate false sharing between cores.
//! - Power-of-two capacity for fast bitwise masking instead of expensive modulo division.
//! - Atomic ticket-based sequencing for multi-producer and multi-consumer access without global lock contention.
//! - Zero memory allocation on push and pop.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Mutex;

#[repr(align(64))]
struct CachePaddedAtomicUsize(AtomicUsize);

impl CachePaddedAtomicUsize {
    fn new(val: usize) -> Self {
        Self(AtomicUsize::new(val))
    }
}

impl std::ops::Deref for CachePaddedAtomicUsize {
    type Target = AtomicUsize;
    #[inline(always)]
    fn deref(&self) -> &AtomicUsize {
        &self.0
    }
}

struct Slot<T> {
    sequence: AtomicUsize,
    data: Mutex<Option<T>>,
}

/// A bounded lock-reduced ring buffer for high-throughput packet and event streaming.
pub struct RingBuffer<T> {
    head: CachePaddedAtomicUsize,
    tail: CachePaddedAtomicUsize,
    slots: Box<[Slot<T>]>,
    mask: usize,
    capacity: usize,
}

impl<T> RingBuffer<T> {
    /// Creates a new `RingBuffer` with the specified capacity (rounded up to the next power of two, minimum 2).
    pub fn new(capacity: usize) -> Self {
        let cap = capacity.max(2).next_power_of_two();
        let mask = cap - 1;

        let mut slots = Vec::with_capacity(cap);
        for i in 0..cap {
            slots.push(Slot {
                sequence: AtomicUsize::new(i),
                data: Mutex::new(None),
            });
        }

        RingBuffer {
            head: CachePaddedAtomicUsize::new(0),
            tail: CachePaddedAtomicUsize::new(0),
            slots: slots.into_boxed_slice(),
            mask,
            capacity: cap,
        }
    }

    /// Returns the capacity of the buffer.
    #[inline(always)]
    pub fn capacity(&self) -> usize {
        self.capacity
    }

    /// Attempts to push an item into the buffer.
    /// Returns `Err(item)` if the buffer is full.
    pub fn try_push(&self, item: T) -> Result<(), T> {
        let mut tail = self.tail.load(Ordering::Relaxed);
        loop {
            let slot = &self.slots[tail & self.mask];
            let seq = slot.sequence.load(Ordering::Acquire);
            let diff = (seq as isize).wrapping_sub(tail as isize);

            if diff == 0 {
                match self.tail.compare_exchange_weak(
                    tail,
                    tail.wrapping_add(1),
                    Ordering::Relaxed,
                    Ordering::Relaxed,
                ) {
                    Ok(_) => {
                        let mut guard = slot.data.lock().unwrap_or_else(|e| e.into_inner());
                        *guard = Some(item);
                        slot.sequence.store(tail.wrapping_add(1), Ordering::Release);
                        return Ok(());
                    }
                    Err(actual) => tail = actual,
                }
            } else if diff < 0 {
                // Buffer is full
                return Err(item);
            } else {
                tail = self.tail.load(Ordering::Relaxed);
            }
        }
    }

    /// Attempts to pop an item from the buffer.
    /// Returns `None` if the buffer is empty.
    pub fn try_pop(&self) -> Option<T> {
        let mut head = self.head.load(Ordering::Relaxed);
        loop {
            let slot = &self.slots[head & self.mask];
            let seq = slot.sequence.load(Ordering::Acquire);
            let diff = (seq as isize).wrapping_sub(head.wrapping_add(1) as isize);

            if diff == 0 {
                match self.head.compare_exchange_weak(
                    head,
                    head.wrapping_add(1),
                    Ordering::Relaxed,
                    Ordering::Relaxed,
                ) {
                    Ok(_) => {
                        let mut guard = slot.data.lock().unwrap_or_else(|e| e.into_inner());
                        let val = guard.take();
                        slot.sequence
                            .store(head.wrapping_add(self.capacity), Ordering::Release);
                        return val;
                    }
                    Err(actual) => head = actual,
                }
            } else if diff < 0 {
                // Buffer is empty
                return None;
            } else {
                head = self.head.load(Ordering::Relaxed);
            }
        }
    }

    /// Returns an estimate of the number of items currently in the buffer.
    pub fn len(&self) -> usize {
        let tail = self.tail.load(Ordering::Relaxed);
        let head = self.head.load(Ordering::Relaxed);
        tail.saturating_sub(head).min(self.capacity)
    }

    /// Returns whether the buffer appears empty.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns whether the buffer appears full.
    pub fn is_full(&self) -> bool {
        self.len() >= self.capacity
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use std::thread;

    #[test]
    fn test_ring_buffer_fifo() {
        let rb = RingBuffer::new(4);
        assert_eq!(rb.capacity(), 4);

        assert!(rb.try_push(10).is_ok());
        assert!(rb.try_push(20).is_ok());
        assert!(rb.try_push(30).is_ok());
        assert!(rb.try_push(40).is_ok());
        assert_eq!(rb.try_push(50), Err(50)); // Full

        assert_eq!(rb.try_pop(), Some(10));
        assert_eq!(rb.try_pop(), Some(20));
        assert!(rb.try_push(60).is_ok());

        assert_eq!(rb.try_pop(), Some(30));
        assert_eq!(rb.try_pop(), Some(40));
        assert_eq!(rb.try_pop(), Some(60));
        assert_eq!(rb.try_pop(), None); // Empty
    }

    #[test]
    fn test_ring_buffer_multithreaded() {
        let rb = Arc::new(RingBuffer::new(128));
        let num_items = 10_000;

        let rb_prod = Arc::clone(&rb);
        let producer = thread::spawn(move || {
            for i in 0..num_items {
                while rb_prod.try_push(i).is_err() {
                    std::hint::spin_loop();
                }
            }
        });

        let rb_cons = Arc::clone(&rb);
        let consumer = thread::spawn(move || {
            let mut received = Vec::with_capacity(num_items);
            for _ in 0..num_items {
                loop {
                    if let Some(val) = rb_cons.try_pop() {
                        received.push(val);
                        break;
                    }
                    std::hint::spin_loop();
                }
            }
            received
        });

        producer.join().unwrap();
        let received = consumer.join().unwrap();
        let expected: Vec<usize> = (0..num_items).collect();
        assert_eq!(received, expected);
    }
}
