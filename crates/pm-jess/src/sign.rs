//! Creation of jess JSON signatures, compatible with Go
//! `filesig.AddJSONSignature`.
//!
//! Only the signing-only suites (`sign_v1`, `signfile_v1`, `sign_v2`,
//! `signfile_v2`) with Ed25519 sender signets are supported. The nonce is
//! random, so signatures are not byte-identical to Go's; they are verified
//! with Go's `filesig.VerifyJSONSignature` instead.

use std::path::{Path, PathBuf};

use ciborium::Value;
use ed25519_dalek::{Signer, SigningKey};
use serde_json::Value as Json;

use crate::letter::{field, load_cbor_map, suite_hash, want_text};
use crate::pretty::{pretty_options, Options};
use crate::signet::{stored_from_cbor, stored_from_json, StoredSignet};
use crate::{encode_raw_url, json_split, Error, Letter, Seal, TrustStore, SIGNATURE_KEY};

const ED25519: &str = "Ed25519";
const ENVELOPE_SUFFIX: &str = ".envelope";
const SIGNET_SUFFIX: &str = ".signet";
const RECIPIENT_SUFFIX: &str = ".recipient";
const NONCE_SIZE: usize = 4; // Session.NonceSize(): 128-bit security / 32.

/// A signet reference inside an envelope. When signing, the referenced
/// private signet is fetched from the trust store by ID.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SignetReference {
    pub version: u8,
    pub id: String,
    pub scheme: String,
}

/// A jess envelope, as stored in a `<name>.envelope` file.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Envelope {
    pub version: u8,
    pub name: String,
    pub suite_id: String,
    pub senders: Vec<SignetReference>,
    pub secrets: Vec<SignetReference>,
    pub recipients: Vec<SignetReference>,
}

impl Envelope {
    /// Parses the DSD JSON written by `truststores.WriteEnvelopeToFile`.
    pub fn from_json(data: &[u8]) -> Result<Self, Error> {
        let value: Json = serde_json::from_slice(json_payload(data))
            .map_err(|e| Error::Format(format!("failed to parse data format: {e}")))?;
        Self::from_value(&value)
    }

    fn from_value(value: &Json) -> Result<Self, Error> {
        let parse = |e: Error| Error::Format(format!("failed to parse data format: {e}"));
        let number = |name: &str| -> Result<u8, Error> {
            match value.get(name) {
                None | Some(Json::Null) => Ok(0),
                Some(Json::Number(n)) => u8::try_from(n.as_u64().unwrap_or(256))
                    .map_err(|_| Error::Format(format!("invalid {name}"))),
                Some(_) => Err(Error::Format(format!("expected integer for {name}"))),
            }
        };
        let text = |name: &str| -> Result<String, Error> {
            match value.get(name) {
                None | Some(Json::Null) => Ok(String::new()),
                Some(Json::String(s)) => Ok(s.clone()),
                Some(_) => Err(Error::Format(format!("expected text for {name}"))),
            }
        };
        let refs = |name: &str| -> Result<Vec<SignetReference>, Error> {
            let Some(Json::Array(items)) = value.get(name) else {
                return Ok(Vec::new());
            };
            items.iter().map(signet_ref_from_value).collect()
        };
        let envelope = Self {
            version: number("Version").map_err(parse)?,
            name: text("Name").map_err(parse)?,
            suite_id: text("SuiteID").map_err(parse)?,
            senders: refs("Senders").map_err(parse)?,
            secrets: refs("Secrets").map_err(parse)?,
            recipients: refs("Recipients").map_err(parse)?,
        };
        envelope.check_suite()?;
        Ok(envelope)
    }

    /// Parses an `envelope:<suite>:<name>:<base58 DSD CBOR>` string, as
    /// written by `Envelope.Export` / the keyring truststore.
    pub fn from_text(text: &str) -> Result<Self, Error> {
        let data = text.trim();
        let Some(body) = data.strip_prefix("envelope:") else {
            return Err(Error::Format("not an envelope".into()));
        };
        let encoded = body.rsplit(':').next().unwrap_or_default();
        let raw = bs58::decode(encoded)
            .into_vec()
            .map_err(|e| Error::Format(format!("failed to decode base58: {e}")))?;
        let map = load_cbor_map(&raw)?;
        let refs = |name: &str| -> Result<Vec<SignetReference>, Error> {
            let Some(Value::Array(items)) = field(&map, name) else {
                return Ok(Vec::new());
            };
            items
                .iter()
                .map(|item| match item {
                    Value::Map(m) => Ok(SignetReference {
                        version: match field(m, "Version") {
                            Some(Value::Integer(i)) => u8::try_from(*i).unwrap_or(0),
                            _ => 0,
                        },
                        id: want_text(field(m, "ID"))?,
                        scheme: want_text(field(m, "Scheme"))?,
                    }),
                    _ => Err(Error::Format("expected signet map".into())),
                })
                .collect()
        };
        let envelope = Self {
            version: match field(&map, "Version") {
                Some(Value::Integer(i)) => u8::try_from(*i).unwrap_or(0),
                _ => 0,
            },
            name: want_text(field(&map, "Name"))?,
            suite_id: want_text(field(&map, "SuiteID"))?,
            senders: refs("Senders")?,
            secrets: refs("Secrets")?,
            recipients: refs("Recipients")?,
        };
        envelope.check_suite()?;
        Ok(envelope)
    }

    fn check_suite(&self) -> Result<(), Error> {
        suite_hash(&self.suite_id)?;
        if self.version == 0 {
            return Err(Error::Format("envelope does not specify version".into()));
        }
        Ok(())
    }
}

fn signet_ref_from_value(value: &Json) -> Result<SignetReference, Error> {
    let number = match value.get("Version") {
        None | Some(Json::Null) => 0,
        Some(Json::Number(n)) => u8::try_from(n.as_u64().unwrap_or(256))
            .map_err(|_| Error::Format("invalid signet version".to_string()))?,
        Some(_) => return Err(Error::Format("expected integer for Version".into())),
    };
    let text = |name: &str| -> Result<String, Error> {
        match value.get(name) {
            None | Some(Json::Null) => Ok(String::new()),
            Some(Json::String(s)) => Ok(s.clone()),
            Some(_) => Err(Error::Format(format!("expected text for {name}"))),
        }
    };
    Ok(SignetReference {
        version: number,
        id: text("ID")?,
        scheme: text("Scheme")?,
    })
}

/// Directory trust store, the format of Go's `truststores.DirTrustStore`:
/// JSON files named `<id>.signet`, `<id>.recipient` and `<name>.envelope`.
#[derive(Debug, Clone)]
pub struct DirTrustStore {
    dir: PathBuf,
}

impl DirTrustStore {
    /// Opens a trust store directory, like `NewDirTrustStore`.
    pub fn new(path: impl AsRef<Path>) -> Result<Self, Error> {
        let dir = path.as_ref().to_path_buf();
        if !dir.is_dir() {
            return Err(Error::Signet(format!(
                "trust store does not exist: {}",
                dir.display()
            )));
        }
        Ok(Self { dir })
    }

    /// Loads an envelope, like `DirTrustStore.GetEnvelope`.
    pub fn get_envelope(&self, name: &str) -> Result<Envelope, Error> {
        check_name(name)?;
        let path = self.dir.join(format!("{name}{ENVELOPE_SUFFIX}"));
        let data = std::fs::read(&path).map_err(|e| {
            Error::Format(format!("failed to read envelope {name}: {e}"))
        })?;
        Envelope::from_json(&data)
    }

    /// Builds a verification trust store from every `.recipient` file in the
    /// directory. Useful for checking a signature just written by this crate.
    pub fn verification_trust_store(&self) -> Result<TrustStore, Error> {
        let mut ts = TrustStore::new();
        for entry in std::fs::read_dir(&self.dir)
            .map_err(|e| Error::Signet(format!("failed to list trust store: {e}")))?
        {
            let entry = entry.map_err(|e| Error::Signet(format!("failed to list trust store: {e}")))?;
            let name = entry.file_name();
            let name = name.to_string_lossy();
            if !name.ends_with(RECIPIENT_SUFFIX) {
                continue;
            }
            let id = name.trim_end_matches(RECIPIENT_SUFFIX).to_string();
            let data = std::fs::read(entry.path())
                .map_err(|e| Error::Signet(format!("failed to read signet {id}: {e}")))?;
            let stored = if data.first() == Some(&b'{') || data.first() == Some(&b'J') {
                let value: Json = serde_json::from_slice(json_payload(&data))
                    .map_err(|e| Error::Signet(format!("failed to parse data format: {e}")))?;
                stored_from_json(&value)?
            } else {
                signet_from_text(&String::from_utf8_lossy(&data))?
            };
            // Go looks signets up by the requested ID, not by the ID stored
            // inside the file, so key the store by the filename stem.
            ts.insert_public(&id, stored.public_key()?);
        }
        Ok(ts)
    }

    /// Loads a signet. Both DSD JSON (the `DirTrustStore` format) and
    /// `sender:` / `recipient:` text (the keyring format) are accepted.
    fn get_signet_json(&self, id: &str, recipient: bool) -> Result<StoredSignet, Error> {
        let suffix = if recipient {
            RECIPIENT_SUFFIX
        } else {
            SIGNET_SUFFIX
        };
        check_name(id)?;
        let path = self.dir.join(format!("{id}{suffix}"));
        let data = std::fs::read(&path)
            .map_err(|e| Error::Signet(format!("failed to read signet {id}: {e}")))?;
        if data.first() == Some(&b'{') || data.first() == Some(&b'J') {
            let value: Json = serde_json::from_slice(json_payload(&data))
                .map_err(|e| Error::Signet(format!("failed to parse data format: {e}")))?;
            stored_from_json(&value)
        } else {
            signet_from_text(&String::from_utf8_lossy(&data))
        }
    }
}

/// Names used as filenames must not contain path separators (Go's
/// `NamePlaysNiceWithFS`, restricted to the safe subset).
fn check_name(name: &str) -> Result<(), Error> {
    const ALLOWED: &str = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789- ._+@";
    if name.is_empty() || name.chars().any(|c| !ALLOWED.contains(c)) {
        return Err(Error::Format(format!(
            "this trust store only allows these characters in names: {ALLOWED}"
        )));
    }
    Ok(())
}

/// DSD writes a one byte format identifier before the serialized JSON.
fn json_payload(data: &[u8]) -> &[u8] {
    match data.first() {
        Some(b) if *b != b'{' && !b.is_ascii_whitespace() => &data[1..],
        _ => data,
    }
}

fn signet_from_text(text: &str) -> Result<StoredSignet, Error> {
    let text = text.trim();
    if !(text.starts_with("sender:")
        || text.starts_with("recipient:")
        || text.starts_with("secret:"))
    {
        return Err(Error::Signet("not a signet".into()));
    }
    let encoded = text.rsplit(':').next().unwrap_or_default();
    let raw = bs58::decode(encoded)
        .into_vec()
        .map_err(|e| Error::Signet(format!("failed to decode base58: {e}")))?;
    stored_from_cbor(&raw)
}

/// Port of `filesig.AddJSONSignature` for the supported suites.
pub fn add_json_signature(
    data: &[u8],
    envelope: &Envelope,
    ts: &DirTrustStore,
) -> Result<Vec<u8>, Error> {
    envelope.check_suite()?;
    if !envelope.secrets.is_empty() {
        return Err(Error::Format(
            "envelope contains secrets, which signing suites do not use".into(),
        ));
    }
    if !envelope.recipients.is_empty() {
        return Err(Error::Format(
            "envelope contains recipients, which signing suites do not use".into(),
        ));
    }
    if envelope.senders.is_empty() {
        return Err(Error::Format("tool Ed25519 requires at least one signet".into()));
    }

    let mut signers: Vec<(String, SigningKey)> = Vec::with_capacity(envelope.senders.len());
    for sender in &envelope.senders {
        if !sender.scheme.is_empty() && sender.scheme != ED25519 {
            return Err(Error::Format(format!(
                "failed to apply signet with ID {:?} from truststore: was expected to be of type {}, but is {}",
                sender.id, ED25519, sender.scheme
            )));
        }
        if sender.id.is_empty() {
            return Err(Error::Format(
                "signets must have a scheme+key or an ID".into(),
            ));
        }
        let stored = ts.get_signet_json(&sender.id, false)?;
        let key = stored.private_key()?;
        let seed: [u8; 32] = key[..32]
            .try_into()
            .expect("a 64 byte private key always has a 32 byte seed");
        signers.push((sender.id.clone(), SigningKey::from_bytes(&seed)));
    }

    let split = json_split(data).ok_or(Error::InvalidJsonStructure)?;

    // Session.Close: Version/SuiteID from the envelope, a random nonce and
    // no keys or MAC (signing suites have no cipher or KDF).
    let mut nonce = [0u8; NONCE_SIZE];
    getrandom::fill(&mut nonce).map_err(|e| Error::Format(format!("failed to get nonce: {e}")))?;
    let letter = Letter {
        version: envelope.version,
        suite_id: envelope.suite_id.clone(),
        nonce: nonce.to_vec(),
        keys: Vec::new(),
        mac: Vec::new(),
        signatures: Vec::new(),
    };

    let mut signed = split.content.clone();
    signed.extend(letter.compile_associated_signing_data());
    let digest = suite_hash(&envelope.suite_id)?.digest(&signed);

    let mut letter = letter;
    for (id, key) in signers {
        let signature = key.sign(&digest);
        letter.signatures.push(Seal {
            scheme: ED25519.into(),
            id,
            value: signature.to_bytes().to_vec(),
        });
    }

    let mut signatures = split.signatures;
    signatures.push(encode_raw_url(&letter.to_dsd()?));
    signatures.sort();
    signatures.dedup();

    let mut checksums = split.checksums;
    checksums.sort();
    checksums.dedup();

    json_add_meta(&split.content, &checksums, &signatures)
}

/// Port of `filesig.jsonAddMeta`. The canonical content already has the
/// metadata removed; the members are appended to the root object, then the
/// document is pretty printed without sorting (as Go does in the last step).
fn json_add_meta(
    content: &[u8],
    checksums: &[String],
    signatures: &[String],
) -> Result<Vec<u8>, Error> {
    let encode = |values: &[String]| -> Result<String, Error> {
        if values.len() == 1 {
            Ok(serde_json::to_string(&values[0])
                .map_err(|e| Error::Format(format!("failed to encode metadata: {e}")))?)
        } else {
            serde_json::to_string(values)
                .map_err(|e| Error::Format(format!("failed to encode metadata: {e}")))
        }
    };
    let mut members = Vec::new();
    if !checksums.is_empty() {
        members.push((crate::CHECKSUM_KEY, encode(checksums)?));
    }
    if !signatures.is_empty() {
        members.push((SIGNATURE_KEY, encode(signatures)?));
    }
    insert_members(content, &members)
}

fn insert_members(content: &[u8], members: &[(&str, String)]) -> Result<Vec<u8>, Error> {
    let Some((_, close)) = object_bounds(content) else {
        return Err(Error::InvalidJsonStructure);
    };
    // The canonical content ends with a newline before the closing brace.
    // Drop trailing whitespace from the head and re-attach it after the
    // inserted members.
    let mut head = content[..close].to_vec();
    while head
        .last()
        .is_some_and(|b| b.is_ascii_whitespace())
    {
        head.pop();
    }
    let had_members = head.last() != Some(&b'{');
    for (key, value) in members {
        if had_members {
            head.push(b',');
        }
        head.extend_from_slice(b"\n \"");
        head.extend_from_slice(key.as_bytes());
        head.extend_from_slice(b"\": ");
        head.extend_from_slice(value.as_bytes());
    }
    head.push(b'\n');
    head.extend_from_slice(&content[close..]);
    Ok(pretty_options(&head, &Options::final_print()))
}

/// Location of the braces of a root JSON object in canonical content.
fn object_bounds(data: &[u8]) -> Option<(usize, usize)> {
    let mut i = 0;
    while i < data.len() && data[i].is_ascii_whitespace() {
        i += 1;
    }
    if data.get(i) != Some(&b'{') {
        return None;
    }
    let open = i;
    let mut depth = 0usize;
    let mut in_string = false;
    let mut escaped = false;
    while i < data.len() {
        let c = data[i];
        if in_string {
            if escaped {
                escaped = false;
            } else if c == b'\\' {
                escaped = true;
            } else if c == b'"' {
                in_string = false;
            }
        } else {
            match c {
                b'"' => in_string = true,
                b'{' | b'[' => depth += 1,
                b'}' | b']' => {
                    depth -= 1;
                    if depth == 0 {
                        return Some((open, i));
                    }
                }
                _ => {}
            }
        }
        i += 1;
    }
    None
}
