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

fn signet_from_bytes(data: &[u8]) -> Result<Signet, Error> {
    let map = load_cbor_map(data)
        .map_err(|e| Error::Signet(format!("failed to parse data format: {e}")))?;
    let parse = |e: Error| Error::Signet(format!("failed to parse data format: {e}"));
    let id = want_text(field(&map, "ID")).map_err(parse)?;
    let scheme = want_text(field(&map, "Scheme")).map_err(parse)?;
    let stored = want_bytes(field(&map, "Key")).map_err(parse)?;
    let public = match field(&map, "Public") {
        None | Some(Value::Null) => false,
        Some(Value::Bool(b)) => *b,
        Some(_) => return Err(parse(Error::Format("expected bool".into()))),
    };
    let protected = !matches!(field(&map, "Protection"), None | Some(Value::Null));

    // LoadKey.
    let invalid = || Error::Signet("failed to parse key: invalid key".into());
    if protected {
        return Err(Error::Signet(
            "failed to parse key: key is protected".into(),
        ));
    }
    if scheme != SCHEME_ED25519 {
        return Err(Error::Signet(format!(
            "failed to parse key: unsupported scheme {scheme:?} (only Ed25519 is supported)"
        )));
    }
    let (version, n) = pm_dsd::varint::unpack8(&stored).map_err(|_| invalid())?;
    if version != 1 {
        return Err(invalid());
    }
    let rest = &stored[n..];
    let key: [u8; 32] = if public {
        rest.try_into().map_err(|_| invalid())?
    } else {
        // Private keys are 64 bytes: seed followed by the public key.
        if rest.len() != 64 {
            return Err(invalid());
        }
        rest[32..].try_into().map_err(|_| invalid())?
    };
    Ok(Signet { id, public, key })
}
