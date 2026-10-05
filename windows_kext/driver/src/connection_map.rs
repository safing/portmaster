use core::{fmt::Display, time::Duration};

use crate::connection::Connection;
use alloc::{collections::BTreeMap, vec::Vec};
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

pub struct ConnectionMap<T: Connection>(BTreeMap<(IpProtocol, u16), Vec<T>>);

impl<T: Connection + Clone> ConnectionMap<T> {
    pub fn new() -> Self {
        Self(BTreeMap::new())
    }

    pub fn add(&mut self, conn: T) {
        let key = conn.get_key().small();
        if let Some(connections) = self.0.get_mut(&key) {
            connections.push(conn);
        } else {
            self.0.insert(key, alloc::vec![conn]);
        }
    }

    pub fn get_mut(&mut self, key: &Key) -> Option<&mut T> {
        if let Some(connections) = self.0.get_mut(&key.small()) {
            for conn in connections {
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
            for conn in connections {
                if conn.remote_equals(key) {
                    conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                    return read_connection(conn);
                }
                if conn.redirect_equals(key) {
                    conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                    return read_connection(conn);
                }
            }
        }

        None
    }

    /// Like `read()`, but for ALE authorization requests, which know which
    /// process is asking (`process_id`).
    ///
    /// An authorization request always comes from a live socket, so a cached
    /// entry may only answer it if the entry can still describe that socket.
    /// Two kinds of entries cannot; they are removed and the lookup reports a
    /// miss, which makes the caller request a fresh verdict from Portmaster:
    ///
    /// * An ended entry: its socket is already closed, so the request comes
    ///   from a new socket reusing the same local port and remote endpoint.
    ///   It must not inherit the old verdict or the old owner's process id.
    /// * An entry without a process id: it was created by the packet layer,
    ///   which cannot know the owner. Once a request with a known process id
    ///   arrives, the entry is replaced so the flow gets a verdict tied to
    ///   the real owner.
    ///
    /// Every other matching entry is returned, even one owned by a different
    /// process: live processes can share a 5-tuple (multicast senders on a
    /// port opened with SO_REUSEADDR). If such entries evicted each other,
    /// every completed verdict would re-authorize the flow, evict the other
    /// owner's entry and request yet another verdict - an endless loop.
    pub fn read_for_process<C>(
        &mut self,
        key: &Key,
        process_id: u64,
        read_connection: fn(&T) -> Option<C>,
    ) -> Option<C> {
        let connections = self.0.get_mut(&key.small())?;
        let mut index = 0;
        while index < connections.len() {
            let conn = &connections[index];
            if conn.remote_equals(key) {
                // Stale entry: the socket it describes is closed (ended), or
                // its owner is unknown while the requester's is known. Drop
                // it and report a miss so the flow gets a fresh verdict. A
                // live entry of another process is NOT stale, see above.
                if conn.has_ended() || (conn.get_process_id() == 0 && process_id != 0) {
                    connections.remove(index);
                    // Re-check the entry that shifted into this slot.
                    continue;
                }
                conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                return read_connection(conn);
            }
            if conn.redirect_equals(key) {
                conn.set_last_accessed_time(wdk::utils::get_system_timestamp_ms());
                return read_connection(conn);
            }
            index += 1;
        }

        None
    }

    pub fn end(&mut self, key: Key) -> Option<T> {
        if let Some(connections) = self.0.get_mut(&key.small()) {
            for conn in connections.iter_mut() {
                if conn.remote_equals(&key) {
                    conn.end(wdk::utils::get_system_timestamp_ms());
                    return Some(conn.clone());
                }
            }
        }
        return None;
    }

    pub fn end_all_on_port(&mut self, key: (IpProtocol, u16)) -> Option<Vec<T>> {
        if let Some(connections) = self.0.get_mut(&key) {
            let mut vec = Vec::with_capacity(connections.len());
            for conn in connections.iter_mut() {
                if !conn.has_ended() {
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
