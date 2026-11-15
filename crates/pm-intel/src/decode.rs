//! Decoding of DSDL filterlist files, a port of Go
//! `service/intel/filterlists/decoder.go`.

use std::io::Read;

use flate2::read::GzDecoder;
use serde::{Deserialize, Serialize};

use crate::Error;

/// A resource that contains a filter list entry.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct EntryResource {
    #[serde(rename = "sourceID")]
    pub source_id: String,
    #[serde(rename = "resourceID")]
    pub resource_id: String,
}

/// A single filter list entry.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ListEntry {
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(rename = "entity")]
    pub entity: String,
    #[serde(rename = "whitelist")]
    pub whitelist: bool,
    #[serde(
        rename = "resources",
        deserialize_with = "crate::deserialize_null_default"
    )]
    pub resources: Vec<EntryResource>,
}

impl ListEntry {
    /// Returns the unique source IDs of the entry, in order of appearance.
    pub fn sources(&self) -> Vec<String> {
        let mut sources = Vec::with_capacity(self.resources.len());
        for resource in &self.resources {
            if !sources.contains(&resource.source_id) {
                sources.push(resource.source_id.clone());
            }
        }
        sources
    }
}

/// Reads the DSDL list header, like Go `parseHeader`.
///
/// Returns whether the content is gzip-compressed and the format of the entries.
pub fn parse_header(reader: &mut impl Read) -> Result<(bool, u8), Error> {
    let mut header = [0u8; 1];
    reader.read_exact(&mut header)?;
    if header[0] != pm_dsd::format::LIST {
        return Err(Error::Decode(format!(
            "unexpected file type: {} ({}), expected dsd list",
            header[0], header[0] as char
        )));
    }

    reader.read_exact(&mut header)?;
    if header[0] == pm_dsd::format::GZIP {
        reader.read_exact(&mut header)?;
        return Ok((true, header[0]));
    }

    Ok((false, header[0]))
}

/// Decodes a DSDL filterlist file, like Go `decodeFile`.
///
/// The Go implementation streams entries through a channel; this returns all
/// entries at once.
pub fn decode_file(mut reader: impl Read) -> Result<Vec<ListEntry>, Error> {
    let (compressed, format) = parse_header(&mut reader)?;
    if format != pm_dsd::format::JSON {
        return Err(Error::Decode(format!(
            "unsupported DSD format: {} ({}), expected JSON",
            format, format as char
        )));
    }

    let mut data = Vec::new();
    if compressed {
        GzDecoder::new(reader).read_to_end(&mut data)?;
    } else {
        reader.read_to_end(&mut data)?;
    }

    let mut entries = Vec::new();
    let mut remaining = data.as_slice();
    loop {
        let Some(length) = read_uvarint(&mut remaining)? else {
            return Ok(entries);
        };
        let length = usize::try_from(length)
            .map_err(|_| Error::Decode(format!("entity length out of range: {length}")))?;
        if remaining.len() < length {
            return Err(Error::Decode(format!(
                "unexpected end of file: entity needs {length} bytes, {} left",
                remaining.len()
            )));
        }

        let (blob, rest) = remaining.split_at(length);
        remaining = rest;
        let entry: ListEntry = serde_json::from_slice(blob)?;
        entries.push(entry);
    }
}

/// Reads a LEB128 unsigned varint, like Go's `binary.ReadUvarint`.
///
/// Returns `None` when the input ends cleanly before the first byte.
fn read_uvarint(data: &mut &[u8]) -> Result<Option<u64>, Error> {
    let mut value = 0u64;
    let mut shift = 0u32;
    let mut index = 0usize;

    loop {
        let Some(&byte) = data.get(index) else {
            if index == 0 {
                return Ok(None);
            }
            return Err(Error::Decode("unexpected end of varint".to_string()));
        };
        index += 1;

        if shift >= 64 {
            return Err(Error::Decode("varint overflows 64 bits".to_string()));
        }
        value |= u64::from(byte & 0x7f) << shift;
        if byte & 0x80 == 0 {
            *data = &data[index..];
            return Ok(Some(value));
        }
        shift += 7;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sources_are_deduplicated_in_order() {
        let entry = ListEntry {
            resources: vec![
                EntryResource {
                    source_id: "list-b".to_string(),
                    resource_id: "r1".to_string(),
                },
                EntryResource {
                    source_id: "list-a".to_string(),
                    resource_id: "r2".to_string(),
                },
                EntryResource {
                    source_id: "list-b".to_string(),
                    resource_id: "r3".to_string(),
                },
            ],
            ..ListEntry::default()
        };
        assert_eq!(entry.sources(), vec!["list-b", "list-a"]);
    }

    #[test]
    fn rejects_broken_headers() {
        assert!(parse_header(&mut &b"X"[..]).is_err());
        assert!(parse_header(&mut &b""[..]).is_err());
        assert_eq!(
            parse_header(&mut &[pm_dsd::format::LIST, pm_dsd::format::JSON][..]).unwrap(),
            (false, pm_dsd::format::JSON)
        );
        assert_eq!(
            parse_header(
                &mut &[
                    pm_dsd::format::LIST,
                    pm_dsd::format::GZIP,
                    pm_dsd::format::JSON
                ][..]
            )
            .unwrap(),
            (true, pm_dsd::format::JSON)
        );
    }

    #[test]
    fn varints_are_read_like_go() {
        let mut data = &[0x96u8, 0x01, 0xff][..];
        assert_eq!(read_uvarint(&mut data).unwrap(), Some(150));
        assert_eq!(data, &[0xff]);

        let mut data = &[][..];
        assert_eq!(read_uvarint(&mut data).unwrap(), None);

        let mut data = &[0x80u8][..];
        assert!(read_uvarint(&mut data).is_err());
    }
}
