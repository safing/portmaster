//! Port of `github.com/seehuhn/fortuna.Generator` (v1.0.2), the core PRNG
//! used by Go's `base/rng`.
//!
//! Algorithm (unchanged from Go):
//!   * 32-byte key, 16-byte counter (little-endian, incremented after each block).
//!   * `reseed(seed)`: `key = SHA256d(key || seed)`, then `inc()`.
//!   * output blocks are `AES/Serpent(key, counter)` in CTR-like fashion;
//!     after at most `MAX_BLOCKS` blocks the key is replaced by 32 fresh bytes
//!     taken from the generator itself.

use std::time::{SystemTime, UNIX_EPOCH};

use crate::cipher::{BlockCipher, CipherKind};
use crate::sha256d::sha256d_two;

/// Maximum number of blocks generated before rekeying (`1 << 16` in Go).
pub const MAX_BLOCKS: u32 = 1 << 16;
pub const KEY_SIZE: usize = 32;
const BLOCK_SIZE: usize = 16;

/// The Fortuna pseudo-random generator. Not thread-safe on its own; the
/// high-level [`crate::Rng`] wraps it in a mutex.
pub struct Generator {
    kind: CipherKind,
    key: [u8; KEY_SIZE],
    cipher: Box<dyn BlockCipher>,
    counter: [u8; BLOCK_SIZE],
}

impl Generator {
    /// Create a generator and seed it from the OS, mirroring Go's
    /// `NewGenerator` (which calls `setInitialSeed`).
    pub fn new(kind: CipherKind) -> Generator {
        let mut gen = Generator::unseeded(kind);
        let mut seed = Vec::with_capacity(KEY_SIZE + 8);
        let mut os = [0u8; KEY_SIZE];
        if getrandom::fill(&mut os).is_ok() {
            seed.extend_from_slice(&os);
        }
        // Go mixes in the current time among other machine-specific data.
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_nanos() as i64)
            .unwrap_or(0);
        seed.extend_from_slice(&nanos.to_be_bytes());
        gen.reseed(&seed);
        gen
    }

    /// Create an unseeded (zero key, zero counter) generator.
    ///
    /// This corresponds to calling `reset()` on a fresh Go `Generator` and is
    /// what the deterministic Go test vectors require before `reseed`.
    pub fn unseeded(kind: CipherKind) -> Generator {
        let key = [0u8; KEY_SIZE];
        let cipher = kind.new_cipher(&key);
        Generator {
            kind,
            key,
            cipher,
            counter: [0u8; BLOCK_SIZE],
        }
    }

    /// Revert to the unseeded state (Go `Generator.reset`).
    pub fn reset(&mut self) {
        self.key = [0u8; KEY_SIZE];
        self.cipher = self.kind.new_cipher(&self.key);
        self.counter = [0u8; BLOCK_SIZE];
    }

    pub fn kind(&self) -> CipherKind {
        self.kind
    }

    /// Update the key from `seed`, then advance the counter (Go `Reseed`).
    pub fn reseed(&mut self, seed: &[u8]) {
        let new_key = sha256d_two(&self.key, seed);
        self.set_key(new_key);
        self.inc();
    }

    /// Deterministic state reset used by the Go `Seed(int64)` method.
    /// Go serialises the int64 big-endian (`int64ToBytes`).
    pub fn seed(&mut self, seed: i64) {
        self.reset();
        self.reseed(&seed.to_be_bytes());
    }

    fn set_key(&mut self, key: [u8; KEY_SIZE]) {
        self.key = key;
        self.cipher = self.kind.new_cipher(&key);
    }

    /// Increment the little-endian counter.
    fn inc(&mut self) {
        for byte in &mut self.counter {
            *byte = byte.wrapping_add(1);
            if *byte != 0 {
                break;
            }
        }
    }

    fn counter_is_zero(&self) -> bool {
        self.counter.iter().all(|&b| b == 0)
    }

    /// Append `k` encrypted counter blocks to `out` (Go `generateBlocks`).
    fn generate_blocks(&mut self, out: &mut Vec<u8>, k: u32) {
        assert!(!self.counter_is_zero(), "Fortuna generator not yet seeded");
        let mut buf = [0u8; BLOCK_SIZE];
        for _ in 0..k {
            buf.copy_from_slice(&self.counter);
            self.cipher.encrypt_block(&mut buf);
            out.extend_from_slice(&buf);
            self.inc();
        }
    }

    fn num_blocks(n: usize) -> u32 {
        n.div_ceil(BLOCK_SIZE) as u32
    }

    /// Return `n` pseudo-random bytes (Go `PseudoRandomData`).
    pub fn pseudo_random_data(&mut self, n: usize) -> Vec<u8> {
        let mut remaining = Self::num_blocks(n);
        let mut res = Vec::with_capacity(remaining as usize * BLOCK_SIZE);
        while remaining > 0 {
            let count = remaining.min(MAX_BLOCKS);
            self.generate_blocks(&mut res, count);
            remaining -= count;

            // Rekey with 32 fresh bytes produced by the generator itself.
            let mut new_key_bytes = Vec::with_capacity(KEY_SIZE);
            self.generate_blocks(&mut new_key_bytes, Self::num_blocks(KEY_SIZE));
            let mut new_key = [0u8; KEY_SIZE];
            new_key.copy_from_slice(&new_key_bytes[..KEY_SIZE]);
            self.set_key(new_key);
        }
        res.truncate(n);
        res
    }

    /// Fill `buf` with pseudo-random bytes.
    pub fn fill(&mut self, buf: &mut [u8]) {
        let data = self.pseudo_random_data(buf.len());
        buf.copy_from_slice(&data);
    }
}
