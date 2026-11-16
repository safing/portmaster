//! Port of Go `pidhint.go`: PIDs reported by info-only packets, kept in two
//! generations so that expiry is a swap instead of a scan.

use std::collections::HashMap;
use std::sync::{Mutex, PoisonError};
use std::time::{Duration, Instant};

/// Interval in which the hints are rotated (Go `pidHintGeneration`).
pub(super) const GENERATION: Duration = Duration::from_secs(1);
/// Entries of the current generation before an early rotation
/// (Go `pidHintMaxEntries`).
const MAX_ENTRIES: usize = 20000;

struct Hints {
    cur: HashMap<String, i32>,
    prev: HashMap<String, i32>,
    rotated: Instant,
}

pub(super) struct PidHints(Mutex<Hints>);

impl PidHints {
    pub(super) fn new() -> PidHints {
        PidHints(Mutex::new(Hints {
            cur: HashMap::new(),
            prev: HashMap::new(),
            rotated: Instant::now(),
        }))
    }

    fn guard(&self) -> std::sync::MutexGuard<'_, Hints> {
        self.0.lock().unwrap_or_else(PoisonError::into_inner)
    }

    /// Rotates if a generation has passed (Go runs a ticker for this).
    pub(super) fn rotate_if_due(&self, now: Instant) {
        let mut hints = self.guard();
        if now.duration_since(hints.rotated) >= GENERATION {
            rotate(&mut hints, now);
        }
    }

    pub(super) fn save(&self, id: &str, pid: i32) {
        let mut hints = self.guard();
        if hints.cur.len() >= MAX_ENTRIES {
            let now = Instant::now();
            rotate(&mut hints, now);
        }
        hints.cur.insert(id.to_string(), pid);
    }

    /// Returns and removes the hint of a connection.
    pub(super) fn take(&self, id: &str) -> Option<i32> {
        let mut hints = self.guard();
        if let Some(pid) = hints.cur.remove(id) {
            // An older hint may have survived a rotation; drop it too.
            hints.prev.remove(id);
            return Some(pid);
        }
        hints.prev.remove(id)
    }

    #[cfg(test)]
    fn force_rotate(&self) {
        rotate(&mut self.guard(), Instant::now());
    }
}

fn rotate(hints: &mut Hints, now: Instant) {
    hints.prev = std::mem::take(&mut hints.cur);
    hints.rotated = now;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hints_survive_one_rotation_and_are_taken_once() {
        let hints = PidHints::new();
        hints.save("a", 1);
        hints.force_rotate();
        hints.save("b", 2);
        assert_eq!(hints.take("a"), Some(1));
        assert_eq!(hints.take("a"), None);
        hints.force_rotate();
        assert_eq!(hints.take("b"), Some(2));
    }

    #[test]
    fn hints_expire_after_two_rotations() {
        let hints = PidHints::new();
        hints.save("a", 1);
        hints.force_rotate();
        hints.force_rotate();
        assert_eq!(hints.take("a"), None);
    }

    #[test]
    fn current_hint_drops_the_stale_one() {
        let hints = PidHints::new();
        hints.save("a", 1);
        hints.force_rotate();
        hints.save("a", 2);
        assert_eq!(hints.take("a"), Some(2));
        assert_eq!(hints.take("a"), None);
    }

    #[test]
    fn rotation_is_time_driven() {
        let hints = PidHints::new();
        hints.save("a", 1);
        let later = Instant::now() + GENERATION * 3;
        hints.rotate_if_due(later);
        hints.rotate_if_due(later + GENERATION * 2);
        assert_eq!(hints.take("a"), None);
    }
}
