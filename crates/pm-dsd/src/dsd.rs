use std::io::{Read, Write};

use serde::de::DeserializeOwned;
use serde::Serialize;

use crate::format::{AUTO, CBOR, GEN_CODE, GZIP, JSON, MSG_PACK, RAW, YAML};
use crate::varint::{pack8, unpack8};
use crate::{gojson, Error, Result};

fn is_serialization_format(f: u8) -> bool {
    matches!(f, AUTO | RAW | CBOR | GEN_CODE | JSON | YAML | MSG_PACK)
}

fn split_format(data: &[u8]) -> Result<(u8, &[u8])> {
    let (format, read) = unpack8(data)?;
    if data.len() <= read {
        return Err(Error::UnexpectedEof);
    }
    Ok((format, &data[read..]))
}

fn load_as_format<T: DeserializeOwned>(data: &[u8], format: u8) -> Result<T> {
    match format {
        RAW => Err(Error::IsRaw),
        JSON => Ok(serde_json::from_slice(data)?),
        CBOR => ciborium::from_reader(data).map_err(|e| Error::Cbor(e.to_string())),
        MSG_PACK => rmp_serde::from_slice(data).map_err(|e| Error::MsgPack(e.to_string())),
        GEN_CODE => Err(Error::Unsupported("gencode")),
        YAML => Err(Error::Unsupported("yaml")),
        _ => Err(Error::IncompatibleFormat),
    }
}

/// Loads a DSD blob (format byte + payload, optionally gzip-wrapped).
/// Returns the value and the serialization format (the inner one for GZIP).
pub fn load<T: DeserializeOwned>(data: &[u8]) -> Result<(T, u8)> {
    let (format, payload) = split_format(data)?;
    if is_serialization_format(format) {
        return Ok((load_as_format(payload, format)?, format));
    }
    if format != GZIP {
        return Err(Error::IncompatibleFormat);
    }
    let mut buf = Vec::new();
    flate2::read::MultiGzDecoder::new(payload).read_to_end(&mut buf)?;
    let (inner, inner_payload) = split_format(&buf)?;
    Ok((load_as_format(inner_payload, inner)?, inner))
}

/// Dumps a value with a leading format byte. `AUTO` means JSON; `GZIP`
/// wraps JSON in gzip (best compression), like Go `DumpAndCompress`.
pub fn dump<T: Serialize>(v: &T, format: u8) -> Result<Vec<u8>> {
    let format = if format == AUTO { JSON } else { format };
    if format == GZIP {
        let inner = dump(v, JSON)?;
        let mut enc = flate2::write::GzEncoder::new(pack8(GZIP), flate2::Compression::best());
        enc.write_all(&inner)?;
        return Ok(enc.finish()?);
    }
    let mut out = pack8(format);
    match format {
        JSON => out.extend(gojson::to_vec(v)?),
        CBOR => ciborium::into_writer(v, &mut out).map_err(|e| Error::Cbor(e.to_string()))?,
        MSG_PACK => {
            out.extend(rmp_serde::to_vec_named(v).map_err(|e| Error::MsgPack(e.to_string()))?);
        }
        GEN_CODE => return Err(Error::Unsupported("gencode")),
        YAML => return Err(Error::Unsupported("yaml")),
        _ => return Err(Error::IncompatibleFormat),
    }
    Ok(out)
}
