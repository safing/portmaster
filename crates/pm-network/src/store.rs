//! High-performance sharded connection store, a low-contention port of Go `network/connection_store.go`.

use std::collections::HashMap;
use std::hash::{BuildHasher, Hash, Hasher};
use foldhash::fast::RandomState;
use parking_lot::RwLock;

use crate::connection::SharedConnection;

const NUM_SHARDS: usize = 32;

/// Connections partitioned across 32 shards to minimize lock contention under
/// high concurrent packet throughput.
pub struct ConnectionStore {
    shards: Box<[RwLock<HashMap<String, SharedConnection, RandomState>>; NUM_SHARDS]>,
    hasher: RandomState,
}

impl Default for ConnectionStore {
    fn default() -> Self {
        Self::new()
    }
}

impl ConnectionStore {
    pub fn new() -> ConnectionStore {
        let hasher = RandomState::default();
        let shards = Box::new(std::array::from_fn(|_| {
            RwLock::new(HashMap::with_hasher(RandomState::default()))
        }));
        ConnectionStore { shards, hasher }
    }

    #[inline]
    fn shard_index(&self, id: &str) -> usize {
        let mut h = self.hasher.build_hasher();
        id.hash(&mut h);
        (h.finish() as usize) % NUM_SHARDS
    }

    pub fn add(&self, id: &str, conn: &SharedConnection) {
        let idx = self.shard_index(id);
        self.shards[idx].write().insert(id.to_string(), conn.clone());
    }

    pub fn delete(&self, id: &str) {
        let idx = self.shard_index(id);
        self.shards[idx].write().remove(id);
    }

    pub fn get(&self, id: &str) -> Option<SharedConnection> {
        let idx = self.shard_index(id);
        self.shards[idx].read().get(id).cloned()
    }

    /// Returns the first connection whose ID matches the prefix; the result is
    /// not deterministic if several match.
    pub fn find_by_prefix(&self, prefix: &str) -> Option<SharedConnection> {
        for shard in self.shards.iter() {
            let guard = shard.read();
            if let Some((_, conn)) = guard.iter().find(|(id, _)| id.starts_with(prefix)) {
                return Some(conn.clone());
            }
        }
        None
    }

    /// Returns a snapshot of all connections.
    pub fn list(&self) -> Vec<SharedConnection> {
        let mut total = Vec::new();
        for shard in self.shards.iter() {
            let guard = shard.read();
            total.extend(guard.values().cloned());
        }
        total
    }

    pub fn len(&self) -> usize {
        self.shards.iter().map(|s| s.read().len()).sum()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Counts connections with `ended != 0`, exactly like Go `active()`
    /// (which despite its name counts the ended ones).
    pub fn active(&self) -> usize {
        let mut count = 0;
        for shard in self.shards.iter() {
            let guard = shard.read();
            for conn in guard.values() {
                if conn.lock().expect("lock poisoned").ended != 0 {
                    count += 1;
                }
            }
        }
        count
    }
}
