//! Port of Go's `base/rng` package: a feedable CSPRNG built on Fortuna.
//!
//! The algorithm is `github.com/seehuhn/fortuna` v1.0.2 (32 entropy pools in
//! the Go `Accumulator`; Portmaster actually drives the lower-level
//! `fortuna.Generator` directly). This crate provides:
//!
//! * [`Generator`] — the byte-for-byte compatible Fortuna PRNG.
//! * [`Rng`] — thread-safe, feedable wrapper reproducing Go's `Read`,
//!   `Bytes`, `Number`, `Feeder` and reseed logic.
//! * [`feed`] — the OS (getrandom), tick and full-feed entropy sources.
//! * [`CipherKind`] — the Go-selectable `aes` (default) and `serpent` ciphers.
//!
//! No `unsafe` code is used.

pub mod cipher;
pub mod feed;
pub mod generator;
pub mod rng;
pub mod sha256d;

use std::sync::OnceLock;

pub use cipher::{BlockCipher, CipherKind};
pub use generator::Generator;
pub use rng::{Reader, Rng, RngError};

use cipher::CipherKind as Kind;

static GLOBAL: OnceLock<Rng> = OnceLock::new();

/// Process-wide RNG, equivalent to the Go package-level `Read`/`Bytes`/
/// `Number` functions. Initialised from the OS on first use.
pub fn global() -> &'static Rng {
    GLOBAL.get_or_init(|| Rng::new(Kind::Aes))
}

/// Go `Read`: read random bytes into `buf` from the global RNG.
pub fn read(buf: &mut [u8]) -> Result<usize, RngError> {
    global().read(buf)
}

/// Go `Bytes`: allocate `n` random bytes from the global RNG.
pub fn bytes(n: usize) -> Result<Vec<u8>, RngError> {
    global().bytes(n)
}

/// Go `Number`: random value in `0..=max` from the global RNG.
pub fn number(max: u64) -> Result<u64, RngError> {
    global().number(max)
}
