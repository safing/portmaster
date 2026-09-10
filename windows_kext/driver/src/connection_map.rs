use core::{fmt::Display, time::Duration};

use crate::connection::{is_redirect_port, Connection};
use alloc::{collections::BTreeMap, vec::Vec};
use core::ops::Range;
use smoltcp::wire::{IpAddress, IpProtocol};

#[derive(Clone, Copy, PartialEq, PartialOrd, Eq, Ord)]
pub struct Key {
    pub(crate) protocol: IpProtocol,
    pub(crate) local_address: IpAddress,
    pub(crate) local_port: u16,
    pub(crate) remote_address: IpAddress,
    pub(crate) remote_port: u16,
}

impl Display for Key {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(
            f,
            "p: {} l: {}:{} r: {}:{}",
            self.protocol,
            self.local_address,
            self.local_port,
            self.remote_address,
            self.remote_port
        )
    }
}

impl Key {
    /// Returns the protocol and port as a tuple.
    pub fn small(&self) -> (IpProtocol, u16) {
        (self.protocol, self.local_port)
    }

    /// Returns true if the local address is an IPv4 address.
    pub fn is_ipv6(&self) -> bool {
        match self.local_address {
            IpAddress::Ipv4(_) => false,
            IpAddress::Ipv6(_) => true,
        }
    }

    /// Returns true if the local address is a loopback address.
    pub fn is_loopback(&self) -> bool {
        match self.local_address {
            IpAddress::Ipv4(ip) => ip.is_loopback(),
            IpAddress::Ipv6(ip) => ip.is_loopback(),
        }
    }

    /// Returns a new key with the local and remote addresses and ports reversed.
    #[allow(dead_code)]
    pub fn reverse(&self) -> Key {
        Key {
            protocol: self.protocol,
            local_address: self.remote_address,
            local_port: self.remote_port,
            remote_address: self.local_address,
            remote_port: self.local_port,
        }
    }
}

/// Connections grouped by `(protocol, local port)`.
///
/// Each vector is kept sorted by `Connection::remote_key()`, so a lookup by
/// remote endpoint is a binary search rather than a scan of the whole port. That
/// matters for ports carrying many connections at once - a busy listener, or an
/// inbound flood - where every packet used to walk the entire vector while
/// holding a spin lock at DISPATCH_LEVEL.
///
/// The invariant is maintained by `add` alone. Nothing else inserts, `retain`
/// preserves relative order, and the only field callers mutate through
/// `get_mut` is the verdict, which is not part of the sort key. Should that ever
/// change, lookups would start missing silently.
///
/// The sort key is deliberately *not* unique. Several connections can share a
/// remote endpoint on the same local port: an ended entry still awaiting cleanup
/// in front of its live replacement, or entries that differ only in local
/// address. Lookups therefore resolve the whole run of equal keys and apply the
/// same first-match rules as before, in insertion order.
pub struct ConnectionMap<T: Connection>(BTreeMap<(IpProtocol, u16), Vec<T>>);

/// Returns the range of entries whose remote endpoint equals `target`.
///
/// `partition_point` is used twice instead of `binary_search_by`, because the
/// sort key is not unique: `binary_search_by` returns an arbitrary index inside
/// a run of equal keys, and picking that one entry would reintroduce the bug
/// documented on `end` - a stale ended entry shadowing the live connection
/// behind it. The two bounds give the whole run, in insertion order.
fn equal_range<T: Connection>(connections: &[T], target: (IpAddress, u16)) -> Range<usize> {
    let start = connections.partition_point(|conn| conn.remote_key() < target);
    let end = connections.partition_point(|conn| conn.remote_key() <= target);
    start..end
}

impl<T: Connection + Clone> ConnectionMap<T> {
    pub fn new() -> Self {
        Self(BTreeMap::new())
    }

    pub fn add(&mut self, conn: T) {
        let key = conn.get_key().small();
        if let Some(connections) = self.0.get_mut(&key) {
            // Insert *after* any entries with the same remote endpoint. That keeps
            // the vector in insertion order within a run of equal keys, which the
            // lookups rely on: `end` expects to reach a live connection that was
            // added behind an ended one, and `read` returning the oldest match
            // preserves the previous behaviour of scanning from the front.
            let index = connections.partition_point(|c| c.remote_key() <= conn.remote_key());
            connections.insert(index, conn);
        } else {
            self.0.insert(key, alloc::vec![conn]);
        }
    }

    pub fn get_mut(&mut self, key: &Key) -> Option<&mut T> {
        if let Some(connections) = self.0.get_mut(&key.small()) {
            let range = equal_range(connections, (key.remote_address, key.remote_port));
            for conn in &mut connections[range] {
                if conn.remote_equals(key) {
                    conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                    return Some(conn);
                }
            }
        }

        None
    }

    pub fn read<C>(&self, key: &Key, read_connection: fn(&T) -> Option<C>) -> Option<C> {
        if let Some(connections) = self.0.get(&key.small()) {
            // Exact remote match first, over the run of equal keys only.
            let range = equal_range(connections, (key.remote_address, key.remote_port));
            for conn in &connections[range] {
                if conn.remote_equals(key) {
                    conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                    return read_connection(conn);
                }
            }

            // A redirected connection cannot be found by the search above: it is
            // stored under its real remote endpoint, while the packet that comes
            // back carries the redirect target instead (loopback:53 for a DNS
            // redirect, for example). Those are found by scanning.
            //
            // The scan is guarded by the port test so it does not run on every
            // miss. `redirect_equals` only ever accepts one of the three redirect
            // ports, so for any other remote port the scan cannot match and is
            // skipped - which is what keeps an inbound flood, where every lookup
            // misses, off the O(n) path.
            if is_redirect_port(key.remote_port) {
                for conn in connections {
                    if conn.redirect_equals(key) {
                        conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                        return read_connection(conn);
                    }
                }
            }
        }

        None
    }

    /// Ends the connection matching `key` and returns a copy of it, or `None` if
    /// there is no live match.
    ///
    /// Already-ended entries are skipped rather than ended again. Two layers can
    /// report the same close: `endpoint_closure_*` (this path) and
    /// `ale_resource_monitor` on the resource-release layer, which performs an
    /// endpoint sweep. Without the check the second one to arrive emitted a
    /// duplicate connection-end event for a connection that was already closed -
    /// observed on IPv6, where Windows indicates both.
    ///
    /// The search continues past ended entries instead of stopping at the first
    /// address match. `add` inserts without replacing, and ended entries are only
    /// removed later by `clean_ended_connections`, so a stale closed entry can sit
    /// in front of a live one with the same 5-tuple - returning `None` on the
    /// first match would then leave the live connection open forever. This is why
    /// the whole run of equal remote keys is examined and not just one entry of it.
    pub fn end(&mut self, key: Key) -> Option<T> {
        if let Some(connections) = self.0.get_mut(&key.small()) {
            let range = equal_range(connections, (key.remote_address, key.remote_port));
            for conn in &mut connections[range] {
                if conn.remote_equals(&key) && !conn.has_ended() {
                    conn.end(wdk::utils::get_system_timestamp_ms());
                    return Some(conn.clone());
                }
            }
        }
        return None;
    }

    /// Ends live connections for one local endpoint and returns copies of them.
    ///
    /// The map is grouped by protocol and local port, but that grouping is not a
    /// sufficient identity: two local addresses can listen on the same port, and
    /// multiple processes can share an endpoint with `SO_REUSEADDR`. The optional
    /// address and PID filters are supplied by the ALE resource indication.
    ///
    /// A connection with PID 0 is treated as an unknown owner and is eligible when
    /// a release carries a PID. Otherwise a PID-0 connection would remain stale
    /// forever when it was created before attribution became available. A missing
    /// local address (WFP `FWP_EMPTY` for a wildcard bind) deliberately means
    /// "all local addresses", leaving the PID as the disambiguating field.
    pub fn end_all_on_endpoint(
        &mut self,
        key: (IpProtocol, u16),
        local_address: Option<IpAddress>,
        process_id: Option<u64>,
    ) -> Option<Vec<T>> {
        if let Some(connections) = self.0.get_mut(&key) {
            let mut vec = Vec::with_capacity(connections.len());
            for conn in connections.iter_mut() {
                let address_matches = local_address
                    .map(|address| conn.get_local_address() == address)
                    .unwrap_or(true);
                let process_matches = process_id
                    .map(|pid| conn.get_process_id() == 0 || conn.get_process_id() == pid)
                    .unwrap_or(true);

                if !conn.has_ended() && address_matches && process_matches {
                    conn.end(wdk::utils::get_system_timestamp_ms());
                    vec.push(conn.clone());
                }
            }
            return Some(vec);
        }
        return None;
    }

    pub fn clear(&mut self) {
        self.0.clear();
    }

    pub fn clean_ended_connections(&mut self) {
        let now = wdk::utils::get_system_timestamp_ms();
        const TEN_MINUETS: u64 = Duration::from_secs(60 * 10).as_millis() as u64;
        let before_ten_minutes = now - TEN_MINUETS;
        let before_one_minute = now - Duration::from_secs(60).as_millis() as u64;

        for (_, connections) in self.0.iter_mut() {
            // `retain` preserves the relative order of the entries it keeps, so
            // the sort order the lookups depend on survives the sweep.
            connections.retain(|c| {
                if c.has_ended() && c.get_end_time() < before_one_minute {
                    // Ended more than 1 minute ago
                    return false;
                }

                if c.get_last_accessed_time() < before_ten_minutes {
                    // Last active more than 10 minutes ago
                    return false;
                }

                // Keep
                return true;
            });
        }
        self.0.retain(|_, v| !v.is_empty());
    }

    pub fn get_count(&self) -> usize {
        let mut count = 0;
        for conn in self.0.values() {
            count += conn.len();
        }
        return count;
    }
}
