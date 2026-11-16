//! SHA-256d: double SHA-256, exactly as implemented by `seehuhn/sha256d`
//! which the Go Fortuna library uses for reseeding and pool hashing.
//!
//! `sha256d(data) = SHA256(SHA256(data))`.

use sha2::{Digest, Sha256};

/// Compute SHA-256d over a single byte slice.
pub fn sha256d(data: &[u8]) -> [u8; 32] {
    let first = Sha256::digest(data);
    let second = Sha256::digest(first);
    let mut out = [0u8; 32];
    out.copy_from_slice(&second);
    out
}

/// Compute SHA-256d over the concatenation of two slices without allocating.
///
/// This is what `Generator::reseed` needs: `SHA256d(key || seed)`.
pub fn sha256d_two(a: &[u8], b: &[u8]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    hasher.update(a);
    hasher.update(b);
    let first = hasher.finalize();
    let second = Sha256::digest(first);
    let mut out = [0u8; 32];
    out.copy_from_slice(&second);
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn known_hash() {
        // SHA256d("abc") computed with Python hashlib:
        // sha256(sha256("abc"))
        let expected =
            hex_decode("4f8b42c22dd3729b519ba6f68d2da7cc5b2d606d05daed5ad5128cc03e6c6358");
        assert_eq!(sha256d(b"abc"), expected);
        assert_eq!(sha256d_two(b"ab", b"c"), expected);
    }

    fn hex_decode(s: &str) -> [u8; 32] {
        let mut out = [0u8; 32];
        for (i, chunk) in s.as_bytes().chunks(2).enumerate() {
            let hi = (chunk[0] as char).to_digit(16).unwrap() as u8;
            let lo = (chunk[1] as char).to_digit(16).unwrap() as u8;
            out[i] = (hi << 4) | lo;
        }
        out
    }
}
