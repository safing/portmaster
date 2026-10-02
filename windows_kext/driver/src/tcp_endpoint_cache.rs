//! Associates one cached TCP connection generation with its WFP endpoint.
//!
//! For an accepted inbound connection, WFP can report a provisional transport
//! endpoint handle at `ALE_AUTH_RECV_ACCEPT` and a different child endpoint handle
//! at `ALE_FLOW_ESTABLISHED`. The parent endpoint and tuple correlate those stages;
//! endpoint closure then consumes the established handle and exact instance ID.

use alloc::{
    collections::{BTreeMap, BTreeSet},
    vec::Vec,
};

use crate::connection_map::Key;

#[derive(Clone, Copy, Eq, PartialEq, Ord, PartialOrd)]
pub struct TcpEndpointConnection {
    pub key: Key,
    pub parent_endpoint_handle: Option<u64>,
    pub instance_id: u64,
}

#[derive(Clone, Copy, Eq, PartialEq, Ord, PartialOrd)]
struct TcpEndpointLookupKey {
    key: Key,
    parent_endpoint_handle: Option<u64>,
}

fn lookup_key(endpoint: &TcpEndpointConnection) -> TcpEndpointLookupKey {
    TcpEndpointLookupKey {
        key: endpoint.key,
        parent_endpoint_handle: endpoint.parent_endpoint_handle,
    }
}

/// Most authorization tuples have only one endpoint handle. Keep it inline so
/// indexing a new TCP connection does not also allocate a vector.
enum TcpEndpointHandles {
    One(u64),
    Multiple(Vec<u64>),
}

impl TcpEndpointHandles {
    fn iter(&self) -> core::slice::Iter<'_, u64> {
        match self {
            Self::One(handle) => core::slice::from_ref(handle).iter(),
            Self::Multiple(handles) => handles.iter(),
        }
    }

    fn insert(&mut self, handle: u64) {
        match self {
            Self::One(existing) if *existing == handle => {}
            Self::One(existing) => {
                let previous = *existing;
                *self = Self::Multiple(if previous < handle {
                    alloc::vec![previous, handle]
                } else {
                    alloc::vec![handle, previous]
                });
            }
            Self::Multiple(handles) => {
                if let Err(index) = handles.binary_search(&handle) {
                    handles.insert(index, handle);
                }
            }
        }
    }

    /// Returns whether the bucket became empty.
    fn remove(&mut self, handle: u64) -> bool {
        match self {
            Self::One(existing) => *existing == handle,
            Self::Multiple(handles) => {
                if let Ok(index) = handles.binary_search(&handle) {
                    handles.remove(index);
                }
                match handles.as_slice() {
                    [] => true,
                    [remaining] => {
                        let remaining = *remaining;
                        *self = Self::One(remaining);
                        false
                    }
                    _ => false,
                }
            }
        }
    }

    /// Keeps selected handles while preserving the inline one-handle case.
    fn retain(self, mut keep: impl FnMut(u64) -> bool) -> Option<Self> {
        match self {
            Self::One(handle) => keep(handle).then_some(Self::One(handle)),
            Self::Multiple(mut handles) => {
                handles.retain(|handle| keep(*handle));
                match handles.as_slice() {
                    [] => None,
                    [handle] => Some(Self::One(*handle)),
                    _ => Some(Self::Multiple(handles)),
                }
            }
        }
    }
}

#[derive(Clone, Copy)]
struct TcpEndpointRecord {
    endpoint: TcpEndpointConnection,
    established: bool,
    associated_at_ms: u64,
}

pub struct TcpEndpointCache {
    endpoints: BTreeMap<u64, TcpEndpointRecord>,
    /// Endpoint handles ordered by authorization tuple and parent listener.
    ///
    /// FLOW_ESTABLISHED normally resolves one of these small buckets. Keeping
    /// this reverse index avoids walking every endpoint belonging to unrelated
    /// connections while the cache is write-locked.
    lookup: BTreeMap<TcpEndpointLookupKey, TcpEndpointHandles>,
}

impl TcpEndpointCache {
    pub fn new() -> Self {
        Self {
            endpoints: BTreeMap::new(),
            lookup: BTreeMap::new(),
        }
    }

    fn add_lookup_handle(&mut self, key: TcpEndpointLookupKey, endpoint_handle: u64) {
        match self.lookup.entry(key) {
            alloc::collections::btree_map::Entry::Occupied(entry) => {
                entry.into_mut().insert(endpoint_handle);
            }
            alloc::collections::btree_map::Entry::Vacant(entry) => {
                entry.insert(TcpEndpointHandles::One(endpoint_handle));
            }
        }
    }

    fn remove_lookup_handle(&mut self, key: TcpEndpointLookupKey, endpoint_handle: u64) {
        let remove_key = self
            .lookup
            .get_mut(&key)
            .is_some_and(|handles| handles.remove(endpoint_handle));
        if remove_key {
            self.lookup.remove(&key);
        }
    }

    /// Removes every alias for one exact connection generation without scanning
    /// endpoint handles belonging to other tuples.
    fn remove_endpoint_aliases(&mut self, endpoint: TcpEndpointConnection) {
        let key = lookup_key(&endpoint);
        let Some(handles) = self.lookup.remove(&key) else {
            return;
        };

        let remaining = handles.retain(|endpoint_handle| {
            let remove = self
                .endpoints
                .get(&endpoint_handle)
                .is_some_and(|record| record.endpoint == endpoint);
            if remove {
                self.endpoints.remove(&endpoint_handle);
            }
            !remove
        });
        if let Some(handles) = remaining {
            self.lookup.insert(key, handles);
        }
    }

    fn rebuild_lookup(&mut self) {
        let endpoints = &self.endpoints;
        let lookup = &mut self.lookup;
        lookup.clear();
        // `endpoints` is a BTreeMap, so handles arrive in the same order used by
        // the old full-cache scan. This also keeps resolution deterministic when
        // more than one generation shares a tuple and parent listener.
        for (&endpoint_handle, record) in endpoints {
            match lookup.entry(lookup_key(&record.endpoint)) {
                alloc::collections::btree_map::Entry::Occupied(entry) => {
                    entry.into_mut().insert(endpoint_handle);
                }
                alloc::collections::btree_map::Entry::Vacant(entry) => {
                    entry.insert(TcpEndpointHandles::One(endpoint_handle));
                }
            }
        }
    }

    /// Associates an endpoint without a timeout timestamp. This is retained for
    /// callers and tests that do not need stale-unestablished cleanup metadata.
    #[allow(dead_code)]
    pub fn associate_instance(
        &mut self,
        endpoint_handle: u64,
        key: Key,
        parent_endpoint_handle: Option<u64>,
        instance_id: u64,
    ) -> bool {
        self.associate_instance_at(endpoint_handle, key, parent_endpoint_handle, instance_id, 0)
    }

    /// Associates one exact live connection-cache generation with its WFP endpoint
    /// and records when its authorization identity appeared.
    ///
    /// Reauthorization can repeat the same association while omitting optional
    /// parent-endpoint metadata. A different connection for an already tracked
    /// handle is rejected: replacing it could let an old closure consume a newer
    /// connection generation.
    ///
    /// # Parameters
    /// * `associated_at_ms` - Timestamp in milliseconds when the endpoint was associated.
    ///   **Special value 0** marks the entry as exempt from timeout-based cleanup;
    ///   such entries are retained until explicitly removed via `take()` or `clear()`.
    ///   Established connections typically use 0 since they should only be removed
    ///   on explicit closure events, not by timeout.
    pub fn associate_instance_at(
        &mut self,
        endpoint_handle: u64,
        key: Key,
        parent_endpoint_handle: Option<u64>,
        instance_id: u64,
        associated_at_ms: u64,
    ) -> bool {
        if endpoint_handle == 0 || instance_id == 0 {
            return false;
        }

        let existing_update = match self.endpoints.get_mut(&endpoint_handle) {
            Some(existing) => {
                if existing.endpoint.key != key || existing.endpoint.instance_id != instance_id {
                    return false;
                }

                let move_lookup = match (
                    existing.endpoint.parent_endpoint_handle,
                    parent_endpoint_handle,
                ) {
                    (Some(existing_parent), Some(parent)) => return existing_parent == parent,
                    (None, Some(parent)) => {
                        let old_key = lookup_key(&existing.endpoint);
                        existing.endpoint.parent_endpoint_handle = Some(parent);
                        Some((old_key, lookup_key(&existing.endpoint)))
                    }
                    _ => None,
                };
                Some(move_lookup)
            }
            None => None,
        };
        if let Some(move_lookup) = existing_update {
            if let Some((old_key, new_key)) = move_lookup {
                self.remove_lookup_handle(old_key, endpoint_handle);
                self.add_lookup_handle(new_key, endpoint_handle);
            }
            return true;
        }
        let endpoint = TcpEndpointConnection {
            key,
            parent_endpoint_handle,
            instance_id,
        };
        let lookup_key = lookup_key(&endpoint);
        // A post-establishment reauthorization may expose a new alias. If any
        // existing alias already represents the established flow, preserve that
        // state so cleanup cannot mistake the alias for a pending connection.
        let established = self.lookup.get(&lookup_key).is_some_and(|handles| {
            handles.iter().any(|handle| {
                self.endpoints
                    .get(handle)
                    .is_some_and(|record| record.endpoint == endpoint && record.established)
            })
        });
        self.endpoints.insert(
            endpoint_handle,
            TcpEndpointRecord {
                endpoint,
                established,
                associated_at_ms,
            },
        );
        self.add_lookup_handle(lookup_key, endpoint_handle);
        true
    }

    /// Resolves the live authorization generation corresponding to an established
    /// flow. Matching the parent endpoint as well as the tuple prevents connections
    /// accepted by different listeners from sharing the transition.
    pub fn resolve_live_instance(
        &self,
        key: &Key,
        parent_endpoint_handle: Option<u64>,
        mut is_live: impl FnMut(u64) -> bool,
    ) -> Option<TcpEndpointConnection> {
        let lookup_key = TcpEndpointLookupKey {
            key: *key,
            parent_endpoint_handle,
        };
        self.lookup.get(&lookup_key)?.iter().find_map(|handle| {
            self.endpoints
                .get(handle)
                .and_then(|record| is_live(record.endpoint.instance_id).then_some(record.endpoint))
        })
    }

    /// Replaces every provisional/alias handle for a connection generation with
    /// the endpoint handle reported for its established flow.
    pub fn rebind_established(
        &mut self,
        endpoint_handle: u64,
        endpoint: TcpEndpointConnection,
    ) -> bool {
        if endpoint_handle == 0 || endpoint.instance_id == 0 {
            return false;
        }
        if self
            .endpoints
            .get(&endpoint_handle)
            .is_some_and(|existing| existing.endpoint != endpoint)
        {
            return false;
        }

        self.remove_endpoint_aliases(endpoint);
        self.endpoints.insert(
            endpoint_handle,
            TcpEndpointRecord {
                endpoint,
                established: true,
                associated_at_ms: 0,
            },
        );
        self.add_lookup_handle(lookup_key(&endpoint), endpoint_handle);
        true
    }

    /// Removes unestablished endpoint generations that have exceeded `cutoff_ms`.
    /// All aliases of an expired generation are removed together, so a later
    /// closure cannot retain a provisional handle after the connection is retired.
    pub fn take_unestablished_before(
        &mut self,
        cutoff_ms: u64,
        mut should_expire: impl FnMut(&TcpEndpointConnection) -> bool,
    ) -> Vec<TcpEndpointConnection> {
        let mut expired_set = BTreeSet::new();
        for record in self.endpoints.values() {
            if !record.established
                && record.associated_at_ms != 0
                && record.associated_at_ms <= cutoff_ms
                && should_expire(&record.endpoint)
            {
                expired_set.insert(record.endpoint);
            }
        }

        if !expired_set.is_empty() {
            self.endpoints
                .retain(|_, record| !expired_set.contains(&record.endpoint));
            self.rebuild_lookup();
        }
        expired_set.into_iter().collect()
    }

    /// Consumes the exact connection identity assigned to the closing endpoint.
    /// Every alias of the same generation is removed defensively as well.
    pub fn take(&mut self, endpoint_handle: u64) -> Option<TcpEndpointConnection> {
        if endpoint_handle == 0 {
            return None;
        }

        let endpoint = self.endpoints.get(&endpoint_handle)?.endpoint;
        self.remove_endpoint_aliases(endpoint);
        Some(endpoint)
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        self.endpoints.len()
    }

    pub fn clear(&mut self) {
        self.endpoints.clear();
        self.lookup.clear();
    }
}

#[cfg(test)]
impl TcpEndpointCache {
    fn lookup_is_consistent(&self) -> bool {
        let indexed_handles: usize = self
            .lookup
            .values()
            .map(|handles| handles.iter().len())
            .sum();
        indexed_handles == self.endpoints.len()
            && self.lookup.iter().all(|(key, handles)| {
                handles.iter().all(|handle| {
                    self.endpoints
                        .get(handle)
                        .is_some_and(|record| lookup_key(&record.endpoint) == *key)
                })
            })
    }
}

#[cfg(test)]
mod tests {
    use super::TcpEndpointCache;
    use crate::{
        connection::{Connection, ConnectionV4, Direction},
        connection_map::{ConnectionMap, Key},
    };
    use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address};

    fn key() -> Key {
        Key {
            protocol: IpProtocol::Tcp,
            local_address: IpAddress::Ipv4(Ipv4Address::new(10, 0, 0, 1)),
            local_port: 40_000,
            remote_address: IpAddress::Ipv4(Ipv4Address::new(192, 0, 2, 1)),
            remote_port: 443,
        }
    }

    fn connection(key: &Key, process_id: u64) -> ConnectionV4 {
        ConnectionV4::from_key(key, process_id, Direction::Outbound).expect("IPv4 key")
    }

    #[test]
    fn established_flow_rebinds_the_authorization_handle() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();
        let parent = Some(20);

        assert!(cache.associate_instance(10, tuple, parent, 100));
        let endpoint = cache
            .resolve_live_instance(&tuple, parent, |instance_id| instance_id == 100)
            .expect("authorization generation");
        assert!(cache.rebind_established(30, endpoint));

        assert!(cache.lookup_is_consistent());
        assert!(cache.take(10).is_none());
        assert_eq!(
            cache.take(30).expect("established endpoint").instance_id,
            100
        );
    }

    #[test]
    fn reauthorization_may_omit_parent_endpoint() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance(10, tuple, Some(20), 100));
        assert!(cache.associate_instance(10, tuple, None, 100));
        assert_eq!(
            cache
                .resolve_live_instance(&tuple, Some(20), |_| true)
                .expect("preserved parent endpoint")
                .instance_id,
            100
        );
    }

    #[test]
    fn repeated_association_fills_but_does_not_replace_parent() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance(10, tuple, None, 100));
        assert!(cache.associate_instance(10, tuple, Some(20), 100));
        assert!(cache.lookup_is_consistent());
        assert!(!cache.associate_instance(10, tuple, Some(21), 100));
        assert!(!cache.associate_instance(10, tuple, None, 101));
        let mut other_tuple = tuple;
        other_tuple.remote_port = 8443;
        assert!(!cache.associate_instance(10, other_tuple, None, 100));
        assert_eq!(
            cache
                .resolve_live_instance(&tuple, Some(20), |_| true)
                .expect("filled parent endpoint")
                .instance_id,
            100
        );
    }

    #[test]
    fn flow_rebind_requires_matching_parent_and_live_instance() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();
        assert!(cache.associate_instance(10, tuple, Some(20), 100));

        assert!(cache
            .resolve_live_instance(&tuple, Some(21), |_| true)
            .is_none());
        assert!(cache
            .resolve_live_instance(&tuple, Some(20), |_| false)
            .is_none());
        assert_eq!(
            cache
                .resolve_live_instance(&tuple, Some(20), |_| true)
                .expect("matching generation")
                .instance_id,
            100
        );
    }

    #[test]
    fn delayed_closure_cannot_end_reused_tuple() {
        let tuple = key();
        let mut connections = ConnectionMap::new();
        let old = connection(&tuple, 100);
        let old_instance_id = old.get_instance_id();
        assert!(matches!(
            connections.insert_if_absent(old),
            Ok(instance_id) if instance_id == old_instance_id
        ));

        let mut endpoints = TcpEndpointCache::new();
        assert!(endpoints.associate_instance(10, tuple, Some(1), old_instance_id));
        assert!(connections.end_instance(tuple, old_instance_id).is_some());

        let replacement = connection(&tuple, 200);
        let replacement_instance_id = replacement.get_instance_id();
        assert!(matches!(
            connections.insert_if_absent(replacement),
            Ok(instance_id) if instance_id == replacement_instance_id
        ));
        assert!(endpoints.associate_instance(20, tuple, Some(1), replacement_instance_id));

        let delayed = endpoints.take(10).expect("old endpoint identity");
        assert_eq!(delayed.instance_id, old_instance_id);
        assert!(connections
            .end_instance(delayed.key, delayed.instance_id)
            .is_none());
        assert!(connections.has_live_instance(&tuple, replacement_instance_id));

        let current = endpoints.take(20).expect("replacement endpoint identity");
        assert_eq!(current.instance_id, replacement_instance_id);
        assert!(connections
            .end_instance(current.key, current.instance_id)
            .is_some());
    }

    #[test]
    fn identical_tuple_isolated_by_parent_endpoint() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance(10, tuple, Some(20), 100));
        assert!(cache.associate_instance(11, tuple, Some(21), 101));

        assert_eq!(
            cache
                .resolve_live_instance(&tuple, Some(20), |_| true)
                .expect("first listener child")
                .instance_id,
            100
        );
        assert_eq!(
            cache
                .resolve_live_instance(&tuple, Some(21), |_| true)
                .expect("second listener child")
                .instance_id,
            101
        );
        assert!(cache
            .resolve_live_instance(&tuple, None, |_| true)
            .is_none());
    }

    #[test]
    fn parentless_authorization_matches_only_parentless_flow() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance(10, tuple, None, 100));
        assert!(cache
            .resolve_live_instance(&tuple, Some(20), |_| true)
            .is_none());
        assert_eq!(
            cache
                .resolve_live_instance(&tuple, None, |_| true)
                .expect("parentless outbound endpoint")
                .instance_id,
            100
        );
    }

    #[test]
    fn established_rebind_removes_every_provisional_alias() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance(10, tuple, Some(20), 100));
        assert!(cache.associate_instance(11, tuple, Some(20), 100));
        let endpoint = cache
            .resolve_live_instance(&tuple, Some(20), |instance_id| instance_id == 100)
            .expect("authorization generation");

        assert!(cache.rebind_established(30, endpoint));
        assert!(cache.lookup_is_consistent());
        assert!(cache.take(10).is_none());
        assert!(cache.take(11).is_none());
        assert!(cache
            .take(30)
            .is_some_and(|candidate| candidate == endpoint));
    }

    #[test]
    fn timeout_removes_all_provisional_aliases_for_one_generation() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance_at(10, tuple, Some(20), 100, 10));
        assert!(cache.associate_instance_at(11, tuple, Some(20), 100, 10));
        assert!(cache
            .take_unestablished_before(20, |_| true)
            .iter()
            .any(|endpoint| endpoint.instance_id == 100));
        assert!(cache.lookup_is_consistent());
        assert_eq!(cache.get_entries_count(), 0);
    }

    #[test]
    fn timeout_does_not_remove_rebound_established_generation() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance_at(10, tuple, Some(20), 100, 10));
        let endpoint = cache
            .resolve_live_instance(&tuple, Some(20), |_| true)
            .expect("provisional endpoint");
        assert!(cache.rebind_established(30, endpoint));

        assert!(cache
            .take_unestablished_before(20, |_| true)
            .is_empty());
        assert!(cache.take(30).is_some());
    }

    #[test]
    fn established_alias_is_not_expired_with_a_provisional_alias() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(cache.associate_instance_at(10, tuple, Some(20), 100, 10));
        let endpoint = cache
            .resolve_live_instance(&tuple, Some(20), |_| true)
            .expect("provisional endpoint");
        assert!(cache.rebind_established(30, endpoint));
        assert!(cache.associate_instance_at(31, tuple, Some(20), 100, 10));

        assert!(cache
            .take_unestablished_before(20, |_| true)
            .is_empty());
        assert!(cache.take(30).is_some());
        assert!(cache.take(31).is_none());
    }

    #[test]
    fn established_handle_cannot_replace_another_generation() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();
        assert!(cache.associate_instance(10, tuple, Some(20), 100));
        assert!(cache.associate_instance(30, tuple, Some(20), 101));
        let endpoint = cache
            .resolve_live_instance(&tuple, Some(20), |instance_id| instance_id == 100)
            .expect("first generation");

        assert!(!cache.rebind_established(30, endpoint));
        assert!(cache
            .take(10)
            .is_some_and(|candidate| candidate == endpoint));
        assert_eq!(
            cache.take(30).expect("conflicting generation").instance_id,
            101
        );
    }

    #[test]
    fn zero_endpoint_or_instance_is_never_associated() {
        let mut cache = TcpEndpointCache::new();
        let tuple = key();

        assert!(!cache.associate_instance(0, tuple, Some(20), 100));
        assert!(!cache.associate_instance(10, tuple, Some(20), 0));
        assert!(cache
            .resolve_live_instance(&tuple, Some(20), |_| true)
            .is_none());
        assert!(!cache.rebind_established(
            0,
            super::TcpEndpointConnection {
                key: tuple,
                parent_endpoint_handle: Some(20),
                instance_id: 100,
            }
        ));
    }
}
