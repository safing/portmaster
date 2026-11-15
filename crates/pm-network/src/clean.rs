//! Cleaning of connections, a port of Go `network/clean.go`.
//!
//! The periodic worker (every [`CLEANER_TICK`]) and the cleanup of the
//! process storage with the returned PIDs are driven by the caller.

use std::collections::HashSet;
use std::time::{Duration, UNIX_EPOCH};

use pm_netstate::UNDEFINED_PROCESS_ID;

use crate::network::Network;
use crate::types::{protocol, PacketInfo};

/// Time after which connections of unsupported protocols are marked as ended.
pub const END_CONNS_AFTER_INACTIVE_FOR: i64 = 5 * 60;
/// Time after which ICMP "connections" are marked as ended.
pub const END_ICMP_CONNS_AFTER_INACTIVE_FOR: i64 = 60;
/// Time after which ended connections are removed.
pub const DELETE_CONNS_AFTER_ENDED_THRESHOLD: i64 = 10 * 60;
/// Time after which incomplete connections are removed.
pub const DELETE_INCOMPLETE_CONNS_AFTER_STARTED_THRESHOLD: i64 = 60;
/// Interval of the cleaner.
pub const CLEANER_TICK: Duration = Duration::from_secs(5);

impl Network {
    /// Ends and deletes old connections and returns the PIDs of processes that
    /// have active connections.
    pub fn clean_connections(&self) -> HashSet<i32> {
        let mut active_pids = HashSet::new();

        let now = self.env.clock.now();
        let now_time = UNIX_EPOCH + Duration::from_secs(now.max(0) as u64);
        let ignore_newer = now - 2;
        let end_not_seen_since = now - END_CONNS_AFTER_INACTIVE_FOR;
        let end_icmp_not_seen_since = now - END_ICMP_CONNS_AFTER_INACTIVE_FOR;
        let delete_older_than = now - DELETE_CONNS_AFTER_ENDED_THRESHOLD;
        let delete_incomplete_older_than = now - DELETE_INCOMPLETE_CONNS_AFTER_STARTED_THRESHOLD;

        for shared in self.conns.list() {
            let mut conn = shared.lock().expect("lock poisoned");

            if conn.started >= ignore_newer {
                // Skip very fresh connections to evade edge cases.
            } else if !conn.data_complete {
                // Step 0: delete old incomplete connections.
                if conn.started < delete_incomplete_older_than {
                    self.delete_locked(&mut conn);
                }
            } else if conn.ended == 0 {
                // Step 1: check if still active.
                let active = match conn.ip_protocol {
                    protocol::TCP | protocol::UDP => {
                        let (remote_ip, remote_port) = conn
                            .entity
                            .as_ref()
                            .map_or((None, 0), |entity| (entity.ip, entity.port));
                        let info = PacketInfo {
                            inbound: false, // src == local
                            in_tunnel: false,
                            version: conn.ip_version,
                            protocol: conn.ip_protocol,
                            src_port: conn.local_port,
                            dst_port: remote_port,
                            src: conn.local_ip,
                            dst: remote_ip,
                            pid: UNDEFINED_PROCESS_ID,
                            // State tables are updated if older than this.
                            seen_at: UNIX_EPOCH + Duration::from_secs(conn.started.max(0) as u64),
                        };
                        let active = self.env.processes.exists(&info, now_time);
                        // Update last seen for permanent verdict connections.
                        if active && conn.verdict_permanent {
                            conn.last_seen = now;
                        }
                        active
                    }
                    protocol::ICMP | protocol::ICMPV6 => conn.last_seen > end_icmp_not_seen_since,
                    _ => conn.last_seen > end_not_seen_since,
                };

                // Step 2: mark as ended.
                if !active {
                    conn.ended = now;
                    self.save_locked(&shared, &mut conn);
                }

                if let Some(process) = &conn.process {
                    active_pids.insert(process.pid);
                }
            } else if conn.ended < delete_older_than {
                // Step 3: delete.
                self.delete_locked(&mut conn);
            }
        }

        for shared in self.dns_conns.list() {
            let mut conn = shared.lock().expect("lock poisoned");
            if conn.ended < delete_older_than {
                self.delete_locked(&mut conn);
            }
        }

        self.clean_dns_request_connections();

        active_pids
    }
}
