//! Associates cached UDP remote tuples with their socket endpoint.
//!
//! WFP creates one UDP ALE flow per remote peer, but reports endpoint closure only
//! once for the socket. The closure indication therefore cannot identify every
//! remote tuple by its fixed fields. The transport endpoint handle is the stable
//! correlation key shared by authorization and endpoint-closure indications.

use alloc::collections::BTreeMap;
use alloc::vec::Vec;

#[cfg(not(test))]
use wdk::rw_spin_lock::RwSpinLock;

#[cfg(test)]
struct RwSpinLock;

#[cfg(test)]
impl RwSpinLock {
    const fn default() -> Self {
        Self
    }

    fn read_lock(&self) {}
    fn write_lock(&self) {}
}

use crate::connection_map::Key;

#[derive(Clone, Copy)]
pub struct UdpEndpointPeer {
    pub key: Key,
    pub instance_id: u64,
    pub associated_at_ms: u64,
}

pub struct UdpEndpointCache {
    endpoints: BTreeMap<u64, Vec<UdpEndpointPeer>>,
    lock: RwSpinLock,
}

impl UdpEndpointCache {
    pub fn new() -> Self {
        Self {
            endpoints: BTreeMap::new(),
            lock: RwSpinLock::default(),
        }
    }

    /// Associates one concrete connection-cache instance with its endpoint.
    ///
    /// Returns true only when this call inserted a new association. Callers that
    /// expose a context to WFP can use the result to roll back their own insertion
    /// if `FwpsFlowAssociateContext0` fails without removing an association that
    /// was already tracking the connection independently.
    ///
    /// This version marks the association with `associated_at_ms = 0`, making it
    /// exempt from timeout-based cleanup. For time-aware cleanup, use
    /// `associate_instance_at` instead.
    #[allow(dead_code)]
    pub fn associate_instance(&mut self, endpoint_handle: u64, key: Key, instance_id: u64) -> bool {
        self.associate_instance_at(endpoint_handle, key, instance_id, 0)
    }

    /// Associates one concrete connection-cache instance with its endpoint and
    /// records when the association was created.
    ///
    /// Returns true only when this call inserted a new association. Callers that
    /// expose a context to WFP can use the result to roll back their own insertion
    /// if `FwpsFlowAssociateContext0` fails without removing an association that
    /// was already tracking the connection independently.
    ///
    /// # Parameters
    /// * `associated_at_ms` - Timestamp in milliseconds when the association was created.
    ///   **Special value 0** marks the entry as exempt from timeout-based cleanup;
    ///   such entries are retained until explicitly removed. Non-zero values enable
    ///   protection against race conditions during periodic cleanup by preventing
    ///   removal of recently created associations.
    pub fn associate_instance_at(&mut self, endpoint_handle: u64, key: Key, instance_id: u64, associated_at_ms: u64) -> bool {
        if endpoint_handle == 0 || instance_id == 0 {
            return false;
        }

        let _guard = self.lock.write_lock();
        if let Some(peers) = self.endpoints.get_mut(&endpoint_handle) {
            if peers
                .iter()
                .any(|peer| peer.key == key && peer.instance_id == instance_id)
            {
                return false;
            }
            peers.push(UdpEndpointPeer { key, instance_id, associated_at_ms });
            return true;
        }

        self.endpoints.insert(
            endpoint_handle,
            alloc::vec![UdpEndpointPeer { key, instance_id, associated_at_ms }],
        );
        true
    }

    /// Atomically resolves one exact endpoint/tuple association and ensures its
    /// connection instance is still live through `accept_instance`.
    ///
    /// The callback executes while the endpoint map is locked. Endpoint closure
    /// therefore cannot consume the association between the lookup and the live
    /// connection check, which prevents a flow-established callback for an old
    /// socket from falling through to a tuple replacement.
    pub fn with_instance_id<T>(
        &self,
        endpoint_handle: u64,
        key: &Key,
        mut accept_instance: impl FnMut(u64) -> Option<T>,
    ) -> Option<T> {
        if endpoint_handle == 0 {
            return None;
        }

        let _guard = self.lock.read_lock();
        for peer in self.endpoints.get(&endpoint_handle)? {
            if peer.key == *key {
                if let Some(value) = accept_instance(peer.instance_id) {
                    return Some(value);
                }
            }
        }
        None
    }

    /// Removes a peer whose WFP ALE flow has ended. Empty endpoint entries are
    /// dropped with their peer allocation; unknown/repeated lifetime indications
    /// are safe to ignore and therefore need neither empty sentinels nor tombstones.
    pub fn dissociate(&mut self, endpoint_handle: u64, key: Key, instance_id: u64) -> bool {
        if endpoint_handle == 0 || instance_id == 0 {
            return false;
        }

        let _guard = self.lock.write_lock();
        let Some(peers) = self.endpoints.get_mut(&endpoint_handle) else {
            return false;
        };
        let previous_len = peers.len();
        peers.retain(|peer| peer.key != key || peer.instance_id != instance_id);
        let removed = peers.len() != previous_len;
        let remove_endpoint = removed && peers.is_empty();
        if removed && !remove_endpoint {
            release_excess_capacity(peers);
        }
        if remove_endpoint {
            self.endpoints.remove(&endpoint_handle);
        }
        removed
    }

    /// Returns the connection-cache instance IDs represented by this cache.
    ///
    /// The snapshot is taken before the live connection snapshot during periodic
    /// cleanup. Therefore an association created afterwards is left for the next
    /// pass rather than being mistaken for stale state.
    pub fn instance_ids(&self) -> Vec<u64> {
        let _guard = self.lock.read_lock();
        let count = self.endpoints.values().map(Vec::len).sum();
        let mut instance_ids = Vec::with_capacity(count);
        for peers in self.endpoints.values() {
            instance_ids.extend(peers.iter().map(|peer| peer.instance_id));
        }
        instance_ids
    }

    /// Removes associations for connection-cache instances that no longer exist.
    ///
    /// Empty endpoint entries are removed together with their peer buffers. The
    /// input is consumed so sorting it does not require another allocation.
    ///
    /// # Parameters
    /// * `instance_ids` - List of dead instance IDs to remove
    /// * `current_time_ms` - Current timestamp in milliseconds. Associations created
    ///   within the grace period (1000ms) are not removed to avoid race conditions
    ///   during the window between cache association and connection_map insertion.
    pub fn remove_instances(&mut self, mut instance_ids: Vec<u64>, current_time_ms: u64) -> usize {
        if instance_ids.is_empty() {
            return 0;
        }
        instance_ids.sort_unstable();
        instance_ids.dedup();

        let _guard = self.lock.write_lock();
        let mut removed = 0;
        const GRACE_PERIOD_MS: u64 = 1000;
        self.endpoints.retain(|_, peers| {
            let previous_len = peers.len();
            peers.retain(|peer| {
                // Protect entries marked as exempt (associated_at_ms == 0)
                if peer.associated_at_ms == 0 {
                    return true;
                }
                // Protect recently created entries (within grace period)
                if current_time_ms >= peer.associated_at_ms
                    && current_time_ms - peer.associated_at_ms < GRACE_PERIOD_MS {
                    return true;
                }
                // Remove if instance_id is in the dead list
                instance_ids.binary_search(&peer.instance_id).is_err()
            });
            let removed_from_endpoint = previous_len - peers.len();
            removed += removed_from_endpoint;
            if removed_from_endpoint != 0 && !peers.is_empty() {
                release_excess_capacity(peers);
            }
            !peers.is_empty()
        });
        removed
    }

    /// Consumes one endpoint lifetime indication and returns every UDP tuple
    /// observed on its socket.
    ///
    /// An unknown handle leaves no state behind. Callers with a concrete handle
    /// deliberately do not use a local-port fallback for an unknown or repeated
    /// indication: ignoring it is safer than ending a replacement socket, and
    /// native flow deletion can still retire the matching peer state.
    pub fn take(&mut self, endpoint_handle: u64) -> Option<Vec<UdpEndpointPeer>> {
        if endpoint_handle == 0 {
            return None;
        }

        let _guard = self.lock.write_lock();
        self.endpoints.remove(&endpoint_handle)
    }

    #[allow(dead_code)]
    pub fn get_entries_counts(&self) -> (usize, usize) {
        let _guard = self.lock.read_lock();
        let peers = self.endpoints.values().map(Vec::len).sum();
        (self.endpoints.len(), peers)
    }

    pub fn clear(&mut self) {
        let _guard = self.lock.write_lock();
        self.endpoints.clear();
    }
}

/// Releases high-water allocation without reallocating after every individual
/// flow deletion. Non-empty vectors shrink after capacity exceeds live state by
/// at least four times; empty vectors are dropped with their endpoint entry.
fn release_excess_capacity(peers: &mut Vec<UdpEndpointPeer>) {
    if peers.len() <= peers.capacity() / 4 {
        peers.shrink_to_fit();
    }
}

#[cfg(test)]
mod tests {
    use super::UdpEndpointCache;
    use crate::connection_map::Key;
    use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address};

    fn key(remote_port: u16) -> Key {
        Key {
            protocol: IpProtocol::Udp,
            local_address: IpAddress::Ipv4(Ipv4Address::new(10, 0, 0, 1)),
            local_port: 40_000,
            remote_address: IpAddress::Ipv4(Ipv4Address::new(192, 0, 2, 1)),
            remote_port,
        }
    }

    #[test]
    fn groups_remote_tuples_under_one_endpoint_and_takes_once() {
        let mut cache = UdpEndpointCache::new();
        assert!(cache.associate_instance(10, key(1000), 100));
        assert!(cache.associate_instance(10, key(1001), 101));
        assert!(!cache.associate_instance(10, key(1001), 101));

        let closed = cache.take(10).expect("endpoint was not tracked");
        assert_eq!(closed.len(), 2);
        assert!(cache.take(10).is_none());
    }

    #[test]
    fn resolves_only_an_accepted_exact_instance_for_a_flow_callback() {
        let mut cache = UdpEndpointCache::new();
        let peer = key(1000);
        assert!(cache.associate_instance(10, peer, 100));
        assert!(cache.associate_instance(10, peer, 300));
        assert!(cache.associate_instance(20, peer, 200));

        assert_eq!(
            cache.with_instance_id(10, &peer, |instance_id| {
                (instance_id == 300).then_some(instance_id)
            }),
            Some(300)
        );
        assert_eq!(
            cache.with_instance_id(20, &peer, |instance_id| Some(instance_id)),
            Some(200)
        );
        assert_eq!(
            cache.with_instance_id(10, &peer, |instance_id| {
                (instance_id == 200).then_some(instance_id)
            }),
            None
        );
        assert_eq!(
            cache.with_instance_id(30, &peer, |instance_id| Some(instance_id)),
            None
        );
        assert_eq!(
            cache.with_instance_id(10, &key(1001), |instance_id| Some(instance_id)),
            None
        );
    }

    #[test]
    fn flow_delete_dissociates_only_its_cache_instance() {
        let mut cache = UdpEndpointCache::new();
        let peer = key(1000);
        assert!(cache.associate_instance(10, peer, 100));
        assert!(cache.associate_instance(10, peer, 200));

        assert!(cache.dissociate(10, peer, 100));
        assert!(!cache.dissociate(10, peer, 100));

        let closed = cache.take(10).expect("endpoint was no longer tracked");
        assert_eq!(closed.len(), 1);
        assert_eq!(closed[0].instance_id, 200);
    }

    #[test]
    fn flow_delete_removes_empty_endpoint_and_peer_buffer() {
        let mut cache = UdpEndpointCache::new();
        let peer = key(1000);
        assert!(cache.associate_instance(10, peer, 100));

        assert!(cache.dissociate(10, peer, 100));
        assert!(!cache.endpoints.contains_key(&10));
        assert!(cache.take(10).is_none());
    }

    #[test]
    fn dissociate_releases_excess_capacity_while_peers_remain() {
        let mut cache = UdpEndpointCache::new();
        for offset in 0..64u16 {
            assert!(cache.associate_instance(10, key(1_000 + offset), 1_000 + u64::from(offset),));
        }
        let peak_capacity = cache.endpoints.get(&10).expect("endpoint").capacity();

        for offset in 0..63u16 {
            assert!(cache.dissociate(10, key(1_000 + offset), 1_000 + u64::from(offset),));
        }

        let peers = cache.endpoints.get(&10).expect("live peer was removed");
        assert_eq!(peers.len(), 1);
        assert!(peers.capacity() < peak_capacity);
        assert!(peers.capacity() <= peers.len() * 4);
    }

    #[test]
    fn unknown_handles_leave_no_tombstones() {
        let mut cache = UdpEndpointCache::new();
        assert!(cache.take(10).is_none());
        assert!(cache.take(10).is_none());
        assert!(cache.endpoints.is_empty());
    }

    #[test]
    fn cleanup_removes_stale_instances_and_empty_endpoints() {
        let mut cache = UdpEndpointCache::new();
        assert!(cache.associate_instance_at(10, key(1000), 100, 1000));
        assert!(cache.associate_instance_at(10, key(1001), 101, 1000));
        assert!(cache.associate_instance_at(20, key(2000), 200, 1000));

        assert_eq!(cache.remove_instances(alloc::vec![200, 100, 200], 3000), 2);
        assert_eq!(cache.instance_ids(), alloc::vec![101]);

        let first = cache.take(10).expect("first endpoint was removed");
        assert_eq!(first.len(), 1);
        assert_eq!(first[0].instance_id, 101);
        assert!(cache.take(20).is_none());
    }

    #[test]
    fn cleanup_protects_recently_associated_instances() {
        let mut cache = UdpEndpointCache::new();
        // Associate with timestamp 1000ms
        assert!(cache.associate_instance_at(10, key(1000), 100, 1000));
        // Associate with timestamp 2000ms
        assert!(cache.associate_instance_at(10, key(1001), 101, 2000));

        // Try to remove at 2500ms - first is old (1500ms ago), second is recent (500ms ago)
        assert_eq!(cache.remove_instances(alloc::vec![100, 101], 2500), 1);

        // Only the old one should be removed
        assert_eq!(cache.instance_ids(), alloc::vec![101]);
    }

    #[test]
    fn cleanup_protects_exempt_instances() {
        let mut cache = UdpEndpointCache::new();
        // Associate with associated_at_ms=0 (exempt from cleanup)
        assert!(cache.associate_instance_at(10, key(1000), 100, 0));
        // Associate with timestamp
        assert!(cache.associate_instance_at(10, key(1001), 101, 1000));

        // Try to remove both at a much later time
        assert_eq!(cache.remove_instances(alloc::vec![100, 101], 10000), 1);

        // Only the timestamped one should be removed, exempt one stays
        assert_eq!(cache.instance_ids(), alloc::vec![100]);
    }
}
