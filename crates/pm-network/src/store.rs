//! The connection store, a port of Go `network/connection_store.go`.

use std::collections::HashMap;
use std::sync::RwLock;

use crate::connection::SharedConnection;

/// Connections by ID.
#[derive(Default)]
pub struct ConnectionStore {
    items: RwLock<HashMap<String, SharedConnection>>,
}

impl ConnectionStore {
    pub fn new() -> ConnectionStore {
        ConnectionStore::default()
    }

    pub fn add(&self, id: &str, conn: &SharedConnection) {
        self.items
            .write()
            .expect("lock poisoned")
            .insert(id.to_string(), conn.clone());
    }

    pub fn delete(&self, id: &str) {
        self.items.write().expect("lock poisoned").remove(id);
    }

    pub fn get(&self, id: &str) -> Option<SharedConnection> {
        self.items.read().expect("lock poisoned").get(id).cloned()
    }

    /// Returns the first connection whose ID matches the prefix; the result is
    /// not deterministic if several match.
    pub fn find_by_prefix(&self, prefix: &str) -> Option<SharedConnection> {
        self.items
            .read()
            .expect("lock poisoned")
            .iter()
            .find(|(id, _)| id.starts_with(prefix))
            .map(|(_, conn)| conn.clone())
    }

    /// Returns a snapshot of all connections.
    pub fn list(&self) -> Vec<SharedConnection> {
        self.items
            .read()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect()
    }

    pub fn len(&self) -> usize {
        self.items.read().expect("lock poisoned").len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Counts connections with `ended != 0`, exactly like Go `active()`
    /// (which despite its name counts the ended ones).
    pub fn active(&self) -> usize {
        self.list()
            .iter()
            .filter(|conn| conn.lock().expect("lock poisoned").ended != 0)
            .count()
    }
}
