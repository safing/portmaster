//! Go-compatible number parsing and formatting.
//!
//! The query package surfaces `strconv` errors verbatim and prints floats with
//! `fmt.Sprintf("%g")`, so the port needs the same output. Only the parts the
//! query package uses are implemented.
//!
//! Known deviations from Go, none of which affect valid query text:
//! `strconv.ParseFloat` accepts hexadecimal floats (`0x1p-2`) and underscores in
//! Go; here they are rejected. `is_print` approximates `unicode.IsPrint`.

use std::fmt;

/// Why a `strconv`-style parse failed.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NumErrorKind {
    InvalidSyntax,
    Range,
}

impl NumErrorKind {
    fn message(self) -> &'static str {
        match self {
            NumErrorKind::InvalidSyntax => "invalid syntax",
            NumErrorKind::Range => "value out of range",
        }
    }
}

/// A parse error that renders exactly like Go's `*strconv.NumError`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NumError {
    pub func: &'static str,
    pub num: Vec<u8>,
    pub kind: NumErrorKind,
}

impl fmt::Display for NumError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{}: parsing {}: {}",
            self.func,
            quote(&self.num),
            self.kind.message()
        )
    }
}

impl std::error::Error for NumError {}

/// Formats a float like Go's `fmt.Sprintf("%g", v)` (shortest round-trip form,
/// switching to exponent notation outside `[-4, 6)` of decimal exponents).
pub fn format_float_g(v: f64) -> String {
    if v.is_nan() {
        return "NaN".to_string();
    }
    if v.is_infinite() {
        return if v.is_sign_positive() { "+Inf" } else { "-Inf" }.to_string();
    }
    if v == 0.0 {
        return if v.is_sign_negative() { "-0" } else { "0" }.to_string();
    }

    // Rust's `{:e}` also produces the shortest round-trip mantissa.
    let sci = format!("{v:e}");
    let (mantissa, exponent) = sci
        .split_once('e')
        .expect("LowerExp always has an exponent");
    let exponent: i32 = exponent.parse().expect("LowerExp exponent is an integer");

    let negative = mantissa.starts_with('-');
    let mantissa = mantissa.trim_start_matches('-');
    let digits: String = mantissa.chars().filter(|c| *c != '.').collect();
    let digits = digits.trim_end_matches('0');
    let digits = if digits.is_empty() { "0" } else { digits };

    let mut out = String::with_capacity(digits.len() + 6);
    if negative {
        out.push('-');
    }

    if !(-4..6).contains(&exponent) {
        out.push_str(&digits[..1]);
        if digits.len() > 1 {
            out.push('.');
            out.push_str(&digits[1..]);
        }
        out.push('e');
        out.push(if exponent < 0 { '-' } else { '+' });
        let abs = exponent.unsigned_abs();
        if abs < 10 {
            out.push('0');
        }
        out.push_str(&abs.to_string());
        return out;
    }

    let digit_count = digits.len() as i32;
    if exponent >= 0 {
        let int_len = exponent + 1;
        if digit_count <= int_len {
            out.push_str(digits);
            for _ in 0..(int_len - digit_count) {
                out.push('0');
            }
        } else {
            let split = int_len as usize;
            out.push_str(&digits[..split]);
            out.push('.');
            out.push_str(&digits[split..]);
        }
    } else {
        out.push_str("0.");
        for _ in 0..(-exponent - 1) {
            out.push('0');
        }
        out.push_str(digits);
    }
    out
}

/// Parses a signed integer like Go `strconv.ParseInt(s, 10, bits)`.
pub fn parse_int(s: &[u8], bits: u32) -> Result<i64, NumError> {
    let error = |kind| NumError {
        func: "strconv.ParseInt",
        num: s.to_vec(),
        kind,
    };

    let Ok(text) = std::str::from_utf8(s) else {
        return Err(error(NumErrorKind::InvalidSyntax));
    };
    let body = text.strip_prefix(['+', '-']).unwrap_or(text);
    if body.is_empty() || !body.bytes().all(|b| b.is_ascii_digit()) {
        return Err(error(NumErrorKind::InvalidSyntax));
    }
    let parsed: i128 = text.parse().map_err(|_| error(NumErrorKind::Range))?;

    let min = -(1i128 << (bits - 1));
    let max = (1i128 << (bits - 1)) - 1;
    if parsed < min || parsed > max {
        return Err(error(NumErrorKind::Range));
    }
    Ok(parsed as i64)
}

/// Parses a float like Go `strconv.ParseFloat(s, 64)`.
pub fn parse_float(s: &[u8]) -> Result<f64, NumError> {
    let error = |kind| NumError {
        func: "strconv.ParseFloat",
        num: s.to_vec(),
        kind,
    };

    let Ok(text) = std::str::from_utf8(s) else {
        return Err(error(NumErrorKind::InvalidSyntax));
    };

    let (sign, body) = match text.as_bytes().first() {
        Some(b'+') => (1.0, &text[1..]),
        Some(b'-') => (-1.0, &text[1..]),
        _ => (1.0, text),
    };
    match body.to_ascii_lowercase().as_str() {
        "inf" | "infinity" => return Ok(sign * f64::INFINITY),
        "nan" => return Ok(f64::NAN),
        _ => {}
    }

    let value: f64 = text
        .parse()
        .map_err(|_| error(NumErrorKind::InvalidSyntax))?;
    if value.is_infinite() {
        return Err(error(NumErrorKind::Range));
    }
    Ok(value)
}

/// Parses a bool like Go `strconv.ParseBool(s)`.
pub fn parse_bool(s: &[u8]) -> Result<bool, NumError> {
    let error = || NumError {
        func: "strconv.ParseBool",
        num: s.to_vec(),
        kind: NumErrorKind::InvalidSyntax,
    };

    match s {
        b"1" | b"t" | b"T" | b"TRUE" | b"true" | b"True" => Ok(true),
        b"0" | b"f" | b"F" | b"FALSE" | b"false" | b"False" => Ok(false),
        _ => Err(error()),
    }
}

/// Quotes a string like Go `strconv.Quote`.
pub fn quote(s: &[u8]) -> String {
    use std::fmt::Write;

    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    let mut rest = s;
    loop {
        match std::str::from_utf8(rest) {
            Ok(valid) => {
                for c in valid.chars() {
                    push_escaped(&mut out, c);
                }
                break;
            }
            Err(err) => {
                let valid = std::str::from_utf8(&rest[..err.valid_up_to()]).expect("valid prefix");
                for c in valid.chars() {
                    push_escaped(&mut out, c);
                }
                let Some(bad) = rest.get(err.valid_up_to()) else {
                    break;
                };
                let _ = write!(out, "\\x{bad:02x}");
                rest = &rest[err.valid_up_to() + 1..];
            }
        }
    }
    out.push('"');
    out
}

fn push_escaped(out: &mut String, c: char) {
    use std::fmt::Write;

    match c {
        '"' => out.push_str("\\\""),
        '\\' => out.push_str("\\\\"),
        '\x07' => out.push_str("\\a"),
        '\x08' => out.push_str("\\b"),
        '\x0c' => out.push_str("\\f"),
        '\n' => out.push_str("\\n"),
        '\r' => out.push_str("\\r"),
        '\t' => out.push_str("\\t"),
        '\x0b' => out.push_str("\\v"),
        _ if is_print(c) => out.push(c),
        _ if (c as u32) < 0x80 => {
            let _ = write!(out, "\\x{:02x}", c as u32);
        }
        _ if (c as u32) < 0x10000 => {
            let _ = write!(out, "\\u{:04x}", c as u32);
        }
        _ => {
            let _ = write!(out, "\\U{:08x}", c as u32);
        }
    }
}

/// Approximates Go `unicode.IsPrint`.
fn is_print(c: char) -> bool {
    if c.is_control() {
        return false;
    }
    !matches!(
        c,
        '\u{00ad}' | '\u{200b}'..='\u{200f}' | '\u{2028}' | '\u{2029}' | '\u{feff}'
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;

    fn cases() -> Vec<Value> {
        let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/gostrconv.json");
        let text = std::fs::read_to_string(path).unwrap();
        let v: Value = serde_json::from_str(&text).unwrap();
        v["cases"].as_array().unwrap().clone()
    }

    fn bytes(c: &Value) -> Vec<u8> {
        hex::decode(c["in_hex"].as_str().unwrap()).unwrap()
    }

    #[test]
    fn format_g_matches_go() {
        let mut seen = 0;
        for c in cases() {
            if c["kind"] != "format_g" {
                continue;
            }
            let bits = u64::from_str_radix(c["bits"].as_str().unwrap(), 16).unwrap();
            let value = f64::from_bits(bits);
            assert_eq!(
                format_float_g(value),
                c["out"].as_str().unwrap(),
                "{value:?} ({})",
                c["bits"]
            );
            seen += 1;
        }
        assert!(seen > 15);
    }

    #[test]
    fn quote_matches_go() {
        for c in cases() {
            if c["kind"] != "quote" {
                continue;
            }
            assert_eq!(
                quote(&bytes(&c)),
                c["out"].as_str().unwrap(),
                "{}",
                c["in_hex"]
            );
        }
    }

    #[test]
    fn parse_int_matches_go() {
        for c in cases() {
            if c["kind"] != "parse_int" {
                continue;
            }
            let got = parse_int(&bytes(&c), 64);
            match c.get("out").and_then(Value::as_str) {
                Some(want) => assert_eq!(got.unwrap_err().to_string(), want, "{}", c["in_hex"]),
                None => {
                    got.unwrap();
                }
            }
        }
    }

    #[test]
    fn parse_float_matches_go() {
        for c in cases() {
            if c["kind"] != "parse_float" {
                continue;
            }
            let got = parse_float(&bytes(&c));
            match c.get("out").and_then(Value::as_str) {
                Some(want) => assert_eq!(got.unwrap_err().to_string(), want, "{}", c["in_hex"]),
                None => {
                    got.unwrap();
                }
            }
        }
    }

    #[test]
    fn parse_bool_matches_go() {
        for c in cases() {
            if c["kind"] != "parse_bool" {
                continue;
            }
            let got = parse_bool(&bytes(&c));
            match c.get("out").and_then(Value::as_str) {
                Some("ok") => {
                    got.unwrap();
                }
                Some(want) => assert_eq!(got.unwrap_err().to_string(), want, "{}", c["in_hex"]),
                None => panic!("missing out"),
            }
        }
    }
}
