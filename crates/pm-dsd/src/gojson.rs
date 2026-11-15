//! JSON output that is byte-identical to Go `encoding/json` (with HTML escaping).

use std::io::{self, Write};

use serde::Serialize;
use serde_json::ser::{CharEscape, Formatter};

use crate::Result;

/// Serializes like Go `json.Marshal`.
pub fn to_vec<T: Serialize + ?Sized>(value: &T) -> Result<Vec<u8>> {
    let mut out = Vec::new();
    let mut ser = serde_json::Serializer::with_formatter(&mut out, GoFormatter::compact());
    value.serialize(&mut ser)?;
    Ok(out)
}

/// Serializes like Go `json.MarshalIndent`.
pub fn to_vec_indent<T: Serialize + ?Sized>(
    value: &T,
    prefix: &str,
    indent: &str,
) -> Result<Vec<u8>> {
    let mut out = Vec::new();
    let mut ser =
        serde_json::Serializer::with_formatter(&mut out, GoFormatter::pretty(prefix, indent));
    value.serialize(&mut ser)?;
    Ok(out)
}

struct GoFormatter<'a> {
    pretty: Option<(&'a str, &'a str)>,
    depth: usize,
    has_value: bool,
}

impl<'a> GoFormatter<'a> {
    fn compact() -> Self {
        Self {
            pretty: None,
            depth: 0,
            has_value: false,
        }
    }

    fn pretty(prefix: &'a str, indent: &'a str) -> Self {
        Self {
            pretty: Some((prefix, indent)),
            depth: 0,
            has_value: false,
        }
    }

    fn newline<W: ?Sized + Write>(&self, w: &mut W) -> io::Result<()> {
        if let Some((prefix, indent)) = self.pretty {
            w.write_all(b"\n")?;
            w.write_all(prefix.as_bytes())?;
            for _ in 0..self.depth {
                w.write_all(indent.as_bytes())?;
            }
        }
        Ok(())
    }

    fn open<W: ?Sized + Write>(&mut self, w: &mut W, c: &[u8]) -> io::Result<()> {
        self.depth += 1;
        self.has_value = false;
        w.write_all(c)
    }

    fn close<W: ?Sized + Write>(&mut self, w: &mut W, c: &[u8]) -> io::Result<()> {
        self.depth -= 1;
        if self.has_value {
            self.newline(w)?;
        }
        w.write_all(c)
    }

    fn item<W: ?Sized + Write>(&self, w: &mut W, first: bool) -> io::Result<()> {
        if !first {
            w.write_all(b",")?;
        }
        self.newline(w)
    }
}

/// Go formats floats with 'f' unless the exponent is very small or large.
fn go_float(shortest_e: &str, plain: String, use_exp: bool) -> String {
    if !use_exp {
        return plain;
    }
    // shortest_e looks like "-1.5e-7" or "1e21".
    let (mantissa, exp) = shortest_e.split_once('e').unwrap_or((shortest_e, "0"));
    let exp: i32 = exp.parse().unwrap_or(0);
    let sign = if exp < 0 { '-' } else { '+' };
    let abs = exp.unsigned_abs();
    // Go keeps two exponent digits ("e+21") but trims "e-07" to "e-7".
    if exp < 0 && abs < 10 {
        format!("{mantissa}e-{abs}")
    } else {
        format!("{mantissa}e{sign}{abs:02}")
    }
}

impl Formatter for GoFormatter<'_> {
    fn write_f64<W: ?Sized + Write>(&mut self, w: &mut W, v: f64) -> io::Result<()> {
        let a = v.abs();
        let use_exp = a != 0.0 && !(1e-6..1e21).contains(&a);
        w.write_all(go_float(&format!("{v:e}"), v.to_string(), use_exp).as_bytes())
    }

    fn write_f32<W: ?Sized + Write>(&mut self, w: &mut W, v: f32) -> io::Result<()> {
        let a = v.abs();
        let use_exp = a != 0.0 && !(1e-6f32..1e21f32).contains(&a);
        w.write_all(go_float(&format!("{v:e}"), v.to_string(), use_exp).as_bytes())
    }

    fn write_string_fragment<W: ?Sized + Write>(&mut self, w: &mut W, s: &str) -> io::Result<()> {
        let mut start = 0;
        for (i, c) in s.char_indices() {
            let esc: &[u8] = match c {
                '<' => b"\\u003c",
                '>' => b"\\u003e",
                '&' => b"\\u0026",
                '\u{2028}' => b"\\u2028",
                '\u{2029}' => b"\\u2029",
                _ => continue,
            };
            w.write_all(&s.as_bytes()[start..i])?;
            w.write_all(esc)?;
            start = i + c.len_utf8();
        }
        w.write_all(&s.as_bytes()[start..])
    }

    fn write_char_escape<W: ?Sized + Write>(&mut self, w: &mut W, e: CharEscape) -> io::Result<()> {
        // Same as serde_json defaults; Go also uses \b and \f short forms.
        let s: &[u8] = match e {
            CharEscape::Quote => b"\\\"",
            CharEscape::ReverseSolidus => b"\\\\",
            CharEscape::Solidus => b"/",
            CharEscape::Backspace => b"\\b",
            CharEscape::FormFeed => b"\\f",
            CharEscape::LineFeed => b"\\n",
            CharEscape::CarriageReturn => b"\\r",
            CharEscape::Tab => b"\\t",
            CharEscape::AsciiControl(b) => {
                const HEX: &[u8; 16] = b"0123456789abcdef";
                return w.write_all(&[
                    b'\\',
                    b'u',
                    b'0',
                    b'0',
                    HEX[usize::from(b >> 4)],
                    HEX[usize::from(b & 0xf)],
                ]);
            }
        };
        w.write_all(s)
    }

    fn begin_array<W: ?Sized + Write>(&mut self, w: &mut W) -> io::Result<()> {
        self.open(w, b"[")
    }

    fn end_array<W: ?Sized + Write>(&mut self, w: &mut W) -> io::Result<()> {
        self.close(w, b"]")
    }

    fn begin_array_value<W: ?Sized + Write>(&mut self, w: &mut W, first: bool) -> io::Result<()> {
        self.item(w, first)
    }

    fn end_array_value<W: ?Sized + Write>(&mut self, _w: &mut W) -> io::Result<()> {
        self.has_value = true;
        Ok(())
    }

    fn begin_object<W: ?Sized + Write>(&mut self, w: &mut W) -> io::Result<()> {
        self.open(w, b"{")
    }

    fn end_object<W: ?Sized + Write>(&mut self, w: &mut W) -> io::Result<()> {
        self.close(w, b"}")
    }

    fn begin_object_key<W: ?Sized + Write>(&mut self, w: &mut W, first: bool) -> io::Result<()> {
        self.item(w, first)
    }

    fn begin_object_value<W: ?Sized + Write>(&mut self, w: &mut W) -> io::Result<()> {
        w.write_all(if self.pretty.is_some() { b": " } else { b":" })
    }

    fn end_object_value<W: ?Sized + Write>(&mut self, _w: &mut W) -> io::Result<()> {
        self.has_value = true;
        Ok(())
    }
}
