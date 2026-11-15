use serde::de::DeserializeOwned;
use serde::Serialize;
use serde_json::Value;

use crate::accessor::JsonAccessor;
use crate::{Error, Key, Meta, Result};
use pm_dsd::format::{GEN_CODE, JSON, RAW};
use pm_dsd::{gojson, varint};

/// A record with metadata and a payload in a DSD format (payload excludes the format byte).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Record {
    pub key: Key,
    pub meta: Meta,
    pub format: u8,
    pub data: Vec<u8>,
}

impl Record {
    /// Parses the stored form (version, meta block, format, payload), like Go `NewRawWrapper`.
    pub fn from_raw(db: &str, key: &str, data: &[u8]) -> Result<Record> {
        let (version, mut offset) = varint::unpack8(data)?;
        if version != 1 {
            return Err(Error::IncompatibleVersion(version));
        }
        let (section, n) = varint::next_block(&data[offset..]).map_err(Error::MetaSection)?;
        offset += n;
        let meta = load_meta(section)?;

        let mut format = RAW;
        if !meta.is_deleted() {
            let (f, n) = varint::unpack8(&data[offset..]).map_err(Error::Format)?;
            format = f;
            offset += n;
        }
        Ok(Record {
            key: Key::new(db, key),
            meta,
            format,
            data: data[offset..].to_vec(),
        })
    }

    /// Builds a JSON record from a serializable value.
    pub fn from_typed<T: Serialize>(key: Key, meta: Meta, value: &T) -> Result<Record> {
        Ok(Record {
            key,
            meta,
            format: JSON,
            data: gojson::to_vec(value)?,
        })
    }

    /// Decodes the payload according to its format.
    pub fn decode<T: DeserializeOwned>(&self) -> Result<T> {
        let mut blob = varint::pack8(self.format);
        blob.extend_from_slice(&self.data);
        Ok(pm_dsd::load(&blob)?.0)
    }

    /// Stored form: version 1, meta block, then format byte and payload (omitted when deleted).
    pub fn marshal_record(&self) -> Vec<u8> {
        let mut meta = varint::pack8(GEN_CODE);
        meta.extend(self.meta.gencode_marshal());
        let mut out = vec![1u8];
        out.extend(varint::prepend_length(&meta));
        if !self.meta.is_deleted() {
            out.extend(varint::pack8(self.format));
            out.extend_from_slice(&self.data);
        }
        out
    }

    /// JSON accessor over the payload, if it is non-empty JSON.
    pub fn accessor(&self) -> Option<JsonAccessor> {
        if self.format != JSON || self.data.is_empty() {
            return None;
        }
        JsonAccessor::from_slice(&self.data).ok()
    }

    /// Like Go `api.MarshalRecord`: the JSON payload with `_meta` (including `Key`) as last field.
    pub fn to_json_with_meta(&self, with_dsd_id: bool) -> Result<Vec<u8>> {
        let payload: &[u8] = if self.meta.is_deleted() {
            &[]
        } else if self.format == JSON {
            &self.data
        } else {
            return Err(Error::FormatMismatch);
        };

        let mut meta_json = gojson::to_vec(&self.meta)?;
        meta_json.pop();
        meta_json.extend_from_slice(b",\"Key\":");
        meta_json.extend(sjson_string(&self.key.to_string()));
        meta_json.push(b'}');

        let mut out = if with_dsd_id {
            varint::pack8(JSON)
        } else {
            Vec::new()
        };
        let trimmed = payload.trim_ascii();
        if trimmed.is_empty() {
            out.extend_from_slice(b"{\"_meta\":");
            out.extend(&meta_json);
            out.push(b'}');
            return Ok(out);
        }
        let Value::Object(mut obj) = serde_json::from_slice::<Value>(trimmed)? else {
            return Err(Error::NotJsonObject);
        };
        if obj.contains_key("_meta") {
            // Replace in place; Key is escaped by Go json here.
            obj.insert("_meta".into(), serde_json::from_slice(&meta_json)?);
            out.extend(gojson::to_vec(&obj)?);
            return Ok(out);
        }
        // Append textually, like sjson, to keep the payload bytes untouched.
        let body = &trimmed[..trimmed.len() - 1];
        out.extend_from_slice(body);
        if !body.trim_ascii_end().ends_with(b"{") {
            out.push(b',');
        }
        out.extend_from_slice(b"\"_meta\":");
        out.extend(&meta_json);
        out.push(b'}');
        Ok(out)
    }
}

fn load_meta(section: &[u8]) -> Result<Meta> {
    let (format, n) = varint::unpack8(section).map_err(Error::MetaUnmarshal)?;
    if section.len() <= n {
        return Err(Error::MetaUnmarshal(pm_dsd::Error::UnexpectedEof));
    }
    if format == GEN_CODE {
        return Meta::gencode_unmarshal(&section[n..]);
    }
    pm_dsd::load::<Meta>(section)
        .map(|(m, _)| m)
        .map_err(Error::MetaUnmarshal)
}

/// Mimics sjson string encoding: plain ASCII is written verbatim (no HTML
/// escaping), anything needing escaping goes through Go `json.Marshal`.
fn sjson_string(s: &str) -> Vec<u8> {
    if s.bytes()
        .any(|b| !(b' '..=0x7f).contains(&b) || b == b'"' || b == b'\\')
    {
        return gojson::to_vec(s).unwrap_or_else(|_| b"\"\"".to_vec());
    }
    let mut out = Vec::with_capacity(s.len() + 2);
    out.push(b'"');
    out.extend_from_slice(s.as_bytes());
    out.push(b'"');
    out
}
