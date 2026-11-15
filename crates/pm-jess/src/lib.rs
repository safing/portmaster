//! Verification of jess JSON signatures and checksums, byte-compatible with
//! Go `filesig.VerifyJSONSignature` / `filesig.VerifyJSONChecksum`.
//!
//! Only verification is implemented, for the signing-only suites
//! (`sign_v1`, `signfile_v1`, `sign_v2`, `signfile_v2`) with Ed25519 signets.
//! Anything else is rejected (fail closed).
//!
//! To use it as an `IndexVerifier` of `pm-updates`, see [`index_verifier`].

mod json;
mod keys;
mod letter;
pub mod lhash;
pub mod pretty;
mod signet;

use base64::engine::general_purpose::{GeneralPurpose, GeneralPurposeConfig};
use base64::engine::DecodePaddingMode;
use base64::{alphabet, Engine};

pub use json::{json_split, Split, CHECKSUM_KEY, SIGNATURE_KEY};
pub use keys::{BINARY_SIGNING_KEYS, INTEL_SIGNING_KEYS};
pub use letter::{Letter, Seal};
pub use signet::TrustStore;

/// Errors; the texts mirror the Go ones.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum Error {
    #[error("invalid json")]
    InvalidJson,
    #[error("invalid json structure: invalid json")]
    InvalidJsonStructure,
    #[error("signature not found")]
    SignatureMissing,
    #[error("no checksum found")]
    ChecksumMissing,
    #[error("checksum does not match")]
    ChecksumFailed,
    #[error("checksum does not match: failed to parse labeled hash: {0}")]
    ChecksumMalformed(String),
    #[error("signature {index} malformed: {reason}")]
    SignatureMalformed { index: usize, reason: String },
    #[error("signature {index} invalid: {reason}")]
    SignatureInvalid { index: usize, reason: String },
    #[error("{0}")]
    Hash(String),
    #[error("{0}")]
    Format(String),
    #[error("{0}")]
    Signet(String),
    #[error("{0}")]
    Verify(String),
}

/// Go `base64.RawURLEncoding`: no padding, trailing bits not checked.
const RAW_URL: GeneralPurpose = GeneralPurpose::new(
    &alphabet::URL_SAFE,
    GeneralPurposeConfig::new()
        .with_encode_padding(false)
        .with_decode_padding_mode(DecodePaddingMode::RequireNone)
        .with_decode_allow_trailing_bits(true),
);

fn decode_raw_url(s: &str) -> Result<Vec<u8>, String> {
    // Go ignores line breaks.
    let cleaned: Vec<u8> = s.bytes().filter(|b| *b != b'\r' && *b != b'\n').collect();
    RAW_URL.decode(&cleaned).map_err(|e| {
        let offset = match e {
            base64::DecodeError::InvalidByte(o, _)
            | base64::DecodeError::InvalidLastSymbol(o, _) => o,
            _ => cleaned.len(),
        };
        format!("illegal base64 data at input byte {offset}")
    })
}

/// Port of `filesig.VerifyJSONSignature`: every signature must verify against
/// the trust store and at least one must be present.
pub fn verify_json_signature(data: &[u8], ts: &TrustStore) -> Result<(), Error> {
    let split = json_split(data).ok_or(Error::InvalidJsonStructure)?;
    let mut verified = 0;
    for (i, sig) in split.signatures.iter().enumerate() {
        let index = i + 1;
        let raw =
            decode_raw_url(sig).map_err(|reason| Error::SignatureMalformed { index, reason })?;
        let letter = Letter::from_dsd(&raw).map_err(|e| Error::SignatureMalformed {
            index,
            reason: e.to_string(),
        })?;
        letter
            .verify(&split.content, ts)
            .map_err(|e| Error::SignatureInvalid {
                index,
                reason: e.to_string(),
            })?;
        verified += 1;
    }
    if verified == 0 {
        return Err(Error::SignatureMissing);
    }
    Ok(())
}

/// Port of `filesig.VerifyJSONChecksum`.
pub fn verify_json_checksum(data: &[u8]) -> Result<(), Error> {
    let split = json_split(data).ok_or(Error::InvalidJson)?;
    let mut verified = 0;
    for checksum in &split.checksums {
        let hash = lhash::LabeledHash::from_base58(checksum)
            .map_err(|e| Error::ChecksumMalformed(e.to_string()))?;
        if !hash.matches(&split.content) {
            return Err(Error::ChecksumFailed);
        }
        verified += 1;
    }
    if verified == 0 {
        return Err(Error::ChecksumMissing);
    }
    Ok(())
}

/// Trust store with the intel signing keys (`configure.IntelSigningTrustStore`).
pub fn intel_trust_store() -> TrustStore {
    TrustStore::from_signet_strings(INTEL_SIGNING_KEYS).expect("compiled-in intel keys are valid")
}

/// Trust store with the code signing keys (`configure.BinarySigningTrustStore`).
pub fn binary_trust_store() -> TrustStore {
    TrustStore::from_signet_strings(BINARY_SIGNING_KEYS).expect("compiled-in binary keys are valid")
}

/// Adapts [`verify_json_signature`] to the `IndexVerifier` signature of
/// `pm-updates`:
///
/// ```ignore
/// let ts = pm_jess::intel_trust_store();
/// let verify = pm_jess::index_verifier(&ts);
/// Index::parse(&data, platform, Some(&verify))?;
/// ```
pub fn index_verifier(ts: &TrustStore) -> impl Fn(&[u8]) -> Result<(), String> + '_ {
    move |data| verify_json_signature(data, ts).map_err(|e| e.to_string())
}
