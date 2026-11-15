//! Reader for DSDL list blobs (see Go `filterlists/decoder.go`).

use std::borrow::Cow;
use std::io::Read;

use crate::format::{GZIP, LIST};
use crate::varint::unpack64;
use crate::{Error, Result};

/// Iterates the entries of a DSDL blob: `'L'`, optional `'Z'`, format byte,
/// then repeated `uvarint length + blob` (gzip-compressed when `'Z'` is set).
pub struct ListReader<'a> {
    body: Cow<'a, [u8]>,
    pos: usize,
    format: u8,
    compressed: bool,
    done: bool,
}

impl<'a> ListReader<'a> {
    pub fn new(data: &'a [u8]) -> Result<Self> {
        let [magic, second, rest @ ..] = data else {
            return Err(Error::BadList("header too short"));
        };
        if *magic != LIST {
            return Err(Error::BadList("unexpected file type"));
        }
        let (compressed, format, rest) = if *second == GZIP {
            let [format, rest @ ..] = rest else {
                return Err(Error::BadList("header too short"));
            };
            (true, *format, rest)
        } else {
            (false, *second, rest)
        };
        let body = if compressed {
            let mut buf = Vec::new();
            flate2::read::MultiGzDecoder::new(rest).read_to_end(&mut buf)?;
            Cow::Owned(buf)
        } else {
            Cow::Borrowed(rest)
        };
        Ok(Self {
            body,
            pos: 0,
            format,
            compressed,
            done: false,
        })
    }

    /// Format byte of the entries.
    pub fn format(&self) -> u8 {
        self.format
    }

    pub fn is_compressed(&self) -> bool {
        self.compressed
    }

    fn read_entry(&mut self) -> Result<Option<Cow<'a, [u8]>>> {
        let rest = &self.body[self.pos..];
        if rest.is_empty() {
            return Ok(None);
        }
        let (len, n) = unpack64(rest)?;
        let end = usize::try_from(len)
            .ok()
            .and_then(|l| l.checked_add(n))
            .filter(|&e| e <= rest.len())
            .ok_or(Error::UnexpectedEof)?;
        let (start, end) = (self.pos + n, self.pos + end);
        self.pos = end;
        Ok(Some(match &self.body {
            Cow::Borrowed(b) => Cow::Borrowed(&b[start..end]),
            Cow::Owned(b) => Cow::Owned(b[start..end].to_vec()),
        }))
    }
}

impl<'a> Iterator for ListReader<'a> {
    type Item = Result<(Cow<'a, [u8]>, u8)>;

    fn next(&mut self) -> Option<Self::Item> {
        if self.done {
            return None;
        }
        match self.read_entry() {
            Ok(Some(blob)) => Some(Ok((blob, self.format))),
            Ok(None) => {
                self.done = true;
                None
            }
            Err(e) => {
                self.done = true;
                Some(Err(e))
            }
        }
    }
}
