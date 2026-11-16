//! Port of Go `updateBandwidth` and `packet.BandwidthUpdate`.

use std::sync::TryLockError;

use super::handler::PacketHandler;

/// How the byte counts of an update are interpreted.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BandwidthMethod {
    /// The counts are the totals since the connection started.
    Absolute,
    /// The counts are added to the totals.
    Additive,
}

/// An update to the seen bandwidth of a connection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BandwidthUpdate {
    pub conn_id: String,
    pub bytes_received: u64,
    pub bytes_sent: u64,
    pub method: BandwidthMethod,
}

impl PacketHandler {
    /// Applies a bandwidth update to the connection and forwards the delta to
    /// the sink. Connections that are locked are skipped, like in Go.
    pub fn update_bandwidth(&self, update: &BandwidthUpdate) {
        if update.bytes_received == 0 && update.bytes_sent == 0 {
            return;
        }
        let Some(shared) = self.network.get_connection(&update.conn_id) else {
            return;
        };
        let mut conn = match shared.try_lock() {
            Ok(conn) => conn,
            Err(TryLockError::Poisoned(poisoned)) => poisoned.into_inner(),
            Err(TryLockError::WouldBlock) => return,
        };

        let (bytes_in, bytes_out) = match update.method {
            BandwidthMethod::Absolute => {
                // Go subtracts unsigned and would wrap; never go below zero.
                let deltas = (
                    update.bytes_received.saturating_sub(conn.bytes_received),
                    update.bytes_sent.saturating_sub(conn.bytes_sent),
                );
                conn.bytes_received = update.bytes_received;
                conn.bytes_sent = update.bytes_sent;
                deltas
            }
            BandwidthMethod::Additive => {
                conn.bytes_received = conn.bytes_received.saturating_add(update.bytes_received);
                conn.bytes_sent = conn.bytes_sent.saturating_add(update.bytes_sent);
                (update.bytes_received, update.bytes_sent)
            }
        };

        let (Some(sink), true) = (&self.bandwidth, conn.bandwidth_enabled) else {
            return;
        };
        let Some(process) = &conn.process else {
            return;
        };
        let profile = format!(
            "{}/{}",
            conn.process_context.source, conn.process_context.profile
        );
        if let Err(error) = sink.update_bandwidth(
            conn.history_enabled,
            &profile,
            process.key(),
            &conn.id,
            bytes_in,
            bytes_out,
        ) {
            tracing::error!("filter: failed to persist bandwidth data: {error}");
        }
    }
}
