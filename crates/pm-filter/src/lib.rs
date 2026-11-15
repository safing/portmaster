#![forbid(unsafe_code)]

//! # pm-filter
//!
//! High-performance scoped Bloom Filter and rule matching engine for Portmaster.

use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};

/// A fast bit-vector bloom filter with parameterized size and hash functions.
#[derive(Debug, Clone)]
pub struct BloomFilter {
    bits: Vec<u64>,
    num_bits: usize,
    num_hashes: usize,
}

impl BloomFilter {
    /// Create a new Bloom filter sized for `expected_items` and target `fp_rate`.
    pub fn new(expected_items: usize, fp_rate: f64) -> Self {
        let expected = expected_items.max(1) as f64;
        let rate = fp_rate.clamp(0.00001, 0.5);

        // Optimal bit size: m = - (n * ln(p)) / (ln(2)^2)
        let m = (-(expected * rate.ln()) / (2.0f64.ln().powi(2))).ceil() as usize;
        let num_bits = m.max(64);

        // Optimal number of hash functions: k = (m / n) * ln(2)
        let k = ((num_bits as f64 / expected) * 2.0f64.ln()).round() as usize;
        let num_hashes = k.clamp(1, 30);

        let num_u64s = (num_bits + 63) / 64;
        Self {
            bits: vec![0u64; num_u64s],
            num_bits,
            num_hashes,
        }
    }

    fn hashes<T: Hash + ?Sized>(&self, item: &T) -> (u64, u64) {
        let mut h1 = DefaultHasher::new();
        item.hash(&mut h1);
        let hash1 = h1.finish();

        let mut h2 = DefaultHasher::new();
        hash1.hash(&mut h2);
        let hash2 = h2.finish();

        (hash1, hash2)
    }

    /// Insert an item into the bloom filter.
    pub fn insert<T: Hash + ?Sized>(&mut self, item: &T) {
        let (h1, h2) = self.hashes(item);
        for i in 0..self.num_hashes {
            let bit_index = (h1.wrapping_add((i as u64).wrapping_mul(h2))) as usize % self.num_bits;
            self.bits[bit_index / 64] |= 1u64 << (bit_index % 64);
        }
    }

    /// Check if an item may be in the bloom filter.
    pub fn contains<T: Hash + ?Sized>(&self, item: &T) -> bool {
        let (h1, h2) = self.hashes(item);
        for i in 0..self.num_hashes {
            let bit_index = (h1.wrapping_add((i as u64).wrapping_mul(h2))) as usize % self.num_bits;
            if (self.bits[bit_index / 64] & (1u64 << (bit_index % 64))) == 0 {
                return false;
            }
        }
        true
    }

    /// Clear all bits in the filter.
    pub fn clear(&mut self) {
        self.bits.fill(0);
    }
}

/// ScopedBloom manages separate bloom filters for domains, IPs, ASNs, etc.
#[derive(Debug, Clone)]
pub struct ScopedBloom {
    pub domain: BloomFilter,
    pub ipv4: BloomFilter,
    pub ipv6: BloomFilter,
    pub asn: BloomFilter,
    pub country: BloomFilter,
}

impl Default for ScopedBloom {
    fn default() -> Self {
        Self {
            domain: BloomFilter::new(500_000, 0.001),
            ipv4: BloomFilter::new(200_000, 0.001),
            ipv6: BloomFilter::new(100_000, 0.001),
            asn: BloomFilter::new(50_000, 0.001),
            country: BloomFilter::new(500, 0.001),
        }
    }
}

impl ScopedBloom {
    pub fn new() -> Self {
        Self::default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bloom_filter_basic() {
        let mut bf = BloomFilter::new(1000, 0.01);
        assert!(!bf.contains("google.com"));

        bf.insert("google.com");
        assert!(bf.contains("google.com"));
        assert!(!bf.contains("tracker.com"));

        bf.insert("tracker.com");
        assert!(bf.contains("tracker.com"));
    }

    #[test]
    fn test_scoped_bloom() {
        let mut scoped = ScopedBloom::new();
        scoped.domain.insert("ads.example.com");
        scoped.ipv4.insert("1.2.3.4");

        assert!(scoped.domain.contains("ads.example.com"));
        assert!(!scoped.domain.contains("good.example.com"));
        assert!(scoped.ipv4.contains("1.2.3.4"));
        assert!(!scoped.ipv4.contains("8.8.8.8"));
    }
}
