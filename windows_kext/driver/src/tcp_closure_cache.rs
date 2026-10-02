//! Holds TCP endpoint-closure classifications while already-published packet
//! decisions are completed.

use alloc::{
    collections::{btree_map::Entry, BTreeMap},
    vec::Vec,
};

#[cfg(not(test))]
use wdk::filter_engine::callout_data::ClassifyPend;

#[cfg(test)]
type ClassifyPend = ();

use crate::{connection_map::Key, tcp_endpoint_cache::TcpEndpointConnection};

pub(crate) fn request_matches_tcp_endpoint(
    endpoint_key: &Key,
    endpoint_instance_id: u64,
    request_key: &Key,
    request_instance_id: Option<u64>,
) -> bool {
    request_instance_id == Some(endpoint_instance_id)
        || (endpoint_key.is_loopback_like() && *request_key == endpoint_key.reverse())
}

pub struct PendingTcpClosure {
    pub endpoint: TcpEndpointConnection,
    pub process_id: u64,
    pub classify: ClassifyPend,
    request_ids: Vec<u64>,
}

impl PendingTcpClosure {
    pub fn new(
        endpoint: TcpEndpointConnection,
        process_id: u64,
        classify: ClassifyPend,
        mut request_ids: Vec<u64>,
    ) -> Self {
        request_ids.sort_unstable();
        request_ids.dedup();
        Self {
            endpoint,
            process_id,
            classify,
            request_ids,
        }
    }

    fn add_request(&mut self, request_id: u64) {
        match self.request_ids.binary_search(&request_id) {
            Ok(_) => {}
            Err(index) => self.request_ids.insert(index, request_id),
        }
    }

    fn finish_request(&mut self, request_id: u64) {
        if let Ok(index) = self.request_ids.binary_search(&request_id) {
            self.request_ids.remove(index);
        }
    }

    pub(crate) fn is_ready(&self) -> bool {
        self.request_ids.is_empty()
    }
}

pub struct TcpClosureCache {
    closures: BTreeMap<u64, PendingTcpClosure>,
}

impl TcpClosureCache {
    pub fn new() -> Self {
        Self {
            closures: BTreeMap::new(),
        }
    }

    /// Inserts one closure keyed by its globally unique connection generation.
    /// Returns the supplied value when that generation is already closing.
    pub fn insert(&mut self, closure: PendingTcpClosure) -> Option<PendingTcpClosure> {
        let instance_id = closure.endpoint.instance_id;
        match self.closures.entry(instance_id) {
            Entry::Occupied(_) => Some(closure),
            Entry::Vacant(entry) => {
                entry.insert(closure);
                None
            }
        }
    }

    /// Adds a request published while a matching closure is already pended.
    pub fn add_request(&mut self, request_id: u64, key: &Key, instance_id: Option<u64>) {
        for closure in self.closures.values_mut() {
            let endpoint = closure.endpoint;
            if request_matches_tcp_endpoint(&endpoint.key, endpoint.instance_id, key, instance_id) {
                closure.add_request(request_id);
            }
        }
    }

    /// Resolves one packet request and returns the identities of closures which
    /// are ready to be claimed under the connection-map write lock.
    pub fn finish_request(&mut self, request_id: u64) -> Vec<TcpEndpointConnection> {
        for closure in self.closures.values_mut() {
            closure.finish_request(request_id);
        }

        self.closures
            .values()
            .filter_map(|closure| closure.is_ready().then_some(closure.endpoint))
            .collect()
    }

    /// Removes one ready closure. The caller serializes this with pending-request
    /// publication by holding the matching connection-map write lock.
    pub fn take_ready(&mut self, instance_id: u64) -> Option<PendingTcpClosure> {
        if self
            .closures
            .get(&instance_id)
            .is_some_and(PendingTcpClosure::is_ready)
        {
            self.closures.remove(&instance_id)
        } else {
            None
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        self.closures.len()
    }

    pub fn drain(&mut self) -> Vec<PendingTcpClosure> {
        let closures = core::mem::take(&mut self.closures);
        closures.into_values().collect()
    }
}

#[cfg(test)]
mod tests {
    use super::{PendingTcpClosure, TcpClosureCache};
    use crate::{connection_map::Key, tcp_endpoint_cache::TcpEndpointConnection};
    use alloc::vec;
    use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address};

    fn endpoint(loopback: bool, instance_id: u64) -> TcpEndpointConnection {
        let local_address = if loopback {
            Ipv4Address::new(127, 0, 0, 1)
        } else {
            Ipv4Address::new(10, 0, 0, 1)
        };
        TcpEndpointConnection {
            key: Key {
                protocol: IpProtocol::Tcp,
                local_address: IpAddress::Ipv4(local_address),
                local_port: 40_000,
                remote_address: IpAddress::Ipv4(Ipv4Address::new(192, 0, 2, 1)),
                remote_port: 50_000,
            },
            parent_endpoint_handle: None,
            instance_id,
        }
    }

    #[test]
    fn closure_tracks_snapshot_and_later_direct_requests() {
        let endpoint = endpoint(false, 100);
        let mut cache = TcpClosureCache::new();
        assert!(cache
            .insert(PendingTcpClosure::new(endpoint, 10, (), vec![7, 9, 7]))
            .is_none());

        let unrelated_key = endpoint.key.reverse();
        cache.add_request(8, &unrelated_key, Some(100));
        assert!(cache.finish_request(7).is_empty());
        assert!(cache.finish_request(9).is_empty());
        assert!(cache.finish_request(8) == vec![endpoint]);
        assert!(cache.take_ready(100).unwrap().endpoint == endpoint);
    }

    #[test]
    fn duplicate_insert_returns_unstored_closure() {
        let endpoint = endpoint(false, 100);
        let mut cache = TcpClosureCache::new();
        assert!(cache
            .insert(PendingTcpClosure::new(endpoint, 10, (), vec![7]))
            .is_none());

        let duplicate = cache
            .insert(PendingTcpClosure::new(endpoint, 20, (), vec![8]))
            .expect("duplicate closure");

        assert_eq!(duplicate.process_id, 20);
        assert_eq!(cache.get_entries_count(), 1);
        assert!(cache.finish_request(8).is_empty());
        assert!(cache.finish_request(7) == vec![endpoint]);
    }

    #[test]
    fn loopback_closure_tracks_reverse_tuple_requests() {
        let mut endpoint = endpoint(true, 100);
        endpoint.key.remote_address = endpoint.key.local_address;
        let mut cache = TcpClosureCache::new();
        assert!(cache
            .insert(PendingTcpClosure::new(endpoint, 10, (), vec![7]))
            .is_none());

        cache.add_request(8, &endpoint.key.reverse(), Some(200));
        assert!(cache.finish_request(7).is_empty());
        assert!(cache.finish_request(8) == vec![endpoint]);
    }

    #[test]
    fn same_local_address_closure_tracks_reverse_tuple_requests() {
        let mut endpoint = endpoint(false, 100);
        endpoint.key.remote_address = endpoint.key.local_address;
        let mut cache = TcpClosureCache::new();
        assert!(cache
            .insert(PendingTcpClosure::new(endpoint, 10, (), vec![7]))
            .is_none());

        cache.add_request(8, &endpoint.key.reverse(), Some(200));
        assert!(cache.finish_request(7).is_empty());
        assert!(cache.finish_request(8) == vec![endpoint]);
    }

    #[test]
    fn non_loopback_closure_ignores_reverse_tuple_requests() {
        let endpoint = endpoint(false, 100);
        let mut cache = TcpClosureCache::new();
        assert!(cache
            .insert(PendingTcpClosure::new(endpoint, 10, (), vec![7]))
            .is_none());

        cache.add_request(8, &endpoint.key.reverse(), Some(200));
        assert!(cache.finish_request(7) == vec![endpoint]);
        assert!(cache.take_ready(100).is_some());
    }
}
