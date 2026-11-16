//! Background jobs of the netquery module (Go `module_api.go`): the live
//! database cleaner, the history retention cleaner and the shutdown steps.
//!
//! Each loop runs until its token is cancelled; the core spawns them.

use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pm_network::DELETE_CONNS_AFTER_ENDED_THRESHOLD;
use tokio_util::sync::CancellationToken;

use crate::Database;

/// Go: the live cleaner runs every 10 seconds, the first time after 10 seconds.
pub const LIVE_CLEANER_INTERVAL: Duration = Duration::from_secs(10);
/// Go: `Delay(10 minutes).Repeat(1 hour)`.
pub const HISTORY_CLEANER_DELAY: Duration = Duration::from_secs(10 * 60);
pub const HISTORY_CLEANER_INTERVAL: Duration = Duration::from_secs(60 * 60);

/// The profile data the history retention needs.
pub trait HistoryRetention: Send + Sync {
    /// `history/keep` days of the local profile with this ID (without the
    /// `local/` prefix), or `None` if the profile cannot be loaded.
    fn local_profile_keep_days(&self, profile_id: &str) -> Option<i64>;
    /// The global `history/keep` setting.
    fn global_keep_days(&self) -> i64;
}

/// Retention days of a stored profile ID (`local/<id>`): the profile's own
/// setting, or the global one if the profile cannot be loaded. 0 keeps
/// everything.
pub fn retention_days(lookup: &dyn HistoryRetention, stored_profile: &str) -> i64 {
    let id = stored_profile.strip_prefix("local/").unwrap_or(stored_profile);
    lookup
        .local_profile_keep_days(id)
        .unwrap_or_else(|| lookup.global_keep_days())
}

fn unix_now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| i64::try_from(d.as_secs()).unwrap_or(i64::MAX))
}

/// One live cleaner pass: removes connections that ended more than
/// [`DELETE_CONNS_AFTER_ENDED_THRESHOLD`] ago.
pub fn cleanup_live_once(db: &Database, now: i64) {
    let threshold = now - DELETE_CONNS_AFTER_ENDED_THRESHOLD;
    match db.cleanup(threshold) {
        Ok(count) => tracing::trace!(count, threshold, "netquery: removed old connections from live db"),
        Err(error) => tracing::error!(%error, "netquery: failed to remove old connections from live db"),
    }
}

/// One history cleanup pass (Go `CleanupHistory`). Errors are logged.
pub fn cleanup_history_once(db: &Database, now: i64, lookup: &dyn HistoryRetention) {
    match db.cleanup_history(now, |profile| retention_days(lookup, profile)) {
        Ok(errors) => {
            for error in errors {
                tracing::warn!(%error, "netquery: failed to clean up history");
            }
        }
        Err(error) => tracing::error!(%error, "netquery: failed to clean up history"),
    }
}

/// Runs the live cleaner every [`LIVE_CLEANER_INTERVAL`] until cancelled.
pub async fn run_live_cleaner(db: Arc<Database>, cancel: CancellationToken) {
    run_live_cleaner_every(db, LIVE_CLEANER_INTERVAL, cancel).await;
}

/// [`run_live_cleaner`] with a custom interval.
pub async fn run_live_cleaner_every(db: Arc<Database>, interval: Duration, cancel: CancellationToken) {
    loop {
        tokio::select! {
            () = cancel.cancelled() => return,
            () = tokio::time::sleep(interval) => cleanup_live_once(&db, unix_now()),
        }
    }
}

/// Runs the history cleaner after [`HISTORY_CLEANER_DELAY`], then every
/// [`HISTORY_CLEANER_INTERVAL`], until cancelled.
pub async fn run_history_cleaner(
    db: Arc<Database>,
    lookup: Arc<dyn HistoryRetention>,
    cancel: CancellationToken,
) {
    run_history_cleaner_every(db, lookup, HISTORY_CLEANER_DELAY, HISTORY_CLEANER_INTERVAL, cancel).await;
}

/// [`run_history_cleaner`] with custom timing.
pub async fn run_history_cleaner_every(
    db: Arc<Database>,
    lookup: Arc<dyn HistoryRetention>,
    delay: Duration,
    interval: Duration,
    cancel: CancellationToken,
) {
    let mut wait = delay;
    loop {
        tokio::select! {
            () = cancel.cancelled() => return,
            () = tokio::time::sleep(wait) => cleanup_history_once(&db, unix_now(), lookup.as_ref()),
        }
        wait = interval;
    }
}

/// Go `Stop`: after the workers have ended, mark connections that are still
/// active in the history as ended now, then vacuum the history file. Go does
/// this on shutdown only, not at start.
pub fn shutdown(db: &Database) {
    if let Err(error) = db.mark_all_history_connections_ended(unix_now()) {
        tracing::error!(%error, "netquery: failed to mark connections in history database as ended");
    }
    if let Err(error) = db.vacuum_history() {
        tracing::error!(%error, "netquery: failed to execute VACUUM in history database");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Fixed {
        local: Option<i64>,
        global: i64,
    }

    impl HistoryRetention for Fixed {
        fn local_profile_keep_days(&self, profile_id: &str) -> Option<i64> {
            assert!(!profile_id.starts_with("local/"));
            self.local
        }
        fn global_keep_days(&self) -> i64 {
            self.global
        }
    }

    #[test]
    fn retention_prefers_profile_and_falls_back_to_global() {
        let with = Fixed { local: Some(7), global: 30 };
        assert_eq!(retention_days(&with, "local/abc"), 7);
        let without = Fixed { local: None, global: 30 };
        assert_eq!(retention_days(&without, "local/abc"), 30);
        assert_eq!(retention_days(&without, "special/system"), 30);
        let forever = Fixed { local: Some(0), global: 30 };
        assert_eq!(retention_days(&forever, "local/abc"), 0);
    }
}
