use core::mem;
use alloc::collections::VecDeque;
use wdk::rw_spin_lock::RwSpinLock;

// This file contains an implementation of a generic ID cache 
// for storing values of any type `T` associated with unique IDs.
// It is based on `VecDeque` and uses a read–write spin lock for thread-safe access.
// The cache provides methods to push values, pop entries by ID, get the entry count,
// and clear all entries.
//
// It is similar to `IdCache` in `id_cache.rs`, but is generic over the value type `T`.

pub struct Entry<T> {
    pub value: T,
    pub id: u64,
}

pub struct GenericIdCache<T> {
    values: VecDeque<Entry<T>>,
    lock: RwSpinLock,
    next_id: u64,
}

impl<T> GenericIdCache<T> {
    pub fn new() -> Self {
        Self {
            values: VecDeque::with_capacity(1000),
            lock: RwSpinLock::default(),
            next_id: 1, // 0 is invalid id
        }
    }

    /// Push a value and return its ID
    pub fn push(&mut self, value: T) -> u64 {
        let _guard = self.lock.write_lock();
        let id = self.next_id;
        self.values.push_back(Entry { value, id });
        self.next_id = self.next_id.wrapping_add(1);
        id
    }

    /// Pop a value by ID
    pub fn pop_id(&mut self, id: u64) -> Option<T> {
        let _guard = self.lock.write_lock();
        if let Ok(index) = self.values.binary_search_by_key(&id, |val| val.id) {
            return Some(self.values.remove(index).unwrap().value);
        }
        None
    }

    /// Get count of entries
    pub fn get_entries_count(&self) -> usize {
        let _guard = self.lock.read_lock();
        self.values.len()
    }

    /// Clear all entries, returning them
    /// Note: The final cache capacity will be 1 after this operation
    pub fn pop_all(&mut self) -> VecDeque<Entry<T>> {        
        // NOTE: assume that we do not plan to use it again.
        //       So we can decrease size allocation overhead.
        let mut values = VecDeque::with_capacity(1); 

        let _guard = self.lock.write_lock();
        mem::swap(&mut self.values, &mut values);

        return values;
    }
}