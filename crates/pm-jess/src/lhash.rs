//! Labeled hashes (`jess/lhash`): an algorithm ID plus a digest.

use blake2::digest::consts::{U32, U48, U64};
use blake2::{Blake2b, Blake2s256, Digest};
use sha2::{Sha224, Sha256, Sha384, Sha512, Sha512_224, Sha512_256};
use sha3::{Sha3_224, Sha3_256, Sha3_384, Sha3_512};

use crate::Error;

/// Hash algorithms with their `lhash.Algorithm` IDs.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Algorithm {
    Sha2_224 = 8,
    Sha2_256 = 9,
    Sha2_384 = 10,
    Sha2_512 = 11,
    Sha2_512_224 = 12,
    Sha2_512_256 = 13,
    Sha3_224 = 16,
    Sha3_256 = 17,
    Sha3_384 = 18,
    Sha3_512 = 19,
    Blake2s256 = 24,
    Blake2b256 = 25,
    Blake2b384 = 26,
    Blake2b512 = 27,
    Blake3 = 32,
}

impl Algorithm {
    pub fn from_id(id: u64) -> Option<Self> {
        Some(match id {
            8 => Self::Sha2_224,
            9 => Self::Sha2_256,
            10 => Self::Sha2_384,
            11 => Self::Sha2_512,
            12 => Self::Sha2_512_224,
            13 => Self::Sha2_512_256,
            16 => Self::Sha3_224,
            17 => Self::Sha3_256,
            18 => Self::Sha3_384,
            19 => Self::Sha3_512,
            24 => Self::Blake2s256,
            25 => Self::Blake2b256,
            26 => Self::Blake2b384,
            27 => Self::Blake2b512,
            32 => Self::Blake3,
            _ => return None,
        })
    }

    /// Computes the digest of `data`.
    pub fn digest(self, data: &[u8]) -> Vec<u8> {
        match self {
            Self::Sha2_224 => Sha224::digest(data).to_vec(),
            Self::Sha2_256 => Sha256::digest(data).to_vec(),
            Self::Sha2_384 => Sha384::digest(data).to_vec(),
            Self::Sha2_512 => Sha512::digest(data).to_vec(),
            Self::Sha2_512_224 => Sha512_224::digest(data).to_vec(),
            Self::Sha2_512_256 => Sha512_256::digest(data).to_vec(),
            Self::Sha3_224 => Sha3_224::digest(data).to_vec(),
            Self::Sha3_256 => Sha3_256::digest(data).to_vec(),
            Self::Sha3_384 => Sha3_384::digest(data).to_vec(),
            Self::Sha3_512 => Sha3_512::digest(data).to_vec(),
            Self::Blake2s256 => Blake2s256::digest(data).to_vec(),
            Self::Blake2b256 => Blake2b::<U32>::digest(data).to_vec(),
            Self::Blake2b384 => Blake2b::<U48>::digest(data).to_vec(),
            Self::Blake2b512 => Blake2b::<U64>::digest(data).to_vec(),
            Self::Blake3 => blake3::hash(data).as_bytes().to_vec(),
        }
    }

    fn digest_len(self) -> usize {
        match self {
            Self::Sha2_224 | Self::Sha2_512_224 | Self::Sha3_224 => 28,
            Self::Sha2_256
            | Self::Sha2_512_256
            | Self::Sha3_256
            | Self::Blake2s256
            | Self::Blake2b256
            | Self::Blake3 => 32,
            Self::Sha2_384 | Self::Sha3_384 | Self::Blake2b384 => 48,
            Self::Sha2_512 | Self::Sha3_512 | Self::Blake2b512 => 64,
        }
    }
}

/// A parsed labeled hash.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LabeledHash {
    alg: Algorithm,
    digest: Vec<u8>,
}

impl LabeledHash {
    /// Digests `data` with `alg`.
    pub fn compute(alg: Algorithm, data: &[u8]) -> Self {
        Self {
            alg,
            digest: alg.digest(data),
        }
    }

    /// Port of `lhash.Load`.
    pub fn load(bytes: &[u8]) -> Result<Self, Error> {
        let (alg_id, n) = pm_dsd::varint::unpack64(bytes)
            .map_err(|e| Error::Hash(format!("failed to parse algorithm ID: {e}")))?;
        let rest = &bytes[n..];
        let (len, n) = pm_dsd::varint::unpack64(rest)
            .map_err(|e| Error::Hash(format!("failed to parse digest: {e}")))?;
        let rest = &rest[n..];
        let len = usize::try_from(len)
            .ok()
            .filter(|&l| l <= rest.len())
            .ok_or_else(|| {
                Error::Hash("failed to parse digest: container: not enough data to return".into())
            })?;
        let (digest, left) = rest.split_at(len);
        if !left.is_empty() {
            return Err(Error::Hash(
                "integrity error: data left over after parsing".into(),
            ));
        }
        let alg = Algorithm::from_id(alg_id).ok_or_else(|| {
            Error::Hash("compatibility error: invalid or unsupported algorithm".into())
        })?;
        if alg.digest_len() != digest.len() {
            return Err(Error::Hash("integrity error: invalid digest length".into()));
        }
        Ok(Self {
            alg,
            digest: digest.to_vec(),
        })
    }

    /// Port of `lhash.FromBase58`.
    pub fn from_base58(s: &str) -> Result<Self, Error> {
        let raw = bs58::decode(s)
            .into_vec()
            .map_err(|e| Error::Hash(format!("failed to decode base58: {e}")))?;
        Self::load(&raw)
    }

    /// Whether the digest of `data` equals this hash.
    pub fn matches(&self, data: &[u8]) -> bool {
        self.alg.digest(data) == self.digest
    }
}
