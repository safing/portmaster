//! Entropy accumulation and the three feeders used by Go's `base/rng`:
//! OS `crypto/rand` (getrandom syscall), the nanosecond tick feeder and the
//! full-feed timer.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::Duration;

use crate::rng::{Rng, RngError};

/// Minimum entropy (in bits) before a feeder flushes its pool (Go
/// `minFeedEntropy`).
pub const MIN_FEED_ENTROPY: i64 = 256;

/// Bytes read from the OS per entropy batch (Go `minFeedEntropy / 8`).
pub const OS_ENTROPY_BYTES: usize = (MIN_FEED_ENTROPY / 8) as usize;

/// `reseedAfterSeconds` from Go `get.go`.
pub const RESEED_AFTER_SECONDS: u64 = 600;
/// `reseedAfterBytes` from Go `get.go`.
pub const RESEED_AFTER_BYTES: u64 = 1_048_576;

/// Full-feed interval: `max(reseedAfterSeconds * 5, 600)` seconds.
pub fn full_feed_duration() -> Duration {
    let secs = (RESEED_AFTER_SECONDS * 5).max(600);
    Duration::from_secs(secs)
}

/// Tick interval: `max(reseedAfterSeconds * 100 / (minFeedEntropy * 8), 10)`
/// milliseconds, matching Go `getTickFeederTickDuration`.
pub fn tick_duration() -> Duration {
    let msecs_available = RESEED_AFTER_SECONDS * 100;
    let ticks_needed = MIN_FEED_ENTROPY as u64 * 8;
    let tick_msecs = (msecs_available / ticks_needed).max(10);
    Duration::from_millis(tick_msecs)
}

/// Read fresh entropy from the operating system (Go `crypto/rand.Read`).
pub fn os_entropy(len: usize) -> Result<Vec<u8>, RngError> {
    let mut buf = vec![0u8; len];
    getrandom::fill(&mut buf).map_err(|e| RngError::OsEntropy(e.to_string()))?;
    Ok(buf)
}

/// Entropy accumulator mirroring Go's `Feeder.run` gather loop.
#[derive(Debug)]
pub struct EntropyPool {
    buffer: Vec<u8>,
    entropy_bits: i64,
    needs_entropy: bool,
}

impl Default for EntropyPool {
    fn default() -> Self {
        Self::new()
    }
}

impl EntropyPool {
    pub fn new() -> EntropyPool {
        EntropyPool {
            buffer: Vec::new(),
            entropy_bits: 0,
            needs_entropy: true,
        }
    }

    /// Whether the pool is still gathering entropy (Go `NeedsEntropy`).
    pub fn needs_entropy(&self) -> bool {
        self.needs_entropy
    }

    /// Supply data with an entropy estimate. Once `MIN_FEED_ENTROPY` is
    /// reached the compiled bytes are returned and the pool resets, matching
    /// the Go feeder handing a batch to `rngFeeder`.
    pub fn supply(&mut self, data: &[u8], bits: i64) -> Option<Vec<u8>> {
        self.buffer.extend_from_slice(data);
        self.entropy_bits += bits;
        if self.entropy_bits >= MIN_FEED_ENTROPY {
            self.needs_entropy = false;
            let compiled = std::mem::take(&mut self.buffer);
            self.entropy_bits = 0;
            self.needs_entropy = true;
            Some(compiled)
        } else {
            None
        }
    }

    /// Supply an `i64` as 8 little-endian bytes (Go `SupplyEntropyAsInt`).
    pub fn supply_int(&mut self, n: i64, bits: i64) -> Option<Vec<u8>> {
        self.supply(&n.to_le_bytes(), bits)
    }

    /// Supply only while the pool is gathering (Go `SupplyEntropyIfNeeded`,
    /// following the method's intent — see the crate docs for the Go quirk).
    pub fn supply_if_needed(&mut self, data: &[u8], bits: i64) -> Option<Vec<u8>> {
        if self.needs_entropy {
            self.supply(data, bits)
        } else {
            None
        }
    }
}

/// The nanosecond tick feeder (Go `tickFeeder`): collect the least
/// significant bit of successive timestamps into an `i64`.
#[derive(Debug, Default)]
pub struct TickFeeder {
    value: i64,
    pushes: u8,
}

impl TickFeeder {
    pub fn new() -> TickFeeder {
        TickFeeder::default()
    }

    /// Add one timestamp (in nanoseconds). Returns 8 bytes (and 8 bits of
    /// entropy) after every 64 pushes, like the Go feeder.
    pub fn push_ns(&mut self, now_ns: i128) -> Option<[u8; 8]> {
        self.value = (self.value << 1) | (now_ns % 2) as i64;
        self.pushes += 1;
        if self.pushes >= 64 {
            let bytes = (self.value as u64).to_le_bytes();
            self.pushes = 0;
            Some(bytes)
        } else {
            None
        }
    }
}

/// Background OS feeder: periodically feeds `os_entropy` into the RNG.
/// Returns a handle whose `is_finished` flag can be set to stop the loop.
pub fn spawn_os_feeder(
    rng: Arc<Rng>,
    interval: Duration,
) -> (JoinHandle<()>, Arc<AtomicBool>) {
    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = Arc::clone(&stop);
    let handle = thread::spawn(move || {
        while !stop_clone.load(Ordering::Relaxed) {
            match os_entropy(OS_ENTROPY_BYTES) {
                Ok(data) => {
                    let bits = (data.len() * 8) as i64;
                    rng.supply_entropy(&data, bits);
                }
                Err(_) => break,
            }
            thread::sleep(interval);
        }
    });
    (handle, stop)
}

/// Background tick feeder: feeds timing noise into the RNG.
pub fn spawn_tick_feeder(
    rng: Arc<Rng>,
    interval: Duration,
) -> (JoinHandle<()>, Arc<AtomicBool>) {
    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = Arc::clone(&stop);
    let handle = thread::spawn(move || {
        let mut feeder = TickFeeder::new();
        while !stop_clone.load(Ordering::Relaxed) {
            let now = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_nanos() as i128)
                .unwrap_or(0);
            if let Some(bytes) = feeder.push_ns(now) {
                rng.supply_entropy(&bytes, 8);
            }
            thread::sleep(interval);
        }
    });
    (handle, stop)
}

/// Drain all pending entropy batches into the generator (Go `fullFeeder`).
pub fn full_feed(rng: &Rng) -> usize {
    rng.reseed_pending()
}

/// Start a background full-feed timer (Go `fullFeeder`).
pub fn spawn_full_feeder(rng: Arc<Rng>) -> (JoinHandle<()>, Arc<AtomicBool>) {
    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = Arc::clone(&stop);
    let handle = thread::spawn(move || {
        let interval = full_feed_duration();
        while !stop_clone.load(Ordering::Relaxed) {
            thread::sleep(interval);
            full_feed(&rng);
        }
    });
    (handle, stop)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pool_waits_for_minimum_entropy() {
        let mut pool = EntropyPool::new();
        assert!(pool.needs_entropy());
        assert!(pool.supply(&[1, 2, 3], 8).is_none());
        assert!(pool.supply(&[4, 5, 6, 7], 247).is_none());
        let batch = pool.supply(&[8], 1).expect("pool should flush at 256 bits");
        assert_eq!(batch, [1, 2, 3, 4, 5, 6, 7, 8]);
        assert!(pool.needs_entropy());
    }

    #[test]
    fn supply_int_is_little_endian() {
        let mut pool = EntropyPool::new();
        let batch = pool.supply_int(0x0102_0304_0506_0708, MIN_FEED_ENTROPY).unwrap();
        assert_eq!(batch, 0x0102_0304_0506_0708i64.to_le_bytes());
    }

    #[test]
    fn tick_feeder_collects_64_bits() {
        let mut tick = TickFeeder::new();
        for _ in 0..63 {
            assert!(tick.push_ns(0).is_none());
        }
        let bytes = tick.push_ns(1).expect("64th push produces a word");
        // bits: 63 zeros then a one, shifted into an i64 and stored LE.
        assert_eq!(bytes, [1, 0, 0, 0, 0, 0, 0, 0]);
    }

    #[test]
    fn feeder_intervals_match_go() {
        assert_eq!(full_feed_duration(), Duration::from_secs(3000));
        // 60000 / 2048 = 29ms in Go, but clamped to at least 10ms.
        assert_eq!(tick_duration(), Duration::from_millis(29));
    }

    #[test]
    fn os_entropy_has_requested_length() {
        assert_eq!(os_entropy(OS_ENTROPY_BYTES).unwrap().len(), 32);
    }
}
