//! Port of the part of `github.com/tidwall/pretty` (v1.2.1) that jess uses:
//! `PrettyOptions` with `Width: 200`, no prefix, one-space indent, sorted keys.
//!
//! The algorithm is ported one to one, quirks included, because the output
//! is hashed and signed. Input must be valid JSON.

/// Options mirroring `pretty.Options` (the prefix is always empty in jess).
#[derive(Debug, Clone)]
pub struct Options {
    pub width: i64,
    pub indent: Vec<u8>,
    pub sort_keys: bool,
}

impl Options {
    /// The options `filesig.jsonSplit` uses for canonical content.
    pub fn canonical() -> Self {
        Self {
            width: 200,
            indent: b" ".to_vec(),
            sort_keys: true,
        }
    }
}

/// Pretty-prints valid JSON like `pretty.PrettyOptions`.
pub fn pretty_options(json: &[u8], opts: &Options) -> Vec<u8> {
    let mut p = Printer { json, opts };
    let mut buf = Vec::with_capacity(json.len());
    p.any(&mut buf, 0, true, opts.sort_keys, 0, 0, -1);
    if !buf.is_empty() {
        buf.push(b'\n');
    }
    buf
}

struct Printer<'a> {
    json: &'a [u8],
    opts: &'a Options,
}

#[derive(Clone, Copy, Default)]
struct Pair {
    kstart: usize,
    kend: usize,
    vstart: usize,
    vend: usize,
}

fn is_nan_or_inf(src: &[u8]) -> bool {
    src[0] == b'i'
        || src[0] == b'I'
        || src[0] == b'+'
        || src[0] == b'N'
        || (src[0] == b'n' && src.len() > 1 && src[1] != b'u')
}

impl Printer<'_> {
    #[allow(clippy::too_many_arguments)]
    fn any(
        &mut self,
        buf: &mut Vec<u8>,
        mut i: usize,
        pretty: bool,
        sortkeys: bool,
        tabs: usize,
        nl: usize,
        max: i64,
    ) -> (usize, usize, bool) {
        let json = self.json;
        while i < json.len() {
            let c = json[i];
            if c <= b' ' {
                i += 1;
                continue;
            }
            if c == b'"' {
                return self.string(buf, i, nl);
            }
            if c.is_ascii_digit() || c == b'-' || is_nan_or_inf(&json[i..]) {
                return self.number(buf, i, nl);
            }
            if c == b'{' {
                return self.object(buf, i, b'{', b'}', pretty, sortkeys, tabs, nl, max);
            }
            if c == b'[' {
                return self.object(buf, i, b'[', b']', pretty, sortkeys, tabs, nl, max);
            }
            match c {
                b't' => {
                    buf.extend_from_slice(b"true");
                    return (i + 4, nl, true);
                }
                b'f' => {
                    buf.extend_from_slice(b"false");
                    return (i + 5, nl, true);
                }
                b'n' => {
                    buf.extend_from_slice(b"null");
                    return (i + 4, nl, true);
                }
                _ => {}
            }
            i += 1;
        }
        (i, nl, true)
    }

    #[allow(clippy::too_many_arguments)]
    fn object(
        &mut self,
        buf: &mut Vec<u8>,
        mut i: usize,
        open: u8,
        close: u8,
        pretty: bool,
        mut sortkeys: bool,
        tabs: usize,
        mut nl: usize,
        max: i64,
    ) -> (usize, usize, bool) {
        let json = self.json;
        let width = self.opts.width;
        if width > 0 {
            if pretty && open == b'[' && max == -1 {
                // Try to create a single line array.
                let m = width - (buf.len() as i64 - nl as i64);
                if m > 3 {
                    let (s1, s2) = (buf.len(), i);
                    let (ni, _, ok) = self.object(buf, i, b'[', b']', false, sortkeys, 0, 0, m);
                    i = ni;
                    if ok && (buf.len() - s1) as i64 <= m {
                        return (i, nl, true);
                    }
                    buf.truncate(s1);
                    i = s2;
                }
            } else if max != -1 && open == b'{' {
                return (i, nl, false);
            }
        }
        buf.push(open);
        i += 1;
        let mut pairs: Vec<Pair> = Vec::new();
        let mut n = 0usize;
        while i < json.len() {
            let c = json[i];
            if c <= b' ' {
                i += 1;
                continue;
            }
            if c == close {
                if pretty {
                    if open == b'{' && sortkeys {
                        self.sort_pairs(buf, &pairs);
                    }
                    if n > 0 {
                        nl = buf.len();
                        if buf[nl - 1] == b' ' {
                            buf[nl - 1] = b'\n';
                        } else {
                            buf.push(b'\n');
                        }
                    }
                    if buf[buf.len() - 1] != open {
                        self.tabs(buf, tabs);
                    }
                }
                buf.push(close);
                return (i + 1, nl, open != b'{');
            }
            if open == b'[' || c == b'"' {
                if n > 0 {
                    buf.push(b',');
                    if width != -1 && open == b'[' {
                        buf.push(b' ');
                    }
                }
                let mut p = Pair::default();
                if pretty {
                    nl = buf.len();
                    if buf[nl - 1] == b' ' {
                        buf[nl - 1] = b'\n';
                    } else {
                        buf.push(b'\n');
                    }
                    if open == b'{' && sortkeys {
                        p.kstart = i;
                        p.vstart = buf.len();
                    }
                    self.tabs(buf, tabs + 1);
                }
                if open == b'{' {
                    let (ni, nnl, _) = self.string(buf, i, nl);
                    i = ni;
                    nl = nnl;
                    if sortkeys {
                        p.kend = i;
                    }
                    buf.push(b':');
                    if pretty {
                        buf.push(b' ');
                    }
                }
                let (ni, nnl, ok) = self.any(buf, i, pretty, sortkeys, tabs + 1, nl, max);
                i = ni;
                nl = nnl;
                if max != -1 && !ok {
                    return (i, nl, false);
                }
                if pretty && open == b'{' && sortkeys {
                    p.vend = buf.len();
                    if p.kstart > p.kend || p.vstart > p.vend {
                        sortkeys = false;
                    } else {
                        pairs.push(p);
                    }
                }
                n += 1;
                // Go decrements and the loop increments again: no net change.
                continue;
            }
            i += 1;
        }
        (i, nl, open != b'{')
    }

    fn sort_pairs(&self, buf: &mut Vec<u8>, pairs: &[Pair]) {
        if pairs.is_empty() {
            return;
        }
        let vstart = pairs[0].vstart;
        let mut order: Vec<usize> = (0..pairs.len()).collect();
        {
            let view: &[u8] = buf;
            // The order is total (kind rank, then string/number/byte order),
            // so the std stable sort gives the same result as Go's.
            order.sort_by(|&a, &b| {
                if self.is_less(view, pairs, a, b, true) {
                    std::cmp::Ordering::Less
                } else if self.is_less(view, pairs, b, a, true) {
                    std::cmp::Ordering::Greater
                } else if self.is_less(view, pairs, a, b, false) {
                    std::cmp::Ordering::Less
                } else if self.is_less(view, pairs, b, a, false) {
                    std::cmp::Ordering::Greater
                } else {
                    std::cmp::Ordering::Equal
                }
            });
        }
        if order.iter().enumerate().all(|(k, &v)| k == v) {
            return;
        }
        let mut nbuf = Vec::new();
        for (k, &idx) in order.iter().enumerate() {
            let p = pairs[idx];
            nbuf.extend_from_slice(&buf[p.vstart..p.vend]);
            if k < pairs.len() - 1 {
                nbuf.extend_from_slice(b",\n");
            }
        }
        buf.truncate(vstart);
        buf.extend_from_slice(&nbuf);
    }

    fn is_less(&self, buf: &[u8], pairs: &[Pair], i: usize, j: usize, by_key: bool) -> bool {
        let k1 = &self.json[pairs[i].kstart..pairs[i].kend];
        let k2 = &self.json[pairs[j].kstart..pairs[j].kend];
        let (v1, v2): (&[u8], &[u8]) = if by_key {
            (k1, k2)
        } else {
            let mut v1 = trim_space(&buf[pairs[i].vstart..pairs[i].vend]);
            let mut v2 = trim_space(&buf[pairs[j].vstart..pairs[j].vend]);
            if v1.len() > k1.len() {
                v1 = trim_space(&v1[k1.len() + 1..]);
            }
            if v2.len() > k2.len() {
                v2 = trim_space(&v2[k2.len() + 1..]);
            }
            (v1, v2)
        };
        let t1 = jtype(v1);
        let t2 = jtype(v2);
        if t1 != t2 {
            return t1 < t2;
        }
        if t1 == JType::String {
            return parse_str(v1) < parse_str(v2);
        }
        if t1 == JType::Number {
            return parse_float(v1) < parse_float(v2);
        }
        v1 < v2
    }

    fn string(&self, buf: &mut Vec<u8>, start: usize, nl: usize) -> (usize, usize, bool) {
        let json = self.json;
        let s = start;
        let mut i = start + 1;
        while i < json.len() {
            if json[i] == b'"' {
                let mut sc = 0;
                let mut j = i - 1;
                while j > s {
                    if json[j] == b'\\' {
                        sc += 1;
                    } else {
                        break;
                    }
                    j -= 1;
                }
                if sc % 2 == 1 {
                    i += 1;
                    continue;
                }
                i += 1;
                break;
            }
            i += 1;
        }
        buf.extend_from_slice(&json[s..i]);
        (i, nl, true)
    }

    fn number(&self, buf: &mut Vec<u8>, start: usize, nl: usize) -> (usize, usize, bool) {
        let json = self.json;
        let mut i = start + 1;
        while i < json.len() {
            let c = json[i];
            if c <= b' ' || c == b',' || c == b':' || c == b']' || c == b'}' {
                break;
            }
            i += 1;
        }
        buf.extend_from_slice(&json[start..i]);
        (i, nl, true)
    }

    fn tabs(&self, buf: &mut Vec<u8>, tabs: usize) {
        for _ in 0..tabs {
            buf.extend_from_slice(&self.opts.indent);
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
enum JType {
    Null,
    False,
    Number,
    String,
    True,
    Json,
}

fn jtype(v: &[u8]) -> JType {
    match v.first() {
        None => JType::Null,
        Some(b'"') => JType::String,
        Some(b'f') => JType::False,
        Some(b't') => JType::True,
        Some(b'n') => JType::Null,
        Some(b'[' | b'{') => JType::Json,
        Some(_) => JType::Number,
    }
}

/// `bytes.TrimSpace` restricted to ASCII: the trimmed text is JSON.
fn trim_space(mut s: &[u8]) -> &[u8] {
    let ws = |c: &u8| matches!(c, b' ' | b'\t' | b'\n' | 0x0b | 0x0c | b'\r');
    while let [first, rest @ ..] = s {
        if ws(first) {
            s = rest;
        } else {
            break;
        }
    }
    while let [rest @ .., last] = s {
        if ws(last) {
            s = rest;
        } else {
            break;
        }
    }
    s
}

/// `strconv.ParseFloat` ignoring errors (0 on syntax errors).
fn parse_float(v: &[u8]) -> f64 {
    std::str::from_utf8(v)
        .ok()
        .and_then(|s| s.parse::<f64>().ok())
        .unwrap_or(0.0)
}

/// `parsestr` of the Go package.
fn parse_str(s: &[u8]) -> Vec<u8> {
    for i in 1..s.len() {
        if s[i] == b'\\' {
            return crate::json::unquote(s).unwrap_or_default();
        }
        if s[i] == b'"' {
            return s[1..i].to_vec();
        }
    }
    Vec::new()
}
