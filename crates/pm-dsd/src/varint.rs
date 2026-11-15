//! Varint encoding as implemented by Go `structures/varint`.

use crate::{Error, Result};

const MAX_LEN: usize = 10;

fn put_uvarint(mut n: u64) -> Vec<u8> {
    let mut out = Vec::with_capacity(MAX_LEN);
    while n >= 0x80 {
        out.push((n as u8) | 0x80);
        n >>= 7;
    }
    out.push(n as u8);
    out
}

/// Port of Go `binary.Uvarint` (accepts non-minimal encodings like Go).
fn uvarint(buf: &[u8]) -> Result<(u64, usize)> {
    let mut x: u64 = 0;
    let mut s: u32 = 0;
    for (i, &b) in buf.iter().enumerate() {
        if i == MAX_LEN {
            return Err(Error::Varint("encoded integer greater than uint64"));
        }
        if b < 0x80 {
            if i == MAX_LEN - 1 && b > 1 {
                return Err(Error::Varint("encoded integer greater than uint64"));
            }
            return Ok((x | (u64::from(b) << s), i + 1));
        }
        x |= u64::from(b & 0x7f) << s;
        s += 7;
    }
    Err(Error::BufTooSmall)
}

/// Go `Pack8`: values >= 128 are encoded as `[n, 0x01]`.
pub fn pack8(n: u8) -> Vec<u8> {
    if n < 128 {
        vec![n]
    } else {
        vec![n, 0x01]
    }
}

pub fn pack16(n: u16) -> Vec<u8> {
    put_uvarint(u64::from(n))
}

pub fn pack32(n: u32) -> Vec<u8> {
    put_uvarint(u64::from(n))
}

pub fn pack64(n: u64) -> Vec<u8> {
    put_uvarint(n)
}

/// Go `Unpack8`. Like Go, a two-byte value reports only 1 byte as used.
pub fn unpack8(blob: &[u8]) -> Result<(u8, usize)> {
    let first = *blob.first().ok_or(Error::BufTooSmall)?;
    if first < 128 {
        return Ok((first, 1));
    }
    match blob.get(1) {
        None => Err(Error::BufTooSmall),
        Some(0x01) => Ok((first, 1)),
        Some(_) => Err(Error::Varint("encoded integer greater than 255 (uint8)")),
    }
}

pub fn unpack16(blob: &[u8]) -> Result<(u16, usize)> {
    let (n, r) = uvarint(blob)?;
    let n = u16::try_from(n)
        .map_err(|_| Error::Varint("encoded integer greater than 65535 (uint16)"))?;
    Ok((n, r))
}

pub fn unpack32(blob: &[u8]) -> Result<(u32, usize)> {
    let (n, r) = uvarint(blob)?;
    let n = u32::try_from(n)
        .map_err(|_| Error::Varint("encoded integer greater than 4294967295 (uint32)"))?;
    Ok((n, r))
}

pub fn unpack64(blob: &[u8]) -> Result<(u64, usize)> {
    uvarint(blob)
}

/// Prepends the varint-encoded length to the data.
pub fn prepend_length(data: &[u8]) -> Vec<u8> {
    let mut out = pack64(data.len() as u64);
    out.extend_from_slice(data);
    out
}

/// Go `GetNextBlock`: returns the block and the total bytes consumed (prefix included).
pub fn next_block(data: &[u8]) -> Result<(&[u8], usize)> {
    let (l, n) = unpack64(data)?;
    let not_enough = || Error::Varint("not enough data for given block length");
    let total = usize::try_from(l)
        .ok()
        .and_then(|l| l.checked_add(n))
        .filter(|&t| t <= data.len())
        .ok_or_else(not_enough)?;
    Ok((&data[n..total], total))
}
