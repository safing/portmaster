//! Verdict resets, port of `resetSingleConnectionVerdict`,
//! `resetProfileConnectionVerdict`, `resetAllConnectionVerdicts` and
//! `resetConnectionVerdict`.

use std::io;

use pm_network::{Connection, ConnectionType, SharedConnection};

use super::env::ConnectionTuple;
use super::handler::PacketHandler;

impl PacketHandler {
    /// Re-evaluates one (re-attributed) IP or DNS connection.
    pub fn reset_single_connection_verdict(&self, conn_id: &str) {
        let shared = self
            .network
            .get_connection(conn_id)
            .or_else(|| self.network.get_dns_connection(conn_id));
        match shared {
            Some(shared) => {
                self.reset_connection_verdict(&shared);
            }
            None => tracing::debug!(
                "filter: could not find re-attributed connection {conn_id} for re-evaluation"
            ),
        }
    }

    /// Re-evaluates all complete connections of a profile.
    pub fn reset_profile_connection_verdict(&self, profile_source: &str, profile_id: &str) {
        tracing::info!("filter: re-evaluating connections of {profile_source}/{profile_id}");
        let mut changed = 0;
        for shared in self.network.get_all_connections() {
            let matches = {
                let conn = shared.lock().unwrap_or_else(|e| e.into_inner());
                conn.data_complete
                    && conn.process_context.profile == profile_id
                    && conn.process_context.source == profile_source
            };
            if matches && self.reset_connection_verdict(&shared) {
                changed += 1;
            }
        }
        tracing::info!("filter: changed verdict on {changed} connections");
    }

    /// Re-evaluates all complete connections.
    pub fn reset_all_connection_verdicts(&self) {
        tracing::info!("filter: re-evaluating all connections");
        let mut changed = 0;
        for shared in self.network.get_all_connections() {
            let complete = shared
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .data_complete;
            if complete && self.reset_connection_verdict(&shared) {
                changed += 1;
            }
        }
        tracing::info!("filter: changed verdict on {changed} connections");
    }

    /// Go `interception.ResetVerdictOfAllConnections`: deletes all conntrack
    /// entries with permanent verdict marks, so that every flow is evaluated
    /// again with its next packet.
    pub fn reset_all_marked_conntrack(&self) -> io::Result<usize> {
        let deleted = self.conntrack.delete_all_marked(self.env.ipv6_enabled())?;
        tracing::info!(
            "nfq: deleted {deleted} conntrack entries to reset permanent connection verdicts"
        );
        Ok(deleted)
    }

    /// Returns whether the verdict changed.
    fn reset_connection_verdict(&self, shared: &SharedConnection) -> bool {
        // Remove any active prompt as the settings are being re-evaluated.
        self.env.remove_prompt(shared);

        let mut conn = shared.lock().unwrap_or_else(|e| e.into_inner());

        // Do not re-evaluate connections that have already ended.
        if conn.ended > 0 {
            return false;
        }

        conn.update_features();

        // Skip internal connections: pre-authenticated connections from the
        // Portmaster, redirected DNS requests and the SPN uplink.
        if conn.internal {
            return false;
        }

        tracing::debug!("filter: re-evaluating verdict of {}", *conn);
        let previous = conn.verdict;

        // Apply the privacy filter and check tunneling. A running SPN tunnel
        // is not stopped, as tunnels are not ported.
        self.filter_connection(&mut conn, true, true);

        if conn.verdict == previous {
            tracing::trace!(
                "filter: verdict to connection {} unchanged at {}",
                *conn,
                conn.verdict_verb()
            );
            return false;
        }

        if conn.conn_type == ConnectionType::Ip {
            if let Err(error) = self.delete_conntrack_entry(&conn) {
                tracing::debug!("filter: failed to update connection verdict: {error}");
            }
        }
        self.network.save_locked(shared, &mut conn);
        tracing::info!(
            "filter: verdict of connection {} changed from {} to {}",
            *conn,
            previous.verb(),
            conn.verdict_verb()
        );
        true
    }

    /// Go `UpdateVerdictOfConnection`: deletes the verdict of the connection
    /// so it is initialized again with the next packet.
    fn delete_conntrack_entry(&self, conn: &Connection) -> io::Result<()> {
        let (Some(src), Some(entity)) = (conn.local_ip, conn.entity.as_ref()) else {
            return Err(io::Error::other("connection has no addresses"));
        };
        let Some(dst) = entity.ip else {
            return Err(io::Error::other("connection has no remote IP"));
        };
        self.conntrack.delete_connection(&ConnectionTuple {
            src,
            dst,
            protocol: entity.protocol,
            src_port: conn.local_port,
            dst_port: entity.port,
        })
    }
}
