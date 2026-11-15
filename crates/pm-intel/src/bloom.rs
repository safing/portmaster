//! Port of the bloom filters used by Go `service/intel/filterlists`.
//!
//! The Go implementation uses `github.com/tannerryan/ring`: a bloom filter with
//! 128-bit MurmurHash3 and a four-hash scheme. Bit layout and serialization are
//! reproduced exactly, so filters cached by the Go core stay valid.

use std::collections::HashMap;

/// False positive rate of the filterlists filters.
pub const BF_FALSE_POSITIVE_RATE: f64 = 0.001;
/// Capacity of the domain filter.
pub const DOMAIN_BF_SIZE: usize = 2_000_000;
/// Capacity of the ASN filter.
pub const ASN_BF_SIZE: usize = 10_000;
/// Capacity of the country filter.
pub const COUNTRY_BF_SIZE: usize = 300;
/// Capacity of the IPv4 filter.
pub const IPV4_BF_SIZE: usize = 200_000;
/// Capacity of the IPv6 filter.
pub const IPV6_BF_SIZE: usize = 20_000;

const MURMUR_C1: u64 = 0x87c3_7b91_1142_53d5;
const MURMUR_C2: u64 = 0x4cf5_ad43_2745_937f;
const MURMUR_C3: u64 = 0x52dc_e729;
const MURMUR_C4: u64 = 0x3849_5ab5;
const MURMUR_C5: u64 = 0xff51_afd7_ed55_8ccd;
const MURMUR_C6: u64 = 0xc4ce_b9fe_1a85_ec53;

/// Errors of the bloom filter.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum BloomError {
    #[error("elements must be greater than 0")]
    NoElements,
    #[error("falsePositive must be greater than 0 and less than 1")]
    InvalidFalsePositive,
    #[error("incorrect length: {0}")]
    IncorrectLength(usize),
    #[error("unexpected version: {0}")]
    UnexpectedVersion(u8),
    #[error("rings must have the same m/k parameters")]
    MismatchedParameters,
}

/// A bloom filter, byte-compatible with Go's `ring.Ring`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Ring {
    size: u64,
    hash: u64,
    bits: Vec<u8>,
}

impl Ring {
    /// Creates a filter for the given number of elements and false positive
    /// rate, like Go `ring.Init`.
    pub fn init(elements: usize, false_positive: f64) -> Result<Ring, BloomError> {
        if elements == 0 {
            return Err(BloomError::NoElements);
        }
        if !(false_positive > 0.0 && false_positive < 1.0) {
            return Err(BloomError::InvalidFalsePositive);
        }

        // Same math as the Go library, including the operation order.
        let ln2 = 2.0f64.ln();
        let m = -(elements as f64) * false_positive.ln() / ln2.powf(2.0);
        let k = (m / elements as f64) * ln2;

        let size = m.ceil() as u64;
        let hash = k.ceil() as u64;
        Ok(Ring {
            size,
            hash,
            bits: vec![0; (size / 8 + 1) as usize],
        })
    }

    /// Returns the number of bits of the filter.
    pub fn size(&self) -> u64 {
        self.size
    }

    /// Returns the number of hash rounds.
    pub fn hash_rounds(&self) -> u64 {
        self.hash
    }

    /// Returns the bit array.
    pub fn bits(&self) -> &[u8] {
        &self.bits
    }

    /// Adds the data to the filter.
    pub fn add(&mut self, data: &[u8]) {
        let hash = generate_multi_hash(data);
        for round in 0..self.hash {
            let index = get_round(hash, round) % self.size;
            self.bits[(index / 8) as usize] |= 1 << (index % 8);
        }
    }

    /// Returns whether the data may be in the filter.
    pub fn test(&self, data: &[u8]) -> bool {
        let hash = generate_multi_hash(data);
        for round in 0..self.hash {
            let index = get_round(hash, round) % self.size;
            if self.bits[(index / 8) as usize] & (1 << (index % 8)) == 0 {
                return false;
            }
        }
        true
    }

    /// Clears the filter.
    pub fn reset(&mut self) {
        self.bits = vec![0; (self.size / 8 + 1) as usize];
    }

    /// Merges another filter into this one, like Go `Ring.Merge`.
    pub fn merge(&mut self, other: &Ring) -> Result<(), BloomError> {
        if self.size != other.size || self.hash != other.hash {
            return Err(BloomError::MismatchedParameters);
        }
        for (target, source) in self.bits.iter_mut().zip(other.bits.iter()) {
            *target |= source;
        }
        Ok(())
    }

    /// Serializes the filter, like Go `Ring.MarshalBinary`.
    pub fn marshal_binary(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.bits.len() + 17);
        out.push(1);
        out.extend_from_slice(&self.size.to_be_bytes());
        out.extend_from_slice(&self.hash.to_be_bytes());
        out.extend_from_slice(&self.bits);
        out
    }

    /// Deserializes a filter, like Go `Ring.UnmarshalBinary`.
    pub fn unmarshal_binary(data: &[u8]) -> Result<Ring, BloomError> {
        if data.len() < 17 + 1 {
            return Err(BloomError::IncorrectLength(data.len()));
        }
        if data[0] != 1 {
            return Err(BloomError::UnexpectedVersion(data[0]));
        }

        let size = u64::from_be_bytes(data[1..9].try_into().expect("slice length"));
        let hash = u64::from_be_bytes(data[9..17].try_into().expect("slice length"));
        let mut bits = vec![0; (size / 8 + 1) as usize];
        let available = &data[17..];
        let length = bits.len().min(available.len());
        bits[..length].copy_from_slice(&available[..length]);

        Ok(Ring { size, hash, bits })
    }
}

/// Returns two 64-bit outputs of a 128-bit MurmurHash3 hash.
fn murmur128(data: &[u8]) -> (u64, u64) {
    let (mut h1, mut h2) = (0u64, 0u64);
    let blocks = data.len() / 16;

    for block in 0..blocks {
        let offset = block * 16;
        let mut k1 = read_le_u64(&data[offset..]);
        let mut k2 = read_le_u64(&data[offset + 8..]);

        k1 = k1.wrapping_mul(MURMUR_C1).rotate_left(31);
        k1 = k1.wrapping_mul(MURMUR_C2);
        h1 ^= k1;

        h1 = h1.rotate_left(27);
        h1 = h1.wrapping_add(h2);
        h1 = h1.wrapping_mul(5).wrapping_add(MURMUR_C3);

        k2 = k2.wrapping_mul(MURMUR_C2).rotate_left(33);
        k2 = k2.wrapping_mul(MURMUR_C1);
        h2 ^= k2;

        h2 = h2.rotate_left(31);
        h2 = h2.wrapping_add(h1);
        h2 = h2.wrapping_mul(5).wrapping_add(MURMUR_C4);
    }

    let tail = blocks * 16;
    let remaining = data.len() & 15;

    // Bytes 8..remaining belong to the second half of the tail (Go cases 9-15).
    if remaining > 8 {
        let mut k2 = 0u64;
        for index in 8..remaining {
            k2 ^= (data[tail + index] as u64) << (8 * (index - 8));
        }
        k2 = k2.wrapping_mul(MURMUR_C2).rotate_left(33);
        k2 = k2.wrapping_mul(MURMUR_C1);
        h2 ^= k2;
    }

    // Bytes 0..min(remaining, 8) belong to the first half (Go cases 1-8).
    if remaining > 0 {
        let mut k1 = 0u64;
        for index in 0..remaining.min(8) {
            k1 ^= (data[tail + index] as u64) << (8 * index);
        }
        k1 = k1.wrapping_mul(MURMUR_C1).rotate_left(31);
        k1 = k1.wrapping_mul(MURMUR_C2);
        h1 ^= k1;
    }

    let length = data.len() as u64;
    h1 ^= length;
    h2 ^= length;

    h1 = h1.wrapping_add(h2);
    h2 = h2.wrapping_add(h1);

    h1 = fmix(h1);
    h2 = fmix(h2);

    h1 = h1.wrapping_add(h2);
    h2 = h2.wrapping_add(h1);

    (h1, h2)
}

/// The 64-bit MurmurHash3 finalizer.
fn fmix(mut h: u64) -> u64 {
    h ^= h >> 33;
    h = h.wrapping_mul(MURMUR_C5);
    h ^= h >> 33;
    h = h.wrapping_mul(MURMUR_C6);
    h ^= h >> 33;
    h
}

/// Reads a little-endian u64; the caller guarantees eight bytes.
fn read_le_u64(data: &[u8]) -> u64 {
    u64::from_le_bytes(data[..8].try_into().expect("slice length"))
}

/// Returns four 64-bit hashes, like Go `generateMultiHash`.
fn generate_multi_hash(data: &[u8]) -> [u64; 4] {
    let (h1, h2) = murmur128(data);
    let mut buff = Vec::with_capacity(data.len() + 1);
    buff.extend_from_slice(data);
    buff.push(1);
    let (h3, h4) = murmur128(&buff);
    [h1, h2, h3, h4]
}

/// Returns the simulated nth round of hashing, like Go `getRound`.
fn get_round(hash: [u64; 4], round: u64) -> u64 {
    let index = 2 + (((round + (round % 2)) % 4) / 2);
    let pre = hash[(round % 2) as usize];
    let post = round.wrapping_mul(hash[index as usize]);
    pre.wrapping_add(post)
}

/// Bloom filters per entity type, like Go `scopedBloom`.
pub struct ScopedBloom {
    domain: Ring,
    asn: Ring,
    country: Ring,
    ipv4: Ring,
    ipv6: Ring,
    /// Number of `add` calls per entity type, duplicates included.
    added: HashMap<String, usize>,
}

impl Default for ScopedBloom {
    fn default() -> Self {
        ScopedBloom::new()
    }
}

impl ScopedBloom {
    /// Creates filters with the capacities of the filterlists package.
    pub fn new() -> ScopedBloom {
        ScopedBloom::with_sizes(
            DOMAIN_BF_SIZE,
            ASN_BF_SIZE,
            COUNTRY_BF_SIZE,
            IPV4_BF_SIZE,
            IPV6_BF_SIZE,
        )
    }

    /// Creates filters with explicit capacities, for tests or embedded use.
    pub fn with_sizes(
        domain: usize,
        asn: usize,
        country: usize,
        ipv4: usize,
        ipv6: usize,
    ) -> ScopedBloom {
        let init = |size: usize| {
            Ring::init(size, BF_FALSE_POSITIVE_RATE)
                .expect("filter size and false positive rate are valid")
        };
        ScopedBloom {
            domain: init(domain),
            asn: init(asn),
            country: init(country),
            ipv4: init(ipv4),
            ipv6: init(ipv6),
            added: HashMap::new(),
        }
    }

    /// Returns the filter of an entity type.
    pub fn ring(&self, entity_type: &str) -> Option<&Ring> {
        match entity_type.to_lowercase().as_str() {
            "domain" => Some(&self.domain),
            "asn" => Some(&self.asn),
            "country" => Some(&self.country),
            "ipv4" => Some(&self.ipv4),
            "ipv6" => Some(&self.ipv6),
            _ => None,
        }
    }

    /// Returns all scopes.
    pub fn scopes() -> &'static [&'static str] {
        &["domain", "asn", "country", "ipv4", "ipv6"]
    }

    /// Adds a value to the filter of its entity type.
    ///
    /// Unknown types are dropped: testing them always returns `true` to the
    /// caller, like in Go.
    pub fn add(&mut self, entity_type: &str, value: &str) {
        let Some(ring) = self.ring_mut(entity_type) else {
            tracing::warn!(entity_type, value, "intel/filterlists: unknown entity type");
            return;
        };
        ring.add(value.as_bytes());
        *self.added.entry(entity_type.to_lowercase()).or_insert(0) += 1;
    }

    /// Tests whether a value may be in the filter of its entity type.
    ///
    /// Unknown types simulate a match, like in Go.
    pub fn test(&self, entity_type: &str, value: &str) -> bool {
        match self.ring(entity_type) {
            Some(ring) => ring.test(value.as_bytes()),
            None => {
                tracing::warn!(entity_type, "intel/filterlists: unknown entity type");
                true
            }
        }
    }

    /// Returns the values added per filter next to its capacity.
    pub fn capacity_report(&self) -> String {
        let added = |scope: &str| self.added.get(scope).copied().unwrap_or(0);
        format!(
            "domain={}/{} asn={}/{} country={}/{} ipv4={}/{} ipv6={}/{}",
            added("domain"),
            self.domain.size(),
            added("asn"),
            self.asn.size(),
            added("country"),
            self.country.size(),
            added("ipv4"),
            self.ipv4.size(),
            added("ipv6"),
            self.ipv6.size(),
        )
    }

    fn ring_mut(&mut self, entity_type: &str) -> Option<&mut Ring> {
        match entity_type.to_lowercase().as_str() {
            "domain" => Some(&mut self.domain),
            "asn" => Some(&mut self.asn),
            "country" => Some(&mut self.country),
            "ipv4" => Some(&mut self.ipv4),
            "ipv6" => Some(&mut self.ipv6),
            _ => None,
        }
    }

    /// Replaces all filters with the ones of `other`.
    pub fn replace_with(&mut self, other: ScopedBloom) {
        *self = other;
    }

    /// Returns the filter of a scope by its cache name.
    pub fn ring_by_scope(&self, scope: &str) -> Option<&Ring> {
        self.ring(scope)
    }

    /// Returns the filters of all scopes together with their cache name.
    pub fn rings(&self) -> [(&'static str, &Ring); 5] {
        [
            ("domain", &self.domain),
            ("asn", &self.asn),
            ("country", &self.country),
            ("ipv4", &self.ipv4),
            ("ipv6", &self.ipv6),
        ]
    }

    /// Replaces the filter of a scope.
    pub fn set_ring(&mut self, scope: &str, ring: Ring) {
        match scope {
            "domain" => self.domain = ring,
            "asn" => self.asn = ring,
            "country" => self.country = ring,
            "ipv4" => self.ipv4 = ring,
            "ipv6" => self.ipv6 = ring,
            _ => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sizing_matches_go() {
        // Values from the Go fixture (`intel.json`).
        let cases = [
            (2_000_000, 28_755_176u64, 10u64),
            (10_000, 143_776, 10),
            (300, 4_314, 10),
            (200_000, 2_875_518, 10),
            (20_000, 287_552, 10),
        ];
        for (elements, size, hash) in cases {
            let ring = Ring::init(elements, BF_FALSE_POSITIVE_RATE).unwrap();
            assert_eq!(ring.size(), size, "{elements}");
            assert_eq!(ring.hash_rounds(), hash, "{elements}");
        }

        assert_eq!(Ring::init(0, 0.1).unwrap_err(), BloomError::NoElements);
        assert_eq!(
            Ring::init(10, 1.0).unwrap_err(),
            BloomError::InvalidFalsePositive
        );
    }

    #[test]
    fn membership_and_reset() {
        let mut ring = Ring::init(100, 0.01).unwrap();
        assert!(!ring.test(b"value"));
        ring.add(b"value");
        assert!(ring.test(b"value"));
        ring.reset();
        assert!(!ring.test(b"value"));
    }

    #[test]
    fn scoped_filters() {
        let mut bloom = ScopedBloom::with_sizes(100, 10, 5, 20, 20);

        bloom.add("domain", "example.com.");
        bloom.add("DOMAIN", "example.com.");
        bloom.add("ipv4", "192.0.2.10");
        bloom.add("unknown", "ignored");

        assert!(bloom.test("domain", "example.com."));
        assert!(bloom.test("ipv4", "192.0.2.10"));
        assert!(!bloom.test("domain", "other.example."));
        // Unknown scopes simulate a match.
        assert!(bloom.test("unknown", "anything"));

        let report = bloom.capacity_report();
        assert!(report.starts_with("domain=2/"), "{report}");
        assert!(report.contains("ipv4=1/"), "{report}");

        let other = ScopedBloom::with_sizes(100, 10, 5, 20, 20);
        bloom.replace_with(other);
        assert!(!bloom.test("domain", "example.com."));
    }
}
