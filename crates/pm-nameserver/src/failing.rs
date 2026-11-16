//! Port of Go `failing.go`: queries that failed recently are answered with
//! the previous error for some time, to protect against retry floods.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

/// Delay of replies to failing queries.
pub const FAILING_DELAY: Duration = Duration::from_millis(900);
const FAILING_BASE_DURATION: Duration = Duration::from_millis(900);
const FAILING_FACTOR_DURATION: Duration = Duration::from_millis(500);
const FAILING_MAX_DURATION: Duration = Duration::from_secs(30);
const FAILING_KEEP_ADDED_DURATION: Duration = Duration::from_secs(10);

#[derive(Debug, Clone)]
struct FailingQuery {
    /// Until when the query is regarded as failing.
    until: Instant,
    /// Until when the failing status is kept.
    keep: Instant,
    /// How often the query failed.
    times: u32,
    /// The error the query failed with.
    err: String,
}

/// A recently failed query.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FailingInfo {
    pub until: Instant,
    pub err: String,
}

/// The registry of failing queries, keyed by query ID.
#[derive(Debug, Default)]
pub struct FailingQueries {
    queries: Mutex<HashMap<String, FailingQuery>>,
}

impl FailingQueries {
    pub fn new() -> FailingQueries {
        FailingQueries::default()
    }

    /// Returns whether the query is currently failing. If the network changed,
    /// all failing queries are forgotten.
    pub fn check(&self, id: &str, network_changed: bool, now: Instant) -> Option<FailingInfo> {
        let mut queries = self.queries.lock().expect("lock poisoned");
        if network_changed {
            queries.clear();
            return None;
        }

        let failing = queries.get(id)?;
        // The failing status may have run out already.
        if now > failing.until {
            return None;
        }
        Some(FailingInfo {
            until: failing.until,
            err: failing.err.clone(),
        })
    }

    /// Registers a failure of the query.
    pub fn add(&self, id: &str, err: &str, now: Instant) {
        let mut queries = self.queries.lock().expect("lock poisoned");
        let failing = queries.entry(id.to_string()).or_insert_with(|| FailingQuery {
            until: now,
            keep: now,
            times: 0,
            err: err.to_string(),
        });

        // The initial duration of 900ms is perfect for a normal retry after
        // 1s, but not any earlier.
        let duration = (FAILING_BASE_DURATION + FAILING_FACTOR_DURATION * failing.times)
            .min(FAILING_MAX_DURATION);

        failing.times += 1;
        failing.until = now + duration;
        failing.keep = failing.until + FAILING_KEEP_ADDED_DURATION;
    }

    /// Removes up to `max_remove` expired entries, giving up after
    /// `max_miss` entries that are still kept.
    pub fn clean(&self, mut max_remove: usize, mut max_miss: usize, now: Instant) {
        let mut queries = self.queries.lock().expect("lock poisoned");
        let mut remove = Vec::new();
        for (key, failing) in queries.iter() {
            if now > failing.keep {
                remove.push(key.clone());
                max_remove = max_remove.saturating_sub(1);
                if max_remove == 0 {
                    break;
                }
            } else {
                max_miss = max_miss.saturating_sub(1);
                if max_miss == 0 {
                    break;
                }
            }
        }
        for key in remove {
            queries.remove(&key);
        }
    }

    pub fn len(&self) -> usize {
        self.queries.lock().expect("lock poisoned").len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn failure_duration_grows_and_expires() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        assert!(failing.check("a", false, t0).is_none());

        failing.add("a", "boom", t0);
        let info = failing.check("a", false, t0 + Duration::from_millis(100)).unwrap();
        assert_eq!(info.err, "boom");
        assert_eq!(info.until, t0 + Duration::from_millis(900));
        // After the failing duration the query is tried again.
        assert!(failing.check("a", false, t0 + Duration::from_millis(901)).is_none());

        // Second failure: 900ms + 500ms.
        let t1 = t0 + Duration::from_secs(1);
        failing.add("a", "boom", t1);
        let info = failing.check("a", false, t1).unwrap();
        assert_eq!(info.until, t1 + Duration::from_millis(1400));
    }

    #[test]
    fn duration_is_capped_at_30_seconds() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        for _ in 0..100 {
            failing.add("a", "boom", t0);
        }
        let info = failing.check("a", false, t0).unwrap();
        assert_eq!(info.until, t0 + Duration::from_secs(30));
    }

    #[test]
    fn first_error_is_kept() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        failing.add("a", "first", t0);
        failing.add("a", "second", t0);
        assert_eq!(failing.check("a", false, t0).unwrap().err, "first");
    }

    #[test]
    fn network_change_resets_everything() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        failing.add("a", "boom", t0);
        failing.add("b", "boom", t0);
        assert!(failing.check("a", true, t0).is_none());
        assert!(failing.is_empty());
    }

    #[test]
    fn clean_removes_only_expired_entries() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        failing.add("old", "boom", t0);
        failing.add("new", "boom", t0 + Duration::from_secs(60));
        // "old" is kept until 900ms + 10s.
        failing.clean(10, 3, t0 + Duration::from_secs(11));
        assert_eq!(failing.len(), 1);
        assert!(failing
            .check("new", false, t0 + Duration::from_secs(60))
            .is_some());
    }

    #[test]
    fn clean_respects_its_limits() {
        let failing = FailingQueries::new();
        let t0 = Instant::now();
        for i in 0..10 {
            failing.add(&format!("q{i}"), "boom", t0);
        }
        failing.clean(2, 3, t0 + Duration::from_secs(60));
        assert_eq!(failing.len(), 8);
    }
}
