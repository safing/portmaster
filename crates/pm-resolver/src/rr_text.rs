//! Zone-file text of resource records, as stored in cached name records.
//!
//! The output is byte-equal to Go `miekg/dns` `RR.String()` for the types with
//! a presentation form here (A, AAAA, CNAME, NS, PTR, MX, TXT, SOA, SRV). Every
//! other type is written in the RFC 3597 generic form
//! (`name ttl IN TYPEn \# len hex`), which Go reads for any known type. Text
//! of other types written by Go (HTTPS, CAA, ...) is rejected when parsing.

use std::net::{Ipv4Addr, Ipv6Addr};
use std::str::FromStr;

use hickory_proto::rr::rdata::{A, AAAA, CNAME, MX, NS, PTR, SOA, SRV, TXT};
use hickory_proto::rr::{DNSClass, Name, RData, Record, RecordType};
use hickory_proto::serialize::binary::{BinDecoder, BinEncodable, BinEncoder, Restrict};

/// Errors of the record text parser.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("{0}")]
pub struct TextError(pub String);

fn err<T>(message: impl Into<String>) -> Result<T, TextError> {
    Err(TextError(message.into()))
}

/// Formats a domain name like Go `sprintName`.
pub fn format_name(name: &Name) -> String {
    let mut out = String::new();
    let mut any = false;
    for label in name.iter() {
        any = true;
        for &b in label {
            match b {
                b'.' | b' ' | b'\'' | b'@' | b';' | b'(' | b')' | b'"' | b'\\' => {
                    out.push('\\');
                    out.push(b as char);
                }
                b if !(b' '..=b'~').contains(&b) => out.push_str(&format!("\\{:03}", b)),
                b => out.push(b as char),
            }
        }
        out.push('.');
    }
    if !any {
        out.push('.');
    }
    out
}

fn format_txt(data: &[u8]) -> String {
    let mut out = String::from("\"");
    for &b in data {
        match b {
            b'"' | b'\\' => {
                out.push('\\');
                out.push(b as char);
            }
            b if !(b' '..=b'~').contains(&b) => out.push_str(&format!("\\{:03}", b)),
            b => out.push(b as char),
        }
    }
    out.push('"');
    out
}

fn class_name(class: DNSClass) -> String {
    match class {
        DNSClass::IN => "IN".to_string(),
        DNSClass::CH => "CH".to_string(),
        DNSClass::HS => "HS".to_string(),
        DNSClass::NONE => "NONE".to_string(),
        DNSClass::ANY => "ANY".to_string(),
        other => format!("CLASS{}", u16::from(other)),
    }
}

fn class_from_name(name: &str) -> Option<DNSClass> {
    match name.to_ascii_uppercase().as_str() {
        "IN" => Some(DNSClass::IN),
        "CH" => Some(DNSClass::CH),
        "HS" => Some(DNSClass::HS),
        "NONE" => Some(DNSClass::NONE),
        "ANY" => Some(DNSClass::ANY),
        other => other
            .strip_prefix("CLASS")
            .and_then(|n| n.parse::<u16>().ok())
            .map(DNSClass::from),
    }
}

/// Wire form of the record data, without name compression.
fn rdata_wire(rdata: &RData) -> Option<Vec<u8>> {
    let mut buf = Vec::new();
    {
        let mut encoder = BinEncoder::new(&mut buf);
        rdata.emit(&mut encoder).ok()?;
    }
    Some(buf)
}

/// Returns whether the record type is part of persisted records. Go drops OPT
/// and NULL because they cannot be read back.
pub fn is_persistable(record: &Record) -> bool {
    !matches!(
        record.data.record_type(),
        RecordType::OPT | RecordType::NULL
    )
}

/// Formats the record like Go `RR.String()` (generic form for other types).
/// Returns `None` if the record data cannot be encoded.
pub fn format_rr(record: &Record) -> Option<String> {
    let head = format!(
        "{}\t{}\t{}\t",
        format_name(&record.name),
        record.ttl,
        class_name(record.dns_class)
    );
    let rdata = &record.data;
    let (type_name, text) = match rdata {
        RData::A(a) => ("A".to_string(), a.0.to_string()),
        RData::AAAA(aaaa) => ("AAAA".to_string(), aaaa.0.to_string()),
        RData::CNAME(name) => ("CNAME".to_string(), format_name(&name.0)),
        RData::NS(name) => ("NS".to_string(), format_name(&name.0)),
        RData::PTR(name) => ("PTR".to_string(), format_name(&name.0)),
        RData::MX(mx) => (
            "MX".to_string(),
            format!("{} {}", mx.preference, format_name(&mx.exchange)),
        ),
        RData::TXT(txt) => {
            let parts: Vec<String> = txt.txt_data.iter().map(|d| format_txt(d)).collect();
            let text = if parts.is_empty() {
                "\"\"".to_string()
            } else {
                parts.join(" ")
            };
            ("TXT".to_string(), text)
        }
        RData::SOA(soa) => (
            "SOA".to_string(),
            format!(
                "{} {} {} {} {} {} {}",
                format_name(&soa.mname),
                format_name(&soa.rname),
                soa.serial,
                soa.refresh,
                soa.retry,
                soa.expire,
                soa.minimum
            ),
        ),
        RData::SRV(srv) => (
            "SRV".to_string(),
            format!(
                "{} {} {} {}",
                srv.priority,
                srv.weight,
                srv.port,
                format_name(&srv.target)
            ),
        ),
        other => {
            let wire = match other {
                RData::Unknown { rdata, .. } => rdata.anything.clone(),
                other => rdata_wire(other)?,
            };
            (
                format!("TYPE{}", u16::from(other.record_type())),
                format!("\\# {} {}", wire.len(), hex::encode(wire)),
            )
        }
    };
    Some(format!("{head}{type_name}\t{text}"))
}

/// A token of record text.
struct Token {
    text: String,
    quoted: bool,
}

/// Splits text into tokens; quoted strings keep their raw (still escaped) body.
fn tokenize(text: &str) -> Result<Vec<Token>, TextError> {
    let mut tokens = Vec::new();
    let bytes = text.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b' ' | b'\t' | b'\r' | b'\n' => i += 1,
            b'"' => {
                let mut j = i + 1;
                let mut body = String::new();
                loop {
                    if j >= bytes.len() {
                        return err("unterminated quoted string");
                    }
                    match bytes[j] {
                        b'"' => break,
                        b'\\' => {
                            if j + 1 >= bytes.len() {
                                return err("unterminated escape");
                            }
                            body.push('\\');
                            body.push(bytes[j + 1] as char);
                            j += 2;
                        }
                        _ => {
                            // Keep multi-byte UTF-8 intact.
                            let ch = text[j..].chars().next().unwrap_or(' ');
                            body.push(ch);
                            j += ch.len_utf8();
                        }
                    }
                }
                tokens.push(Token {
                    text: body,
                    quoted: true,
                });
                i = j + 1;
            }
            _ => {
                let mut j = i;
                let mut body = String::new();
                while j < bytes.len() && !matches!(bytes[j], b' ' | b'\t' | b'\r' | b'\n') {
                    if bytes[j] == b'\\' {
                        if j + 1 >= bytes.len() {
                            return err("unterminated escape");
                        }
                        body.push('\\');
                        body.push(bytes[j + 1] as char);
                        j += 2;
                    } else {
                        let ch = text[j..].chars().next().unwrap_or(' ');
                        body.push(ch);
                        j += ch.len_utf8();
                    }
                }
                tokens.push(Token {
                    text: body,
                    quoted: false,
                });
                i = j;
            }
        }
    }
    Ok(tokens)
}

/// Resolves `\c` and `\DDD` escapes to raw bytes.
fn unescape(text: &str) -> Result<Vec<u8>, TextError> {
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'\\' {
            if i + 1 >= bytes.len() {
                return err("unterminated escape");
            }
            if i + 3 < bytes.len()
                && bytes[i + 1].is_ascii_digit()
                && bytes[i + 2].is_ascii_digit()
                && bytes[i + 3].is_ascii_digit()
            {
                let value = u32::from(bytes[i + 1] - b'0') * 100
                    + u32::from(bytes[i + 2] - b'0') * 10
                    + u32::from(bytes[i + 3] - b'0');
                if value > 255 {
                    return err("bad escape value");
                }
                out.push(value as u8);
                i += 4;
            } else {
                out.push(bytes[i + 1]);
                i += 2;
            }
        } else {
            out.push(bytes[i]);
            i += 1;
        }
    }
    Ok(out)
}

/// Parses a domain name in presentation form; it must be fully qualified.
pub fn parse_name(text: &str) -> Result<Name, TextError> {
    if text == "." {
        return Ok(Name::root());
    }
    let bytes = text.as_bytes();
    let mut labels: Vec<Vec<u8>> = Vec::new();
    let mut current: Vec<u8> = Vec::new();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'\\' => {
                if i + 1 >= bytes.len() {
                    return err("unterminated escape");
                }
                if i + 3 < bytes.len()
                    && bytes[i + 1].is_ascii_digit()
                    && bytes[i + 2].is_ascii_digit()
                    && bytes[i + 3].is_ascii_digit()
                {
                    let value = u32::from(bytes[i + 1] - b'0') * 100
                        + u32::from(bytes[i + 2] - b'0') * 10
                        + u32::from(bytes[i + 3] - b'0');
                    if value > 255 {
                        return err("bad escape value");
                    }
                    current.push(value as u8);
                    i += 4;
                } else {
                    current.push(bytes[i + 1]);
                    i += 2;
                }
            }
            b'.' => {
                if current.is_empty() {
                    return err("empty label in domain name");
                }
                labels.push(std::mem::take(&mut current));
                i += 1;
            }
            b => {
                current.push(b);
                i += 1;
            }
        }
    }
    if !current.is_empty() {
        return err(format!("domain name {text:?} is not fully qualified"));
    }
    let mut name = Name::from_labels(labels).map_err(|e| TextError(e.to_string()))?;
    name.set_fqdn(true);
    Ok(name)
}

fn number<T: FromStr>(token: Option<&Token>, what: &str) -> Result<T, TextError> {
    let Some(token) = token else {
        return err(format!("missing {what}"));
    };
    token
        .text
        .parse::<T>()
        .or_else(|_| err(format!("bad {what} {:?}", token.text)))
}

fn name_token(token: Option<&Token>, what: &str) -> Result<Name, TextError> {
    match token {
        Some(token) if !token.quoted => parse_name(&token.text),
        _ => err(format!("missing {what}")),
    }
}

fn record_type_from_name(name: &str) -> Option<RecordType> {
    let upper = name.to_ascii_uppercase();
    if let Some(n) = upper.strip_prefix("TYPE") {
        return n.parse::<u16>().ok().map(RecordType::from);
    }
    match RecordType::from_str(&upper) {
        Ok(RecordType::Unknown(_)) | Err(_) => None,
        Ok(record_type) => Some(record_type),
    }
}

/// Parses the text of a record as written by [`format_rr`] (the field order of
/// Go `RR.String()`: name, TTL, class, type, data).
pub fn parse_rr(text: &str) -> Result<Record, TextError> {
    let tokens = tokenize(text)?;
    let mut it = tokens.iter();

    let name_token = it.next().ok_or_else(|| TextError("empty record".into()))?;
    if name_token.quoted {
        return err("invalid record name");
    }
    let name = parse_name(&name_token.text)?;

    let mut ttl: Option<u32> = None;
    let mut class: Option<DNSClass> = None;
    let record_type;
    loop {
        let Some(token) = it.next() else {
            return err("missing record type");
        };
        if ttl.is_none() {
            if let Ok(value) = token.text.parse::<u32>() {
                ttl = Some(value);
                continue;
            }
        }
        if class.is_none() {
            if let Some(value) = class_from_name(&token.text) {
                class = Some(value);
                continue;
            }
        }
        match record_type_from_name(&token.text) {
            Some(value) => {
                record_type = value;
                break;
            }
            None => return err(format!("unknown record type {:?}", token.text)),
        }
    }
    let rest: Vec<&Token> = it.collect();
    let mut r = rest.iter().copied();

    let rdata = if rest.first().is_some_and(|t| !t.quoted && t.text == "\\#") {
        let length: usize = number(rest.get(1).copied(), "rdata length")?;
        let hex_text: String = rest[2..].iter().map(|t| t.text.as_str()).collect();
        let wire = hex::decode(&hex_text).map_err(|e| TextError(format!("bad rdata hex: {e}")))?;
        if wire.len() != length {
            return err("rdata length mismatch");
        }
        rdata_from_wire(record_type, &wire)?
    } else {
        match record_type {
            RecordType::A => {
                let ip: Ipv4Addr = number(r.next(), "A address")?;
                RData::A(A(ip))
            }
            RecordType::AAAA => {
                let ip: Ipv6Addr = number(r.next(), "AAAA address")?;
                RData::AAAA(AAAA(ip))
            }
            RecordType::CNAME => RData::CNAME(CNAME(name_token_of(r.next())?)),
            RecordType::NS => RData::NS(NS(name_token_of(r.next())?)),
            RecordType::PTR => RData::PTR(PTR(name_token_of(r.next())?)),
            RecordType::MX => {
                let preference: u16 = number(r.next(), "MX preference")?;
                RData::MX(MX::new(preference, name_token_of(r.next())?))
            }
            RecordType::TXT => {
                let mut parts: Vec<Vec<u8>> = Vec::new();
                for token in r.by_ref() {
                    parts.push(unescape(&token.text)?);
                }
                if parts.is_empty() {
                    return err("missing TXT data");
                }
                let mut txt = TXT::new(Vec::new());
                txt.txt_data = parts
                    .into_iter()
                    .map(Vec::into_boxed_slice)
                    .collect::<Vec<_>>()
                    .into_boxed_slice();
                RData::TXT(txt)
            }
            RecordType::SOA => {
                let mname = name_token_of(r.next())?;
                let rname = name_token_of(r.next())?;
                RData::SOA(SOA::new(
                    mname,
                    rname,
                    number(r.next(), "SOA serial")?,
                    number(r.next(), "SOA refresh")?,
                    number(r.next(), "SOA retry")?,
                    number(r.next(), "SOA expire")?,
                    number(r.next(), "SOA minimum")?,
                ))
            }
            RecordType::SRV => {
                let priority: u16 = number(r.next(), "SRV priority")?;
                let weight: u16 = number(r.next(), "SRV weight")?;
                let port: u16 = number(r.next(), "SRV port")?;
                RData::SRV(SRV::new(priority, weight, port, name_token_of(r.next())?))
            }
            other => {
                return err(format!(
                    "record type {other} is only supported in generic form"
                ))
            }
        }
    };

    let mut record = Record::from_rdata(name, ttl.unwrap_or(0), rdata);
    record.dns_class = class.unwrap_or(DNSClass::IN);
    Ok(record)
}

fn name_token_of(token: Option<&Token>) -> Result<Name, TextError> {
    name_token(token, "domain name")
}

fn rdata_from_wire(record_type: RecordType, wire: &[u8]) -> Result<RData, TextError> {
    let length = u16::try_from(wire.len()).map_err(|_| TextError("rdata too long".into()))?;
    let mut decoder = BinDecoder::new(wire);
    let rdata = RData::read(&mut decoder, record_type, Restrict::new(length))
        .map_err(|e| TextError(format!("bad rdata: {e}")))?;
    Ok(match rdata {
        // Keep unknown types opaque so they round-trip byte for byte.
        RData::Unknown { code, rdata } => RData::Unknown { code, rdata },
        other => other,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rt(text: &str) -> String {
        format_rr(&parse_rr(text).unwrap()).unwrap()
    }

    #[test]
    fn formats_like_go() {
        assert_eq!(
            rt("example.com. 300 IN A 192.0.2.1"),
            "example.com.\t300\tIN\tA\t192.0.2.1"
        );
        assert_eq!(
            rt("example.com. 17 IN MX 10 mail.example.com."),
            "example.com.\t17\tIN\tMX\t10 mail.example.com."
        );
        assert_eq!(
            rt(r#"example.com. 17 IN TXT "a" "b c" "q\"b\\s""#),
            "example.com.\t17\tIN\tTXT\t\"a\" \"b c\" \"q\\\"b\\\\s\""
        );
    }

    #[test]
    fn generic_form_roundtrips() {
        let text = "example.com. 17 IN TYPE65 \\# 13 00010000010006026832026833";
        let record = parse_rr(text).unwrap();
        assert_eq!(record.data.record_type(), RecordType::HTTPS);
        assert_eq!(
            format_rr(&record).unwrap(),
            "example.com.\t17\tIN\tTYPE65\t\\# 13 00010000010006026832026833"
        );
    }

    #[test]
    fn malformed_text_is_an_error() {
        for bad in [
            "",
            "example.com.",
            "example.com. 17 IN",
            "example.com. 17 IN A",
            "example.com. 17 IN A not-an-ip",
            "example.com 17 IN A 1.2.3.4",
            "example.com. 17 IN TXT \"open",
            "example.com. 17 IN MX x mail.",
            "example.com. 17 IN BOGUSTYPE 1",
            "example.com. 17 IN TYPE999 \\# 4 0a",
            "example.com. 17 IN TYPE999 \\# 2 zz",
            "a..b. 17 IN A 1.2.3.4",
            "\\",
        ] {
            assert!(parse_rr(bad).is_err(), "{bad:?}");
        }
    }
}
