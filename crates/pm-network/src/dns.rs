//! DNS request bookkeeping, a port of Go `network/dns.go` (without the DNS
//! message crafting, which belongs to the nameserver).

use std::sync::Arc;

use pm_netstate::UNDEFINED_PROCESS_ID;
use pm_process::SYSTEM_PROCESS_ID;
use pm_process::UNIDENTIFIED_PROCESS_ID;

use crate::connection::{Connection, SharedConnection};
use crate::network::{Network, DNS_REQUEST_CONN_GRACE};
use crate::types::{format_ip, PacketInfo};

/// DNS record types whose requests are merged into the resulting connection.
pub const DNS_TYPE_A: u16 = 1;
pub const DNS_TYPE_AAAA: u16 = 28;
pub const DNS_TYPE_SVCB: u16 = 64;
pub const DNS_TYPE_HTTPS: u16 = 65;

const SUPPORTED_DOMAIN_TO_IP_RECORD_TYPES: [u16; 4] =
    [DNS_TYPE_A, DNS_TYPE_AAAA, DNS_TYPE_SVCB, DNS_TYPE_HTTPS];

/// Seconds after which a DNS request without a following connection is
/// written.
pub const OPEN_DNS_REQUEST_LIMIT: i64 = 3;

/// The parts of a query and its answer that decide whether a request is saved.
#[derive(Debug, Clone, Copy)]
pub struct OpenDnsQuery {
    pub qtype: u16,
    pub served_from_cache: bool,
    pub requesting_new: bool,
}

/// Returns whether requests for the record type are merged into connections.
pub fn is_supported_dns_record_type(rr_type: u16) -> bool {
    SUPPORTED_DOMAIN_TO_IP_RECORD_TYPES.contains(&rr_type)
}

fn type_name(qtype: u16) -> &'static str {
    match qtype {
        DNS_TYPE_A => "A",
        DNS_TYPE_AAAA => "AAAA",
        DNS_TYPE_SVCB => "SVCB",
        DNS_TYPE_HTTPS => "HTTPS",
        _ => "",
    }
}

fn request_cache_key(pid: i32, fqdn: &str, qtype: u16) -> String {
    format!("{pid}/{fqdn}{}", type_name(qtype))
}

/// Returns the key of a DNS request connection: `<protocol>-<ip>-<port>`.
fn dns_request_connection_key(info: &PacketInfo) -> Option<String> {
    // Only protocols with ports are supported.
    if info.src_port == 0 {
        return None;
    }
    Some(format!(
        "{}-{}-{}",
        info.protocol,
        format_ip(info.src),
        info.src_port
    ))
}

impl Network {
    /// Saves a DNS request connection for fast attribution of later requests.
    ///
    /// Locking rule: the plain variants lock the connection themselves and must
    /// not be called with the lock held; callers that hold the lock (as the Go
    /// firewall and nameserver do) use the `_locked` variants. The internal map
    /// locks are never held while a connection is locked.
    pub fn save_dns_request_connection(&self, conn: &SharedConnection, info: &PacketInfo) {
        let guard = conn.lock().expect("lock poisoned");
        self.save_dns_request_connection_locked(conn, &guard, info);
    }

    /// Like [`Network::save_dns_request_connection`] for a locked connection.
    pub fn save_dns_request_connection_locked(
        &self,
        shared: &SharedConnection,
        conn: &Connection,
        info: &PacketInfo,
    ) {
        // Re-injected packets on Windows are reported with the kernel PID.
        if conn.pid == UNDEFINED_PROCESS_ID || conn.pid == SYSTEM_PROCESS_ID {
            return;
        }
        let Some(key) = dns_request_connection_key(info) else {
            return;
        };
        self.dns_request_conns
            .lock()
            .expect("lock poisoned")
            .insert(key, Arc::clone(shared));
    }

    /// Returns a saved DNS request connection.
    pub fn get_dns_request_connection(&self, info: &PacketInfo) -> Option<SharedConnection> {
        let key = dns_request_connection_key(info)?;
        self.dns_request_conns
            .lock()
            .expect("lock poisoned")
            .get(&key)
            .cloned()
    }

    /// Deletes old DNS request connections.
    pub(crate) fn clean_dns_request_connections(&self) {
        let delete_older_than = self.env.clock.now() - DNS_REQUEST_CONN_GRACE;
        // Do not hold the map lock while locking connections.
        let snapshot: Vec<(String, SharedConnection)> = self
            .dns_request_conns
            .lock()
            .expect("lock poisoned")
            .iter()
            .map(|(key, conn)| (key.clone(), Arc::clone(conn)))
            .collect();
        for (key, shared) in snapshot {
            let ended = shared.lock().expect("lock poisoned").ended;
            if ended > 0 && ended < delete_older_than {
                let mut map = self.dns_request_conns.lock().expect("lock poisoned");
                if map.get(&key).is_some_and(|c| Arc::ptr_eq(c, &shared)) {
                    map.remove(&key);
                }
            }
        }
    }

    /// Removes the open requests for the domain, also those without a process.
    pub fn remove_open_dns_request(&self, pid: i32, fqdn: &str) {
        let mut open = self.open_dns_requests.lock().expect("lock poisoned");
        for qtype in SUPPORTED_DOMAIN_TO_IP_RECORD_TYPES {
            open.remove(&request_cache_key(pid, fqdn, qtype));
        }
        // If the process is known, also check for non-attributed requests.
        if pid != UNIDENTIFIED_PROCESS_ID {
            for qtype in SUPPORTED_DOMAIN_TO_IP_RECORD_TYPES {
                open.remove(&request_cache_key(UNIDENTIFIED_PROCESS_ID, fqdn, qtype));
            }
        }
    }

    /// Saves a DNS request connection that was allowed to proceed. Without a
    /// following connection it is written by
    /// [`Network::write_open_dns_requests`]. The connection must not be locked
    /// by the caller; see [`Network::save_open_dns_request_locked`].
    pub fn save_open_dns_request(&self, query: Option<&OpenDnsQuery>, conn: &SharedConnection) {
        let mut guard = conn.lock().expect("lock poisoned");
        self.save_open_dns_request_locked(query, conn, &mut guard);
    }

    /// Like [`Network::save_open_dns_request`] for a locked connection.
    pub fn save_open_dns_request_locked(
        &self,
        query: Option<&OpenDnsQuery>,
        shared: &SharedConnection,
        conn: &mut Connection,
    ) {
        // Only save requests that actually went out (or triggered an async
        // resolve) to reduce clutter.
        let Some(query) = query else {
            return;
        };
        if query.served_from_cache && !query.requesting_new {
            return;
        }

        // Unsupported requests are saved immediately.
        if !is_supported_dns_record_type(query.qtype) {
            self.save_locked(shared, conn);
            return;
        }

        // Duplicates in quick succession are not worth keeping, so an existing
        // open request is replaced.
        let domain = conn
            .entity
            .as_ref()
            .map(|e| e.domain.as_str())
            .unwrap_or("");
        let key = request_cache_key(conn.process_pid(), domain, query.qtype);
        self.open_dns_requests
            .lock()
            .expect("lock poisoned")
            .insert(key, Arc::clone(shared));
    }

    /// Writes open DNS requests that were not followed by a connection.
    pub fn write_open_dns_requests(&self) {
        let threshold = self.env.clock.now() - OPEN_DNS_REQUEST_LIMIT;
        // Do not hold the map lock while locking connections: gathering holds
        // a connection lock and takes the map lock. Entries are removed under
        // the map lock first and only the removed ones are saved.
        let snapshot: Vec<(String, SharedConnection)> = self
            .open_dns_requests
            .lock()
            .expect("lock poisoned")
            .iter()
            .map(|(key, conn)| (key.clone(), Arc::clone(conn)))
            .collect();

        for (key, shared) in snapshot {
            let mut conn = shared.lock().expect("lock poisoned");
            if conn.ended >= threshold {
                continue;
            }
            let removed = {
                let mut open = self.open_dns_requests.lock().expect("lock poisoned");
                if open.get(&key).is_some_and(|c| Arc::ptr_eq(c, &shared)) {
                    open.remove(&key);
                    true
                } else {
                    false
                }
            };
            if removed {
                self.save_locked(&shared, &mut conn);
            }
        }
    }

    /// Returns the number of open DNS requests.
    pub fn open_dns_request_count(&self) -> usize {
        self.open_dns_requests.lock().expect("lock poisoned").len()
    }
}
