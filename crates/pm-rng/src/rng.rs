//! High-level feedable RNG, port of Go `base/rng` (`get.go`, `rng.go`).

use std::collections::VecDeque;
use std::io::{self, Read as IoRead};
use std::sync::Mutex;
use std::time::Instant;

use crate::cipher::CipherKind;
use crate::feed::{EntropyPool, RESEED_AFTER_BYTES, RESEED_AFTER_SECONDS};
use crate::generator::Generator;

#[derive(Debug, thiserror::Error)]
pub enum RngError {
    #[error("RNG is not ready yet")]
    NotReady,
    #[error("failed to get new entropy")]
    NoEntropy,
    #[error("could not read entropy from os: {0}")]
    OsEntropy(String),
    #[error("invalid maximum value")]
    InvalidMax,
}

struct Inner {
    gen: Generator,
    bytes_read: u64,
    last_feed: Instant,
    /// Entropy batches delivered by the feeders (Go `rngFeeder` channel).
    pending: VecDeque<Vec<u8>>,
    pool: EntropyPool,
    ready: bool,
}

/// A thread-safe, feedable CSPRNG. All public methods take `&self`; state is
/// protected by a mutex, making concurrent use safe (Go used `rngLock`).
pub struct Rng {
    inner: Mutex<Inner>,
}

impl Rng {
    /// Start a module-equivalent RNG, seeded from the OS.
    pub fn new(kind: CipherKind) -> Rng {
        Rng::from_generator(Generator::new(kind))
    }

    /// Build an RNG around an existing generator (useful for deterministic
    /// tests). Mirrors Go `Start()` after the generator has been created.
    pub fn from_generator(gen: Generator) -> Rng {
        Rng {
            inner: Mutex::new(Inner {
                gen,
                bytes_read: 0,
                last_feed: Instant::now(),
                pending: VecDeque::new(),
                pool: EntropyPool::new(),
                ready: true,
            }),
        }
    }

    pub fn is_ready(&self) -> bool {
        self.inner.lock().expect("rng mutex poisoned").ready
    }

    /// Go `Read`: reseed if necessary, then generate `buf.len()` bytes.
    pub fn read(&self, buf: &mut [u8]) -> Result<usize, RngError> {
        let data = self.bytes(buf.len())?;
        buf.copy_from_slice(&data);
        Ok(buf.len())
    }

    /// Go `Bytes`: allocate and fill `n` random bytes.
    pub fn bytes(&self, n: usize) -> Result<Vec<u8>, RngError> {
        let mut inner = self.inner.lock().expect("rng mutex poisoned");
        inner.check_entropy()?;
        let out = inner.gen.pseudo_random_data(n);
        inner.bytes_read += n as u64;
        Ok(out)
    }

    /// Go `Number`: uniformly distributed value in `0..=max` using rejection
    /// sampling over little-endian `u64` reads.
    pub fn number(&self, max: u64) -> Result<u64, RngError> {
        if max == 0 || max == u64::MAX {
            return Err(RngError::InvalidMax);
        }
        let secure_limit = u64::MAX - (u64::MAX % max);
        let upper = max + 1;
        loop {
            let bytes = self.bytes(8)?;
            let candidate = u64::from_le_bytes(
                bytes
                    .try_into()
                    .expect("eight random bytes"),
            );
            if candidate < secure_limit {
                return Ok(candidate % upper);
            }
        }
    }

    /// Supply entropy to the accumulator (Go `Feeder.SupplyEntropy`). Once
    /// enough bits have arrived the batch is queued for the generator.
    pub fn supply_entropy(&self, data: &[u8], bits: i64) {
        let mut inner = self.inner.lock().expect("rng mutex poisoned");
        if let Some(batch) = inner.pool.supply(data, bits) {
            inner.pending.push_back(batch);
        }
    }

    /// Supply entropy only while the pool is gathering.
    pub fn supply_entropy_if_needed(&self, data: &[u8], bits: i64) {
        let mut inner = self.inner.lock().expect("rng mutex poisoned");
        if let Some(batch) = inner.pool.supply_if_needed(data, bits) {
            inner.pending.push_back(batch);
        }
    }

    /// Supply an `i64` as 8 little-endian bytes.
    pub fn supply_entropy_as_int(&self, n: i64, bits: i64) {
        self.supply_entropy(&n.to_le_bytes(), bits);
    }

    pub fn needs_entropy(&self) -> bool {
        self.inner.lock().expect("rng mutex poisoned").pool.needs_entropy()
    }

    /// Drain all queued batches into the generator (Go `fullFeeder`).
    pub fn reseed_pending(&self) -> usize {
        let mut inner = self.inner.lock().expect("rng mutex poisoned");
        Self::reseed_pending_inner(&mut inner)
    }

    fn reseed_pending_inner(inner: &mut Inner) -> usize {
        let mut drained = 0;
        while let Some(batch) = inner.pending.pop_front() {
            inner.gen.reseed(&batch);
            inner.bytes_read = 0;
            inner.last_feed = Instant::now();
            drained += 1;
        }
        drained
    }
}

impl Inner {
    /// Go `checkEntropy`: reseed when too many bytes were read or too much
    /// time has passed. Returns `NoEntropy` if the feeder has nothing ready.
    fn check_entropy(&mut self) -> Result<(), RngError> {
        if !self.ready {
            return Err(RngError::NotReady);
        }
        if self.bytes_read > RESEED_AFTER_BYTES
            || self.last_feed.elapsed().as_secs() > RESEED_AFTER_SECONDS
        {
            let batch = self
                .pending
                .pop_front()
                .ok_or(RngError::NoEntropy)?;
            self.gen.reseed(&batch);
            self.bytes_read = 0;
            self.last_feed = Instant::now();
        }
        Ok(())
    }
}

/// `std::io::Read` adapter usable without owning the RNG (Go `Reader`).
#[derive(Copy, Clone, Debug, Default)]
pub struct Reader;

impl IoRead for Reader {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        Rng::read(crate::global(), buf).map_err(io::Error::other)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use std::thread;

    #[test]
    fn output_lengths_match_requests() {
        let rng = Rng::new(CipherKind::Aes);
        for n in [0usize, 1, 15, 16, 17, 32, 100, 4096] {
            assert_eq!(rng.bytes(n).unwrap().len(), n);
            let mut buf = vec![0u8; n];
            assert_eq!(rng.read(&mut buf).unwrap(), n);
        }
    }

    #[test]
    fn number_stays_in_range_and_is_unbiased() {
        let rng = Rng::new(CipherKind::Aes);
        let subjects = 10u64;
        let samples = 20_000u64;
        let mut counts = vec![0u64; subjects as usize + 1];
        for _ in 0..samples {
            let n = rng.number(subjects).unwrap();
            assert!(n <= subjects);
            counts[n as usize] += 1;
        }
        let expected = samples / (subjects + 1);
        // A generous 20% margin: enough to catch a gross modulo bias while
        // staying far outside normal sampling noise (the Go test was flaky
        // enough that upstream skipped it entirely).
        let margin = expected / 5;
        for count in counts {
            assert!(
                (expected - margin) <= count && count <= (expected + margin),
                "biased distribution: {count} vs expected {expected}"
            );
        }
        assert!(matches!(rng.number(0), Err(RngError::InvalidMax)));
    }

    #[test]
    fn concurrent_access_is_safe_and_diverse() {
        let rng = Arc::new(Rng::new(CipherKind::Aes));
        let mut handles = Vec::new();
        for _ in 0..8 {
            let rng = Arc::clone(&rng);
            handles.push(thread::spawn(move || {
                let mut seen = std::collections::HashSet::new();
                for _ in 0..1000 {
                    let b = rng.bytes(8).unwrap();
                    seen.insert(b);
                    let n = rng.number(1000).unwrap();
                    assert!(n <= 1000);
                }
                seen
            }));
        }
        let mut all: std::collections::HashSet<Vec<u8>> = std::collections::HashSet::new();
        for h in handles {
            all.extend(h.join().unwrap());
        }
        // 8000 independent 8-byte values: collisions are essentially
        // impossible, so this catches a broken (e.g. constant) generator.
        assert_eq!(all.len(), 8000);
    }

    #[test]
    fn empty_entropy_pool_returns_error_on_reseed() {
        let rng = Rng::new(CipherKind::Aes);
        {
            let mut inner = rng.inner.lock().unwrap();
            inner.bytes_read = RESEED_AFTER_BYTES + 1;
            assert!(inner.pending.is_empty());
        }
        assert!(matches!(rng.bytes(16), Err(RngError::NoEntropy)));

        // Once entropy is supplied, the next call succeeds and resets state.
        rng.supply_entropy(&[7u8; 32], 256);
        assert_eq!(rng.bytes(16).unwrap().len(), 16);
        {
            let inner = rng.inner.lock().unwrap();
            assert_eq!(inner.bytes_read, 16);
            assert!(inner.pending.is_empty());
        }
    }

    #[test]
    fn not_ready_blocks_reads() {
        let gen = Generator::new(CipherKind::Aes);
        let rng = Rng::from_generator(gen);
        rng.inner.lock().unwrap().ready = false;
        assert!(matches!(rng.bytes(8), Err(RngError::NotReady)));
    }
}
