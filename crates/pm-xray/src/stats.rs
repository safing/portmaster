//! Optional traffic statistics adapter.
//!
//! Xray exposes a gRPC `StatsService`; a real client would need a gRPC/HTTP2
//! stack (`tonic`/`h2`), which is intentionally not added to this GPL-3.0
//! workspace for an optional feature. The adapter is kept explicit so a
//! follow-up can implement it without touching the rest of the crate.

/// Traffic counters of one outbound, in bytes.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct TrafficSnapshot {
    pub uplink: u64,
    pub downlink: u64,
}

/// Statistics source used by the status view.
pub trait TrafficStats: Send + Sync {
    fn query(&self, tag: &str) -> std::result::Result<TrafficSnapshot, String>;
}

/// Placeholder implementation: no gRPC client is compiled in yet.
#[derive(Debug, Clone, Copy, Default)]
pub struct NoopTrafficStats;

impl TrafficStats for NoopTrafficStats {
    fn query(&self, _tag: &str) -> std::result::Result<TrafficSnapshot, String> {
        Ok(TrafficSnapshot::default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn noop_stats_are_zero() {
        let snapshot = NoopTrafficStats.query("proxy").unwrap();
        assert_eq!(snapshot, TrafficSnapshot::default());
    }
}
