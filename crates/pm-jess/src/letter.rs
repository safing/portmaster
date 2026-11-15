//! The jess `Letter` as stored in `_jess-signature` (DSD/CBOR) and the
//! verification of its signatures (`Letter.Verify`).

use ciborium::Value;
use ed25519_dalek::{Signature, VerifyingKey};

use crate::lhash::Algorithm;
use crate::signet::{self, TrustStore};
use crate::Error;

/// A signature or key seal.
#[derive(Debug, Default, Clone)]
pub struct Seal {
    pub scheme: String,
    pub id: String,
    pub value: Vec<u8>,
}

/// A jess letter (only the fields relevant for signature verification).
#[derive(Debug, Default, Clone)]
pub struct Letter {
    pub version: u8,
    pub suite_id: String,
    pub nonce: Vec<u8>,
    pub keys: Vec<Seal>,
    pub mac: Vec<u8>,
    pub signatures: Vec<Seal>,
}

fn cbor_err(e: impl std::fmt::Display) -> Error {
    Error::Format(format!("dsd: failed to unpack cbor: {e}"))
}

pub(crate) fn field<'a>(map: &'a [(Value, Value)], name: &str) -> Option<&'a Value> {
    let text = |k: &Value| match k {
        Value::Text(t) => Some(t.clone()),
        _ => None,
    };
    let mut found = None;
    for (k, v) in map {
        if let Some(t) = text(k) {
            if t == name {
                return Some(v);
            }
            if found.is_none() && t.eq_ignore_ascii_case(name) {
                found = Some(v);
            }
        }
    }
    found
}

pub(crate) fn want_text(v: Option<&Value>) -> Result<String, Error> {
    match v {
        None | Some(Value::Null) => Ok(String::new()),
        Some(Value::Text(t)) => Ok(t.clone()),
        Some(_) => Err(cbor_err("expected text")),
    }
}

pub(crate) fn want_bytes(v: Option<&Value>) -> Result<Vec<u8>, Error> {
    match v {
        None | Some(Value::Null) => Ok(Vec::new()),
        Some(Value::Bytes(b)) => Ok(b.clone()),
        Some(_) => Err(cbor_err("expected byte string")),
    }
}

/// Decodes a DSD blob that must use the CBOR format into a CBOR map.
pub(crate) fn load_cbor_map(data: &[u8]) -> Result<Vec<(Value, Value)>, Error> {
    let (format, n) = pm_dsd::varint::unpack8(data).map_err(|e| Error::Format(e.to_string()))?;
    if data.len() <= n {
        return Err(Error::Format(pm_dsd::Error::UnexpectedEof.to_string()));
    }
    if format != pm_dsd::format::CBOR {
        return Err(Error::Format(pm_dsd::Error::IncompatibleFormat.to_string()));
    }
    let mut cursor = std::io::Cursor::new(&data[n..]);
    let value: Value = ciborium::from_reader(&mut cursor).map_err(cbor_err)?;
    if (cursor.position() as usize) != data.len() - n {
        return Err(cbor_err("extraneous data"));
    }
    match value {
        Value::Map(m) => Ok(m),
        _ => Err(cbor_err("expected map")),
    }
}

fn parse_seals(v: Option<&Value>) -> Result<Vec<Seal>, Error> {
    let items = match v {
        None | Some(Value::Null) => return Ok(Vec::new()),
        Some(Value::Array(a)) => a,
        Some(_) => return Err(cbor_err("expected array")),
    };
    items
        .iter()
        .map(|item| match item {
            Value::Map(m) => Ok(Seal {
                scheme: want_text(field(m, "Scheme"))?,
                id: want_text(field(m, "ID"))?,
                value: want_bytes(field(m, "Value"))?,
            }),
            _ => Err(cbor_err("expected map")),
        })
        .collect()
}

impl Letter {
    /// `dsd.Load(data, &Letter{})`.
    pub fn from_dsd(data: &[u8]) -> Result<Self, Error> {
        let map = load_cbor_map(data)?;
        let version = match field(&map, "Version") {
            None | Some(Value::Null) => 0,
            Some(Value::Integer(i)) => {
                u8::try_from(*i).map_err(|_| cbor_err("version overflow"))?
            }
            Some(_) => return Err(cbor_err("expected integer")),
        };
        Ok(Self {
            version,
            suite_id: want_text(field(&map, "SuiteID"))?,
            nonce: want_bytes(field(&map, "Nonce"))?,
            keys: parse_seals(field(&map, "Keys"))?,
            mac: want_bytes(field(&map, "Mac"))?,
            signatures: parse_seals(field(&map, "Signatures"))?,
        })
    }

    fn compile_associated_data(&self) -> Vec<u8> {
        use pm_dsd::varint::{pack64, prepend_length};
        let mut c = Vec::new();
        if self.version > 0 {
            c.extend(pack64(1));
            c.extend(pack64(u64::from(self.version)));
        }
        if !self.suite_id.is_empty() {
            c.extend(pack64(2));
            c.extend(prepend_length(self.suite_id.as_bytes()));
        }
        if !self.nonce.is_empty() {
            c.extend(pack64(3));
            c.extend(prepend_length(&self.nonce));
        }
        if !self.keys.is_empty() {
            c.extend(pack64(4));
            c.extend(pack64(self.keys.len() as u64));
            for (i, seal) in self.keys.iter().enumerate() {
                c.extend(pack64(i as u64));
                if !seal.scheme.is_empty() {
                    c.extend(pack64(16));
                    c.extend(prepend_length(seal.scheme.as_bytes()));
                }
                if !seal.id.is_empty() {
                    c.extend(pack64(17));
                    c.extend(prepend_length(seal.id.as_bytes()));
                }
                if !seal.value.is_empty() {
                    c.extend(pack64(18));
                    c.extend(prepend_length(&seal.value));
                }
            }
        }
        c
    }

    fn compile_associated_signing_data(&self) -> Vec<u8> {
        use pm_dsd::varint::{pack64, prepend_length};
        let mut c = self.compile_associated_data();
        if !self.mac.is_empty() {
            c.extend(pack64(5));
            c.extend(prepend_length(&self.mac));
        }
        c
    }

    /// Port of `Letter.Verify(fileSigRequirements, trustStore)` for the
    /// signing-only suites, with `data` as the signed payload.
    pub fn verify(&self, data: &[u8], trust: &TrustStore) -> Result<(), Error> {
        if self.version == 0 {
            return Err(Error::Verify("letter does not specify version".into()));
        }
        if self.suite_id.is_empty() {
            return Err(Error::Verify("letter does not specify a suite".into()));
        }
        let hash = suite_hash(&self.suite_id)?;

        // Seals with an ID would be recipients or secrets, which a signing
        // suite never uses; Go fails to resolve them in the trust store.
        if let Some(seal) = self.keys.iter().find(|s| !s.id.is_empty()) {
            return Err(Error::Verify(format!(
                "failed to get signet with ID \"{}\" from truststore: could not find signet",
                seal.id
            )));
        }

        // Resolve the senders.
        let mut keys: Vec<[u8; 32]> = Vec::with_capacity(self.signatures.len());
        for seal in &self.signatures {
            if seal.id.is_empty() {
                return Err(Error::Verify(
                    "signets must have a scheme+key or an ID".into(),
                ));
            }
            let Some(trusted) = trust.get_public(&seal.id) else {
                return Err(Error::Verify(format!(
                    "failed to get signet with ID \"{}\" from truststore: could not find signet",
                    seal.id
                )));
            };
            if !seal.scheme.is_empty() && seal.scheme != signet::SCHEME_ED25519 {
                return Err(Error::Verify(format!(
                    "failed to apply signet with ID \"{}\" from truststore: was expected to be of type {}, but is {}",
                    seal.id,
                    seal.scheme,
                    signet::SCHEME_ED25519
                )));
            }
            keys.push(trusted);
        }
        if keys.is_empty() {
            return Err(Error::Verify(
                "tool Ed25519 requires at least one signet".into(),
            ));
        }
        if self.version != 1 {
            return Err(Error::Verify(format!(
                "unsupported letter version: {}",
                self.version
            )));
        }

        let mut signed = data.to_vec();
        signed.extend(self.compile_associated_signing_data());
        let sum = hash.digest(&signed);

        for (seal, key) in self.signatures.iter().zip(&keys) {
            let ok = VerifyingKey::from_bytes(key)
                .ok()
                .zip(Signature::from_slice(&seal.value).ok())
                .is_some_and(|(vk, sig)| vk.verify_strict_compat(&sum, &sig));
            if !ok {
                return Err(Error::Verify(format!(
                    "failed to verify signature (Ed25519) with ID {}: signature invalid",
                    seal.id
                )));
            }
        }
        Ok(())
    }
}

/// Hash of the managed hasher of a signing suite.
fn suite_hash(suite_id: &str) -> Result<Algorithm, Error> {
    match suite_id {
        "sign_v1" => Ok(Algorithm::Blake2b256),
        "signfile_v1" => Ok(Algorithm::Sha2_256),
        "sign_v2" | "signfile_v2" => Ok(Algorithm::Blake3),
        // Registered suites that lack sender authentication.
        "rcpt_v1" | "w1" | "rcpt_v2" | "w2" => Err(Error::Verify(
            "missing security requirements: SenderAuthentication".into(),
        )),
        // Registered suites that need keys or recipients next to signers.
        "key_v1" | "pw_v1" | "v1" | "key_v2" | "pw_v2" | "v2" => Err(Error::Verify(format!(
            "suite {suite_id} is not a signing suite"
        ))),
        _ => Err(Error::Verify(format!("suite {suite_id} does not exist"))),
    }
}

/// Ed25519 verification as in Go `crypto/ed25519` (cofactorless, canonical S).
trait VerifyCompat {
    fn verify_strict_compat(&self, msg: &[u8], sig: &Signature) -> bool;
}

impl VerifyCompat for VerifyingKey {
    fn verify_strict_compat(&self, msg: &[u8], sig: &Signature) -> bool {
        use ed25519_dalek::Verifier;
        self.verify(msg, sig).is_ok()
    }
}
