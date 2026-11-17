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

/// Returns whether a DSD format can be decoded into a structured value.
///
/// Go uses `dsd.LoadAsFormat` and accepts any format that can hold a complex
/// structure. `pm-dsd` currently implements JSON, CBOR and MessagePack;
/// GenCode and YAML are not supported by it.
fn is_decodable_format(format: u8) -> bool {
    matches!(
        format,
        pm_dsd::format::JSON | pm_dsd::format::CBOR | pm_dsd::format::MSG_PACK
    )
}

/// Decodes one entry blob using the format from the DSDL header.
///
/// DSDL entries are bare encoded values (the format byte lives in the file
/// header), so re-add it before handing the blob to `pm-dsd`.
fn decode_entry(blob: &[u8], format: u8) -> Result<ListEntry, Error> {
    let mut framed = pm_dsd::varint::pack8(format);
    framed.extend_from_slice(blob);
    let (entry, _) = pm_dsd::load::<ListEntry>(&framed)?;
    Ok(entry)
}

/// Decodes a DSDL filterlist file, like Go `decodeFile`.
///
/// The Go implementation streams entries through a channel; this returns all
/// entries at once. Unlike Go, an unsupported format or a single bad entry does
/// not abort the whole update: the file/entry is skipped with a warning.
pub fn decode_file(mut reader: impl Read) -> Result<Vec<ListEntry>, Error> {
    let (compressed, format) = parse_header(&mut reader)?;

    if !is_decodable_format(format) {
        tracing::warn!(
            format,
            "intel/filterlists: skipping DSDL list with unsupported DSD format"
        );
        return Ok(Vec::new());
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
        match decode_entry(blob, format) {
            Ok(entry) => entries.push(entry),
            Err(error) => {
                tracing::warn!(?error, "intel/filterlists: skipping undecodable DSDL entry");
            }
        }
    }
}

/// Reads a LEB128 unsigned varint, like Go's `binary.ReadUvarint`.
///
/// A `u64` takes at most 10 bytes; the 10th byte may only be 0 or 1. Go rejects
/// a larger value instead of silently truncating it. Returns `None` when the
/// input ends cleanly before the first byte.
fn read_uvarint(data: &mut &[u8]) -> Result<Option<u64>, Error> {
    const MAX_LEN: usize = 10;
    const OVERFLOW: &str = "varint overflows 64 bits";

    let mut value = 0u64;
    let mut shift = 0u32;
    for index in 0..MAX_LEN {
        let Some(&byte) = data.get(index) else {
            return if index == 0 {
                Ok(None)
            } else {
                Err(Error::Decode("unexpected end of varint".to_string()))
            };
        };
        if byte < 0x80 {
            // The 10th byte contributes only bit 63, so anything above 1
            // overflows. Go's `binary.ReadUvarint` rejects exactly this case.
            if index == MAX_LEN - 1 && byte > 1 {
                return Err(Error::Decode(OVERFLOW.to_string()));
            }
            *data = &data[index + 1..];
            return Ok(Some(value | (u64::from(byte) << shift)));
        }
        value |= u64::from(byte & 0x7f) << shift;
        shift += 7;
    }

    Err(Error::Decode(OVERFLOW.to_string()))
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

    #[test]
    fn varint_rejects_10th_byte_above_one() {
        // Nine continuation bytes followed by a 10th byte > 1 must be
        // rejected, just like Go's `binary.ReadUvarint`, not truncated.
        let mut overflow = [0x80u8; 9].to_vec();
        overflow.push(0x02);
        assert!(read_uvarint(&mut overflow.as_slice()).is_err());

        // Ten continuation bytes without a terminator also overflow.
        let unterminated = [0x80u8; 10];
        assert!(read_uvarint(&mut unterminated.as_slice()).is_err());

        // The largest valid 10-byte encoding ends with a single 1.
        let mut maximum = [0xffu8; 9].to_vec();
        maximum.push(0x01);
        assert_eq!(
            read_uvarint(&mut maximum.as_slice()).unwrap(),
            Some(u64::MAX)
        );

        let mut highest_bit = [0x80u8; 9].to_vec();
        highest_bit.push(0x01);
        assert_eq!(
            read_uvarint(&mut highest_bit.as_slice()).unwrap(),
            Some(1u64 << 63)
        );
    }

    fn sample_entry() -> ListEntry {
        ListEntry {
            type_: "domain".to_string(),
            entity: "example.com".to_string(),
            whitelist: false,
            resources: vec![EntryResource {
                source_id: "list-a".to_string(),
                resource_id: "r1".to_string(),
            }],
        }
    }

    fn encoded_entry(format: u8, entry: &ListEntry) -> Vec<u8> {
        let framed = pm_dsd::dump(entry, format).unwrap();
        // `dump` prepends the format byte; DSDL entries are bare values.
        framed[1..].to_vec()
    }

    fn make_file(format: u8, blobs: &[Vec<u8>]) -> Vec<u8> {
        let mut out = vec![pm_dsd::format::LIST, format];
        for blob in blobs {
            out.extend_from_slice(&pm_dsd::varint::pack64(blob.len() as u64));
            out.extend_from_slice(blob);
        }
        out
    }

    #[test]
    fn decodes_all_pm_dsd_structural_formats() {
        let entry = sample_entry();
        for format in [
            pm_dsd::format::JSON,
            pm_dsd::format::CBOR,
            pm_dsd::format::MSG_PACK,
        ] {
            let blob = encoded_entry(format, &entry);
            let file = make_file(format, &[blob]);
            let decoded = decode_file(file.as_slice()).unwrap();
            assert_eq!(decoded, vec![entry.clone()], "format {format}");
        }
    }

    #[test]
    fn skips_unsupported_formats_without_failing() {
        for format in [
            pm_dsd::format::AUTO,
            pm_dsd::format::RAW,
            pm_dsd::format::GEN_CODE,
            pm_dsd::format::YAML,
        ] {
            let file = make_file(format, &[b"anything".to_vec()]);
            assert!(
                decode_file(file.as_slice()).unwrap().is_empty(),
                "format {format}"
            );
        }
    }

    #[test]
    fn skips_undecodable_entries_without_failing() {
        let entry = sample_entry();
        let good = encoded_entry(pm_dsd::format::JSON, &entry);
        let bad = b"not json".to_vec();
        let file = make_file(pm_dsd::format::JSON, &[bad, good]);
        assert_eq!(decode_file(file.as_slice()).unwrap(), vec![entry]);
    }
}
