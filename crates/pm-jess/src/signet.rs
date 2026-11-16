//! Signets (public keys) and the in-memory trust store.

use std::collections::HashMap;

use ciborium::Value;

use crate::letter::{field, load_cbor_map, want_bytes, want_text};
use crate::Error;

/// Scheme (tool name) of Ed25519 signets.
pub const SCHEME_ED25519: &str = "Ed25519";

const RECIPIENT_PREFIX: &str = "recipient:";

/// A loaded Ed25519 signet.
#[derive(Debug, Clone)]
struct Signet {
    id: String,
    public: bool,
    /// Raw public key bytes.
    key: [u8; 32],
}

/// Port of `jess.MemTrustStore` for Ed25519 recipient signets.
#[derive(Debug, Default, Clone)]
pub struct TrustStore {
    /// Keyed like Go: the ID plus whether the signet is public.
    storage: HashMap<(String, bool), Signet>,
}

impl TrustStore {
    /// An empty trust store.
    pub fn new() -> Self {
        Self::default()
    }

    /// Parses signets in text format (`jess.RecipientFromTextFormat`) and
    /// stores them, like `configure.loadSigningKeys`.
    pub fn from_signet_strings(keys: &[&str]) -> Result<Self, Error> {
        let mut ts = Self::new();
        for key in keys {
            let signet = recipient_from_text(key)?;
            if signet.id.is_empty() {
                return Err(Error::Signet(
                    "signets require an ID to be stored in a trust store".into(),
                ));
            }
            ts.storage
                .insert((signet.id.clone(), signet.public), signet);
        }
        Ok(ts)
    }

    /// Number of stored signets.
    pub fn len(&self) -> usize {
        self.storage.len()
    }

    /// Whether the store is empty.
    pub fn is_empty(&self) -> bool {
        self.storage.is_empty()
    }

    /// `GetSignet(id, recipient=true)`: the raw public key.
    pub(crate) fn get_public(&self, id: &str) -> Option<[u8; 32]> {
        self.storage.get(&(id.to_string(), true)).map(|s| s.key)
    }

    /// Stores a public Ed25519 key, like `StoreSignet` does for a recipient.
    pub(crate) fn insert_public(&mut self, id: &str, key: [u8; 32]) {
        self.storage.insert(
            (id.to_string(), true),
            Signet {
                id: id.to_string(),
                public: true,
                key,
            },
        );
    }
}

fn recipient_from_text(text: &str) -> Result<Signet, Error> {
    if !text.starts_with(RECIPIENT_PREFIX) {
        return Err(Error::Signet("not a recipient".into()));
    }
    let last = text.rsplit(':').next().unwrap_or_default();
    if text.split(':').count() < 2 {
        return Err(Error::Signet("invalid format".into()));
    }
    signet_from_base58(last)
}

fn signet_from_base58(s: &str) -> Result<Signet, Error> {
    let data = bs58::decode(s)
        .into_vec()
        .map_err(|e| Error::Signet(format!("failed to decode base58: {e}")))?;
    signet_from_bytes(&data)
}

/// A signet as it is stored on disk, before the loaded key is extracted.
#[derive(Debug, Clone)]
pub(crate) struct StoredSignet {
    pub id: String,
    pub scheme: String,
    pub key: Vec<u8>,
    pub public: bool,
    pub protected: bool,
}

impl StoredSignet {
    fn check(&self) -> Result<(), Error> {
        if self.id.is_empty() {
            return Err(Error::Signet("signet has no ID".into()));
        }
        if self.protected {
            return Err(Error::Signet("failed to parse key: key is protected".into()));
        }
        if self.scheme != SCHEME_ED25519 {
            return Err(Error::Signet(format!(
                "failed to parse key: unsupported scheme {:?} (only Ed25519 is supported)",
                self.scheme
            )));
        }
        Ok(())
    }

    /// The raw 32 byte public key, from `Key`'s serialized container.
    pub(crate) fn public_key(&self) -> Result<[u8; 32], Error> {
        self.check()?;
        ed25519_public_key(&self.key)
    }

    /// The raw 64 byte private key, from `Key`'s serialized container.
    pub(crate) fn private_key(&self) -> Result<[u8; 64], Error> {
        self.check()?;
        if self.public {
            return Err(Error::Signet("signet is not a private key".into()));
        }
        ed25519_private_key(&self.key)
    }
}

fn invalid_key() -> Error {
    Error::Signet("failed to parse key: invalid key".into())
}

/// Unwraps the version byte of an Ed25519 stored key.
fn key_bytes(stored: &[u8]) -> Result<&[u8], Error> {
    let (version, n) = pm_dsd::varint::unpack8(stored).map_err(|_| invalid_key())?;
    if version != 1 || stored.len() <= n {
        return Err(invalid_key());
    }
    Ok(&stored[n..])
}

pub(crate) fn ed25519_public_key(stored: &[u8]) -> Result<[u8; 32], Error> {
    key_bytes(stored)?
        .try_into()
        .map_err(|_| invalid_key())
}

pub(crate) fn ed25519_private_key(stored: &[u8]) -> Result<[u8; 64], Error> {
    key_bytes(stored)?
        .try_into()
        .map_err(|_| invalid_key())
}

/// Parses a DSD CBOR signet, like `SignetFromBase58` does.
pub(crate) fn stored_from_cbor(data: &[u8]) -> Result<StoredSignet, Error> {
    let map = load_cbor_map(data)
        .map_err(|e| Error::Signet(format!("failed to parse data format: {e}")))?;
    let parse = |e: Error| Error::Signet(format!("failed to parse data format: {e}"));
    Ok(StoredSignet {
        id: want_text(field(&map, "ID")).map_err(parse)?,
        scheme: want_text(field(&map, "Scheme")).map_err(parse)?,
        key: want_bytes(field(&map, "Key")).map_err(parse)?,
        public: match field(&map, "Public") {
            None | Some(Value::Null) => false,
            Some(Value::Bool(b)) => *b,
            Some(_) => return Err(parse(Error::Format("expected bool".into()))),
        },
        protected: !matches!(field(&map, "Protection"), None | Some(Value::Null)),
    })
}

/// Parses the JSON files written by `truststores.WriteSignetToFile`.
pub(crate) fn stored_from_json(value: &serde_json::Value) -> Result<StoredSignet, Error> {
    use serde_json::Value as Json;
    let parse = |e: Error| Error::Signet(format!("failed to parse data format: {e}"));
    let text = |name: &str| -> Result<String, Error> {
        match value.get(name) {
            None | Some(Json::Null) => Ok(String::new()),
            Some(Json::String(s)) => Ok(s.clone()),
            Some(_) => Err(Error::Format(format!("expected text for {name}"))),
        }
    };
    let key = match value.get("Key") {
        None | Some(Json::Null) => Vec::new(),
        Some(Json::String(s)) => base64::Engine::decode(&crate::STD, s)
            .map_err(|e| parse(Error::Format(format!("invalid base64 key: {e}"))))?,
        Some(_) => return Err(parse(Error::Format("expected text for Key".into()))),
    };
    Ok(StoredSignet {
        id: text("ID").map_err(parse)?,
        scheme: text("Scheme").map_err(parse)?,
        key,
        public: match value.get("Public") {
            Some(Json::Bool(b)) => *b,
            None | Some(Json::Null) => false,
            Some(_) => return Err(parse(Error::Format("expected bool for Public".into()))),
        },
        protected: !matches!(value.get("Protection"), None | Some(Json::Null)),
    })
}

fn signet_from_bytes(data: &[u8]) -> Result<Signet, Error> {
    let stored = stored_from_cbor(data)?;
    let key = stored.public_key()?;
    Ok(Signet {
        id: stored.id,
        public: stored.public,
        key,
    })
}
