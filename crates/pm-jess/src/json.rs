//! The JSON handling of `filesig.jsonSplit`: gjson validation, extraction and
//! sjson deletion of the `_jess-*` keys, then canonical pretty printing.

use crate::pretty::{pretty_options, Options};

/// Metadata key holding the checksum(s).
pub const CHECKSUM_KEY: &str = "_jess-checksum";
/// Metadata key holding the signature(s).
pub const SIGNATURE_KEY: &str = "_jess-signature";

/// Nesting limit; deeper documents are rejected as invalid (Go has none, but
/// a hostile index must not exhaust the stack).
const MAX_DEPTH: usize = 1000;

/// Result of splitting a JSON document into content and metadata.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Split {
    /// Canonical (pretty printed, key sorted) content without metadata.
    pub content: Vec<u8>,
    pub checksums: Vec<String>,
    pub signatures: Vec<String>,
}

/// Port of `filesig.jsonSplit`. `None` means "invalid json".
pub fn json_split(data: &[u8]) -> Option<Split> {
    if !valid(data) {
        return None;
    }
    let mut content = data.to_vec();
    let mut checksums = Vec::new();
    let mut signatures = Vec::new();

    if let Some(found) = find_top_key(&content, CHECKSUM_KEY) {
        checksums = found.strings(&content);
        content = found.delete(&content);
    }
    if let Some(found) = find_top_key(&content, SIGNATURE_KEY) {
        signatures = found.strings(&content);
        content = found.delete(&content);
    }

    let content = pretty_options(&content, &Options::canonical());
    Some(Split {
        content,
        checksums,
        signatures,
    })
}

/// Equivalent of `gjson.ValidBytes`.
pub fn valid(data: &[u8]) -> bool {
    let mut i = 0;
    while i < data.len() {
        match data[i] {
            b' ' | b'\t' | b'\n' | b'\r' => i += 1,
            _ => {
                let Some(mut j) = valid_any(data, i, 0) else {
                    return false;
                };
                while j < data.len() {
                    match data[j] {
                        b' ' | b'\t' | b'\n' | b'\r' => j += 1,
                        _ => return false,
                    }
                }
                return true;
            }
        }
    }
    false
}

fn valid_any(data: &[u8], mut i: usize, depth: usize) -> Option<usize> {
    if depth > MAX_DEPTH {
        return None;
    }
    while i < data.len() {
        match data[i] {
            b' ' | b'\t' | b'\n' | b'\r' => i += 1,
            b'{' => return valid_object(data, i + 1, depth + 1),
            b'[' => return valid_array(data, i + 1, depth + 1),
            b'"' => return valid_string(data, i + 1),
            b'-' | b'0'..=b'9' => return valid_number(data, i),
            b't' => return valid_lit(data, i, b"true"),
            b'f' => return valid_lit(data, i, b"false"),
            b'n' => return valid_lit(data, i, b"null"),
            _ => return None,
        }
    }
    None
}

fn valid_lit(data: &[u8], i: usize, lit: &[u8]) -> Option<usize> {
    if data.len() >= i + lit.len() && &data[i..i + lit.len()] == lit {
        Some(i + lit.len())
    } else {
        None
    }
}

fn skip_ws(data: &[u8], mut i: usize) -> usize {
    while i < data.len() && matches!(data[i], b' ' | b'\t' | b'\n' | b'\r') {
        i += 1;
    }
    i
}

fn valid_object(data: &[u8], mut i: usize, depth: usize) -> Option<usize> {
    i = skip_ws(data, i);
    match data.get(i)? {
        b'}' => return Some(i + 1),
        b'"' => {}
        _ => return None,
    }
    loop {
        // At the opening quote of a key.
        i = valid_string(data, i + 1)?;
        i = skip_ws(data, i);
        if *data.get(i)? != b':' {
            return None;
        }
        i = valid_any(data, i + 1, depth)?;
        i = skip_ws(data, i);
        match data.get(i)? {
            b'}' => return Some(i + 1),
            b',' => {}
            _ => return None,
        }
        i = skip_ws(data, i + 1);
        if *data.get(i)? != b'"' {
            return None;
        }
    }
}

fn valid_array(data: &[u8], mut i: usize, depth: usize) -> Option<usize> {
    i = skip_ws(data, i);
    if *data.get(i)? == b']' {
        return Some(i + 1);
    }
    loop {
        i = valid_any(data, i, depth)?;
        i = skip_ws(data, i);
        match data.get(i)? {
            b']' => return Some(i + 1),
            b',' => i += 1,
            _ => return None,
        }
    }
}

fn valid_string(data: &[u8], mut i: usize) -> Option<usize> {
    while i < data.len() {
        let c = data[i];
        if c < b' ' {
            return None;
        } else if c == b'\\' {
            i += 1;
            match data.get(i)? {
                b'"' | b'\\' | b'/' | b'b' | b'f' | b'n' | b'r' | b't' => {}
                b'u' => {
                    for _ in 0..4 {
                        i += 1;
                        if !data.get(i)?.is_ascii_hexdigit() {
                            return None;
                        }
                    }
                }
                _ => return None,
            }
        } else if c == b'"' {
            return Some(i + 1);
        }
        i += 1;
    }
    None
}

fn valid_number(data: &[u8], mut i: usize) -> Option<usize> {
    if data[i] == b'-' {
        i += 1;
        if !data.get(i)?.is_ascii_digit() {
            return None;
        }
    }
    if data[i] == b'0' {
        i += 1;
    } else {
        while i < data.len() && data[i].is_ascii_digit() {
            i += 1;
        }
    }
    if i == data.len() {
        return Some(i);
    }
    if data[i] == b'.' {
        i += 1;
        if !data.get(i)?.is_ascii_digit() {
            return None;
        }
        i += 1;
        while i < data.len() && data[i].is_ascii_digit() {
            i += 1;
        }
    }
    if i == data.len() {
        return Some(i);
    }
    if data[i] == b'e' || data[i] == b'E' {
        i += 1;
        if i == data.len() {
            return None;
        }
        if data[i] == b'+' || data[i] == b'-' {
            i += 1;
        }
        if !data.get(i)?.is_ascii_digit() {
            return None;
        }
        i += 1;
        while i < data.len() && data[i].is_ascii_digit() {
            i += 1;
        }
    }
    Some(i)
}

/// Location of a top-level object member.
struct Found {
    /// Start of the member (opening quote of its key).
    start: usize,
    /// End of the member (after its value).
    end: usize,
    /// End of the previous member, if any.
    prev_end: Option<usize>,
    /// Start of the next member, if any.
    next_start: Option<usize>,
    value: (usize, usize),
}

impl Found {
    /// Strings of a string or array-of-strings value (gjson semantics).
    fn strings(&self, data: &[u8]) -> Vec<String> {
        let v = &data[self.value.0..self.value.1];
        match v.first() {
            Some(b'"') => vec![lossy(&unquote(v).unwrap_or_default())],
            Some(b'[') => {
                let mut out = Vec::new();
                let mut i = 1;
                while i < v.len() {
                    match v[i] {
                        b']' => break,
                        b' ' | b'\t' | b'\n' | b'\r' | b',' => i += 1,
                        _ => {
                            let end = skip_value(v, i);
                            if v[i] == b'"' {
                                out.push(lossy(&unquote(&v[i..end]).unwrap_or_default()));
                            }
                            i = end;
                        }
                    }
                }
                out
            }
            _ => Vec::new(),
        }
    }

    /// Removes the member and one adjacent comma.
    fn delete(&self, data: &[u8]) -> Vec<u8> {
        let (from, to) = match (self.prev_end, self.next_start) {
            (Some(prev_end), _) => (prev_end, self.end),
            (None, Some(next)) => (self.start, next),
            (None, None) => (self.start, self.end),
        };
        let mut out = data[..from].to_vec();
        out.extend_from_slice(&data[to..]);
        out
    }
}

fn lossy(b: &[u8]) -> String {
    String::from_utf8_lossy(b).into_owned()
}

/// Skips one valid JSON value starting at `i` (which is not whitespace).
fn skip_value(data: &[u8], i: usize) -> usize {
    valid_any(data, i, 0).unwrap_or(data.len())
}

/// Finds the first member named `key` of a top-level object. The data is valid
/// JSON, so only structure is scanned.
fn find_top_key(data: &[u8], key: &str) -> Option<Found> {
    let mut i = skip_ws(data, 0);
    if data.get(i) != Some(&b'{') {
        return None;
    }
    i += 1;
    let mut prev_end: Option<usize> = None;
    loop {
        i = skip_ws(data, i);
        match data.get(i)? {
            b'}' => return None,
            b',' => {
                i += 1;
                continue;
            }
            _ => {}
        }
        let start = i;
        let kend = valid_string(data, i + 1)?;
        let name = unquote(&data[start..kend])?;
        i = skip_ws(data, kend);
        i += 1; // colon
        i = skip_ws(data, i);
        let vstart = i;
        let vend = valid_any(data, i, 0)?;
        i = vend;
        if name == key.as_bytes() {
            // Look ahead for the next member start.
            let mut j = skip_ws(data, i);
            let mut next_start = None;
            if data.get(j) == Some(&b',') {
                j = skip_ws(data, j + 1);
                if data.get(j) == Some(&b'"') {
                    next_start = Some(j);
                }
            }
            return Some(Found {
                start,
                end: vend,
                prev_end,
                next_start,
                value: (vstart, vend),
            });
        }
        prev_end = Some(vend);
    }
}

/// Unquotes a JSON string literal like `encoding/json` does (invalid UTF-8 and
/// lone surrogates become U+FFFD). `None` for malformed input.
pub fn unquote(s: &[u8]) -> Option<Vec<u8>> {
    if s.len() < 2 || s[0] != b'"' || s[s.len() - 1] != b'"' {
        return None;
    }
    let s = &s[1..s.len() - 1];
    let mut out = Vec::with_capacity(s.len());
    let mut r = 0;
    while r < s.len() {
        let c = s[r];
        match c {
            b'\\' => {
                r += 1;
                match *s.get(r)? {
                    b'"' | b'\\' | b'/' | b'\'' => {
                        out.push(s[r]);
                        r += 1;
                    }
                    b'b' => {
                        out.push(8);
                        r += 1;
                    }
                    b'f' => {
                        out.push(12);
                        r += 1;
                    }
                    b'n' => {
                        out.push(b'\n');
                        r += 1;
                    }
                    b'r' => {
                        out.push(b'\r');
                        r += 1;
                    }
                    b't' => {
                        out.push(b'\t');
                        r += 1;
                    }
                    b'u' => {
                        r -= 1;
                        let rr = get_u4(&s[r..])?;
                        r += 6;
                        let mut ch = rr;
                        if (0xd800..0xe000).contains(&rr) {
                            let rr1 = get_u4(&s[r..]);
                            match rr1 {
                                Some(lo)
                                    if (0xd800..0xdc00).contains(&rr)
                                        && (0xdc00..0xe000).contains(&lo) =>
                                {
                                    r += 6;
                                    ch = 0x10000 + ((rr - 0xd800) << 10 | (lo - 0xdc00));
                                }
                                _ => ch = 0xfffd,
                            }
                        }
                        let c = char::from_u32(ch).unwrap_or('\u{fffd}');
                        let mut tmp = [0u8; 4];
                        out.extend_from_slice(c.encode_utf8(&mut tmp).as_bytes());
                    }
                    _ => return None,
                }
            }
            b'"' => return None,
            c if c < b' ' => return None,
            c if c < 0x80 => {
                out.push(c);
                r += 1;
            }
            c => {
                let len = match c {
                    0xc2..=0xdf => 2,
                    0xe0..=0xef => 3,
                    0xf0..=0xf4 => 4,
                    _ => 0,
                };
                match s.get(r..r + len).filter(|_| len > 0) {
                    Some(chunk) if std::str::from_utf8(chunk).is_ok() => {
                        out.extend_from_slice(chunk);
                        r += len;
                    }
                    _ => {
                        out.extend_from_slice("\u{fffd}".as_bytes());
                        r += 1;
                    }
                }
            }
        }
    }
    Some(out)
}

/// `getu4`: parses `\uXXXX` at the start of `s`.
fn get_u4(s: &[u8]) -> Option<u32> {
    if s.len() < 6 || s[0] != b'\\' || s[1] != b'u' {
        return None;
    }
    let mut r = 0u32;
    for &c in &s[2..6] {
        r = r * 16 + (c as char).to_digit(16)?;
    }
    Some(r)
}
