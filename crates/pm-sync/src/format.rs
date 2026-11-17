//! Serialization and parsing of the share-able export format.
//!
//! JSON is serialized like Go `encoding/json` (HTML escaping) and then wrapped
//! with a jess checksum exactly like `filesig.AddJSONChecksum`. YAML uses a
//! small emitter matching `gopkg.in/yaml.v3` (4-space indent, sorted map keys,
//! Go-compatible scalar quoting) and `filesig.AddYAMLChecksum`.

use blake2::digest::consts::U32;
use blake2::{Blake2b, Digest};
use serde::de::DeserializeOwned;
use serde::Serialize;
use serde_json::Value;

use crate::{err_incompatible_format, err_invalid_import_request, SyncError};

/// Minimal YAML tree used to control emission (Go struct order vs. sorted maps).
pub enum YamlNode {
    Scalar(String),
    /// Already formatted scalar (numbers, booleans, timestamps).
    Raw(String),
    Map(Vec<(String, YamlNode)>),
    Seq(Vec<YamlNode>),
}

/// Maps Go `dsd.FormatFromAccept`: empty / wildcard means JSON.
pub fn format_from_accept(accept: &str) -> Option<u8> {
    if accept.is_empty() {
        return Some(pm_dsd::format::JSON);
    }
    let mut wildcard = false;
    for part in accept.split(',') {
        let mut mime = part.trim();
        // Go strings.Cut keeps the part BEFORE ';', dropping q/charset params.
        if let Some((main, _)) = mime.split_once(';') {
            mime = main;
        }
        if mime.contains('/') {
            mime = mime.split_once('/').map(|(_, rest)| rest).unwrap_or(mime);
        }
        let mime = mime.trim().to_ascii_lowercase();
        let format = match mime.as_str() {
            "json" => pm_dsd::format::JSON,
            "yaml" | "yml" => pm_dsd::format::YAML,
            "cbor" => pm_dsd::format::CBOR,
            "msgpack" => pm_dsd::format::MSG_PACK,
            "*" => {
                wildcard = true;
                continue;
            }
            _ => continue,
        };
        return Some(format);
    }
    if wildcard {
        return Some(pm_dsd::format::JSON);
    }
    None
}

/// Base58 of the BLAKE2b-256 labeled hash Go `filesig` adds to a document.
fn blake2b_256_base58(content: &[u8]) -> String {
    let digest = Blake2b::<U32>::digest(content);
    // lhash.Bytes(): varint(25 = BLAKE2b_256), varint(32), digest.
    let mut raw = Vec::with_capacity(34);
    raw.push(25);
    raw.push(32);
    raw.extend_from_slice(&digest);
    bs58::encode(raw).into_string()
}

/// Port of `filesig.AddJSONChecksum`.
fn add_json_checksum(data: &[u8]) -> Result<Vec<u8>, String> {
    let split = pm_jess::json_split(data).ok_or_else(|| "invalid json".to_string())?;
    let sum = blake2b_256_base58(&split.content);

    // Insert the checksum as the last object member (sjson appends new keys),
    // then run the final (unsorted) pretty print, like `jsonAddMeta`.
    let content = String::from_utf8_lossy(&split.content);
    let trimmed = content.trim_end();
    let close = trimmed
        .rfind('}')
        .ok_or_else(|| "invalid json structure: invalid json".to_string())?;
    let needs_comma = !trimmed[..close].trim_end().ends_with('{');
    let mut with_meta = String::with_capacity(trimmed.len() + sum.len() + 32);
    with_meta.push_str(&trimmed[..close]);
    if needs_comma {
        with_meta.push(',');
    }
    with_meta.push_str("\"_jess-checksum\":\"");
    with_meta.push_str(&sum);
    with_meta.push_str("\"}");

    let opts = pm_jess::pretty::Options {
        width: 200,
        indent: b" ".to_vec(),
        sort_keys: false,
    };
    Ok(pm_jess::pretty::pretty_options(
        with_meta.as_bytes(),
        &opts,
    ))
}

/// Splits a YAML text file into its trimmed content and `jess-` metadata lines.
fn yaml_split(data: &[u8]) -> (Vec<u8>, Vec<String>) {
    let text = String::from_utf8_lossy(data);
    let mut content = Vec::with_capacity(data.len());
    let mut meta = Vec::new();
    for line in text.split_inclusive('\n') {
        if line.starts_with("# jess-") {
            meta.push(line.trim_start_matches('#').trim().to_string());
        } else {
            content.extend_from_slice(line.as_bytes());
        }
    }
    let content = trim_ascii(&content).to_vec();
    (content, meta)
}

fn trim_ascii(mut data: &[u8]) -> &[u8] {
    while let [first, rest @ ..] = data {
        if first.is_ascii_whitespace() {
            data = rest;
        } else {
            break;
        }
    }
    while let [rest @ .., last] = data {
        if last.is_ascii_whitespace() {
            data = rest;
        } else {
            break;
        }
    }
    data
}

/// Port of `filesig.AddYAMLChecksum` with `TextPlacementBottom`.
fn add_yaml_checksum(data: &[u8]) -> Result<Vec<u8>, String> {
    let (content, _) = yaml_split(data);
    let sum = blake2b_256_base58(&content);
    let mut out = content;
    out.extend_from_slice(b"\n\n# jess-checksum: ");
    out.extend_from_slice(sum.as_bytes());
    out.push(b'\n');
    Ok(out)
}

/// Port of `filesig.VerifyYAMLChecksum`. Like Go, a document without any
/// checksum lines is accepted here: `parseExport` ignores
/// `filesig.ErrChecksumMissing`. Real verification failures still return an
/// error.
fn verify_yaml_checksum(data: &[u8]) -> Result<(), String> {
    let (content, meta) = yaml_split(data);
    for line in &meta {
        if let Some(value) = line.strip_prefix("jess-checksum") {
            let value = value.trim().trim_matches(|c| c == ':' || c == '=' || c == ' ');
            let hash = pm_jess::lhash::LabeledHash::from_base58(value)
                .map_err(|error| format!("checksum does not match: failed to parse labeled hash: {error}"))?;
            if !hash.matches(&content) {
                return Err("checksum does not match".to_string());
            }
        }
    }
    // Go reports ErrChecksumMissing when nothing was verified; parseExport
    // explicitly ignores it, so an export without a checksum is still valid.
    Ok(())
}

/// An export that can also be rendered as Go-yaml.v3 compatible YAML.
pub trait YamlSerializable: Serialize {
    fn yaml_root(&self) -> YamlNode;
}

/// Serializes an export with the format selected by the Accept header.
/// Returns the data and the MIME type Go would put in `Content-Type`.
pub fn serialize_export<T: YamlSerializable>(
    export: &T,
    accept: &str,
) -> Result<(Vec<u8>, &'static str), SyncError> {
    match format_from_accept(accept) {
        Some(pm_dsd::format::JSON) => {
            let compact = pm_dsd::gojson::to_vec(export)
                .map_err(|error| SyncError::new(500, format!("failed to serialize: {error}")))?;
            let data = add_json_checksum(&compact)
                .map_err(|error| SyncError::new(500, format!("failed to serialize: {error}")))?;
            Ok((data, "application/json"))
        }
        Some(pm_dsd::format::YAML) => {
            let data = emit_root(&export.yaml_root());
            let data = add_yaml_checksum(data.as_bytes())
                .map_err(|error| SyncError::new(500, format!("failed to serialize: {error}")))?;
            Ok((data, "application/yaml"))
        }
        _ => Err(err_incompatible_format()),
    }
}

impl YamlSerializable for crate::SettingsExport {
    fn yaml_root(&self) -> YamlNode {
        YamlNode::Map(vec![
            ("type".into(), YamlNode::Scalar(self.type_.clone())),
            ("config".into(), value_to_node(&Value::Object(self.config.clone()))),
        ])
    }
}

impl YamlSerializable for crate::SingleSettingExport {
    fn yaml_root(&self) -> YamlNode {
        YamlNode::Map(vec![
            ("type".into(), YamlNode::Scalar(self.type_.clone())),
            ("id".into(), YamlNode::Scalar(self.id.clone())),
            ("value".into(), value_to_node(&self.value)),
        ])
    }
}

impl YamlSerializable for crate::ProfileExport {
    fn yaml_root(&self) -> YamlNode {
        profile_export_root(self)
    }
}

/// Serializes a profile export. YAML with an icon is split into profile data
/// and a separate icon part, like Go `serializeProfileExport`.
pub fn serialize_profile_export(
    export: &crate::ProfileExport,
    accept: &str,
) -> Result<(Vec<u8>, &'static str), SyncError> {
    let format = format_from_accept(accept);
    if export.icon_data.is_empty() || format != Some(pm_dsd::format::YAML) {
        return serialize_export(export, accept);
    }

    let mut without_icon = export.clone();
    without_icon.icon_data.clear();
    let profile_data = emit_root(&profile_export_root(&without_icon));

    let mut icon_data = String::from("iconData: ");
    icon_data.push_str(&scalar(&export.icon_data));
    icon_data.push('\n');

    let comment = "\n\
# The application icon below is the property of its respective owner.\n\
# The icon is used for identification purposes only, and does not imply any endorsement or affiliation with their respective owners.\n\
# It is the sole responsibility of the individual or entity sharing this dataset to ensure they have the necessary permissions to do so.\n";

    let joined = format!("{profile_data}{comment}{icon_data}");
    let data = add_yaml_checksum(joined.as_bytes())
        .map_err(|error| SyncError::new(500, format!("failed to add checksum: {error}")))?;
    Ok((data, "application/yaml"))
}

/// Go `dsd.MimeLoad`: decodes the raw payload according to the MIME type.
pub fn mime_load<T: DeserializeOwned>(data: &[u8], mime: &str) -> Result<T, String> {
    match format_from_accept(mime) {
        Some(pm_dsd::format::JSON) => serde_json::from_slice(data).map_err(|error| error.to_string()),
        Some(pm_dsd::format::YAML) => serde_yaml::from_slice(data).map_err(|error| error.to_string()),
        Some(pm_dsd::format::CBOR) => ciborium::from_reader(data).map_err(|error| error.to_string()),
        Some(pm_dsd::format::MSG_PACK) => rmp_serde::from_slice(data).map_err(|error| error.to_string()),
        _ => Err("dsd: format is incompatible with operation".to_string()),
    }
}

/// Parses an export with the format selected by the MIME type and verifies its
/// checksum, like Go `parseExport`.
pub fn parse_export<T: DeserializeOwned>(data: &[u8], mime: &str) -> Result<(T, u8), SyncError> {
    let format = format_from_accept(mime).ok_or_else(err_incompatible_format)?;
    let value = mime_load(data, mime).map_err(|error| {
        err_invalid_import_request().wrap(&format!("failed to parse export: {error}"))
    })?;

    let verified = match format {
        pm_dsd::format::JSON => match pm_jess::verify_json_checksum(data) {
            // Go ignores filesig.ErrChecksumMissing in parseExport; only real
            // verification failures (mismatch, malformed hash) are fatal.
            Ok(()) | Err(pm_jess::Error::ChecksumMissing) => Ok(()),
            Err(error) => Err(format!("checksum does not match: {error}")),
        },
        pm_dsd::format::YAML => verify_yaml_checksum(data),
        _ => Ok(()),
    };
    verified.map_err(|error| SyncError::new(500, format!("failed to verify checksum: {error}")))?;
    Ok((value, format))
}

/// Emits a YAML tree at the document root.
fn emit_root(node: &YamlNode) -> String {
    let mut out = String::new();
    match node {
        YamlNode::Map(fields) => emit_map(fields, 0, None, &mut out),
        other => emit_standalone(other, 0, &mut out),
    }
    out
}

fn value_to_node(value: &Value) -> YamlNode {
    match value {
        Value::Object(map) => {
            let mut fields: Vec<(String, YamlNode)> = map
                .iter()
                .map(|(key, value)| (key.clone(), value_to_node(value)))
                .collect();
            fields.sort_by(|a, b| a.0.cmp(&b.0));
            YamlNode::Map(fields)
        }
        Value::Array(items) => YamlNode::Seq(items.iter().map(value_to_node).collect()),
        Value::String(text) => YamlNode::Scalar(text.clone()),
        Value::Number(number) => YamlNode::Raw(number.to_string()),
        Value::Bool(flag) => YamlNode::Raw(flag.to_string()),
        Value::Null => YamlNode::Raw("null".to_string()),
    }
}

/// Builds the YAML tree of a profile export in Go struct field order.
fn profile_export_root(export: &crate::ProfileExport) -> YamlNode {
    use crate::ProfileExport;

    fn opt(fields: &mut Vec<(String, YamlNode)>, key: &str, condition: bool, node: YamlNode) {
        if condition {
            fields.push((key.to_string(), node));
        }
    }

    let ProfileExport {
        type_,
        id,
        source,
        name,
        description,
        homepage,
        presentation_path,
        use_presentation_path,
        icon_data,
        fingerprints,
        config,
        last_edited,
        created,
        internal,
    } = export;

    let mut fields: Vec<(String, YamlNode)> = vec![("type".into(), YamlNode::Scalar(type_.clone()))];
    opt(&mut fields, "id", !id.is_empty(), YamlNode::Scalar(id.clone()));
    opt(&mut fields, "source", !source.is_empty(), YamlNode::Scalar(source.clone()));
    fields.push(("name".into(), YamlNode::Scalar(name.clone())));
    opt(&mut fields, "description", !description.is_empty(), YamlNode::Scalar(description.clone()));
    opt(&mut fields, "homepage", !homepage.is_empty(), YamlNode::Scalar(homepage.clone()));
    opt(&mut fields, "presPath", !presentation_path.is_empty(), YamlNode::Scalar(presentation_path.clone()));
    opt(&mut fields, "usePresPath", *use_presentation_path, YamlNode::Raw("true".into()));
    opt(&mut fields, "iconData", !icon_data.is_empty(), YamlNode::Scalar(icon_data.clone()));

    let prints: Vec<YamlNode> = fingerprints
        .iter()
        .map(|fp| {
            let mut map = vec![("type".to_string(), YamlNode::Scalar(fp.type_.clone()))];
            if !fp.key.is_empty() {
                map.push(("key".to_string(), YamlNode::Scalar(fp.key.clone())));
            }
            map.push(("operation".to_string(), YamlNode::Scalar(fp.operation.clone())));
            map.push(("value".to_string(), YamlNode::Scalar(fp.value.clone())));
            if !fp.merged_from.is_empty() {
                map.push(("mergedFrom".to_string(), YamlNode::Scalar(fp.merged_from.clone())));
            }
            YamlNode::Map(map)
        })
        .collect();
    fields.push(("fingerprints".into(), YamlNode::Seq(prints)));

    if !config.is_empty() {
        fields.push(("config".into(), value_to_node(&Value::Object(config.clone()))));
    }
    if let Some(value) = last_edited {
        fields.push(("lastEdited".into(), YamlNode::Raw(value.clone())));
    }
    if let Some(value) = created {
        fields.push(("created".into(), YamlNode::Raw(value.clone())));
    }
    if *internal {
        fields.push(("internal".into(), YamlNode::Raw("true".into())));
    }

    YamlNode::Map(fields)
}

fn spaces(count: usize) -> String {
    " ".repeat(count)
}

fn emit_standalone(node: &YamlNode, indent: usize, out: &mut String) {
    match node {
        YamlNode::Map(fields) => emit_map(fields, indent, None, out),
        YamlNode::Seq(items) => emit_seq(items, indent, out),
        YamlNode::Scalar(value) => {
            out.push_str(&spaces(indent));
            out.push_str(&scalar(value));
            out.push('\n');
        }
        YamlNode::Raw(value) => {
            out.push_str(&spaces(indent));
            out.push_str(value);
            out.push('\n');
        }
    }
}

fn emit_map(fields: &[(String, YamlNode)], indent: usize, first_prefix: Option<&str>, out: &mut String) {
    for (index, (key, child)) in fields.iter().enumerate() {
        if index == 0 {
            if let Some(prefix) = first_prefix {
                // The caller already emitted the sequence indentation.
                out.push_str(prefix);
            } else {
                out.push_str(&spaces(indent));
            }
        } else {
            out.push_str(&spaces(indent));
        }
        out.push_str(key);
        out.push(':');
        emit_child(child, indent, out);
    }
}

fn emit_child(child: &YamlNode, key_indent: usize, out: &mut String) {
    match child {
        YamlNode::Scalar(value) => {
            out.push(' ');
            out.push_str(&scalar(value));
            out.push('\n');
        }
        YamlNode::Raw(value) => {
            out.push(' ');
            out.push_str(value);
            out.push('\n');
        }
        YamlNode::Map(fields) if fields.is_empty() => out.push_str(" {}\n"),
        YamlNode::Seq(items) if items.is_empty() => out.push_str(" []\n"),
        YamlNode::Map(fields) => {
            out.push('\n');
            emit_map(fields, key_indent + 4, None, out);
        }
        YamlNode::Seq(items) => {
            out.push('\n');
            emit_seq(items, key_indent + 4, out);
        }
    }
}

fn emit_seq(items: &[YamlNode], indent: usize, out: &mut String) {
    for item in items {
        match item {
            YamlNode::Scalar(value) => {
                out.push_str(&spaces(indent));
                out.push_str("- ");
                out.push_str(&scalar(value));
                out.push('\n');
            }
            YamlNode::Raw(value) => {
                out.push_str(&spaces(indent));
                out.push_str("- ");
                out.push_str(value);
                out.push('\n');
            }
            YamlNode::Map(fields) => {
                out.push_str(&spaces(indent));
                emit_map(fields, indent + 2, Some("- "), out);
            }
            YamlNode::Seq(inner) => {
                out.push_str(&spaces(indent));
                out.push_str("-\n");
                emit_seq(inner, indent + 4, out);
            }
        }
    }
}

/// Quotes a string scalar the way Go yaml.v3 does for the subset used here.
fn scalar(value: &str) -> String {
    if value.is_empty() {
        return "\"\"".to_string();
    }
    if value.contains('\n') {
        let mut out = String::from("|-\n");
        for line in value.split('\n') {
            out.push_str("    ");
            out.push_str(line);
            out.push('\n');
        }
        // Drop the trailing newline added for the (empty) last split segment.
        out.pop();
        return out;
    }
    if value.chars().any(|c| (c as u32) < 0x20 && c != '\n' || (c as u32) > 0xffff) {
        let mut out = String::from("\"");
        for c in value.chars() {
            match c {
                '\t' => out.push_str("\\t"),
                '\r' => out.push_str("\\r"),
                '\n' => out.push_str("\\n"),
                '\\' => out.push_str("\\\\"),
                '"' => out.push_str("\\\""),
                c if (c as u32) < 0x20 || (c as u32) > 0xffff => {
                    out.push_str(&format!("\\U{:08X}", c as u32));
                }
                c => out.push(c),
            }
        }
        out.push('"');
        return out;
    }

    let first = value.chars().next().unwrap();
    let indicator = matches!(
        first,
        '-' | '?' | ':' | '[' | ']' | '{' | '}' | ',' | '#' | '&' | '*' | '!' | '|' | '>' | '\'' | '"' | '%' | '@' | '`'
    ) && (value.len() == 1 || matches!(first, '[' | ']' | '{' | '}' | ',' | '#' | '&' | '*' | '!' | '|' | '>' | '\'' | '"' | '%' | '@' | '`') || value.as_bytes().get(1) == Some(&b' '));
    let needs_space_quote = value.starts_with(' ') || value.ends_with(' ')
        || value.contains(": ") || value.contains(" #");
    let resolves = resolves_to_non_string(value);

    if resolves {
        return format!("\"{}\"", value.replace('\\', "\\\\").replace('"', "\\\""));
    }
    if indicator || needs_space_quote {
        return format!("'{}'", value.replace('\'', "''"));
    }
    value.to_string()
}

/// Whether yaml.v3 would resolve the plain scalar to a non-string type.
fn resolves_to_non_string(value: &str) -> bool {
    let lower = value.to_ascii_lowercase();
    if matches!(
        lower.as_str(),
        "true" | "false" | "yes" | "no" | "on" | "off" | "y" | "n" | "null" | "~"
    ) {
        return true;
    }
    if value.parse::<i64>().is_ok() || value.parse::<f64>().is_ok() {
        return true;
    }
    // Dates and timestamps.
    let bytes = value.as_bytes();
    if bytes.len() >= 10
        && bytes[0..4].iter().all(u8::is_ascii_digit)
        && bytes.get(4) == Some(&b'-')
        && bytes[5..7].iter().all(u8::is_ascii_digit)
        && bytes.get(7) == Some(&b'-')
        && bytes[8..10].iter().all(u8::is_ascii_digit)
    {
        return true;
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accept_formats_match_go() {
        assert_eq!(format_from_accept(""), Some(pm_dsd::format::JSON));
        assert_eq!(format_from_accept("application/json"), Some(pm_dsd::format::JSON));
        assert_eq!(format_from_accept("application/yaml"), Some(pm_dsd::format::YAML));
        assert_eq!(format_from_accept("text/yaml, application/json"), Some(pm_dsd::format::YAML));
        assert_eq!(format_from_accept("application/xml, */*"), Some(pm_dsd::format::JSON));
        assert_eq!(format_from_accept("application/xml"), None);
    }
}

#[cfg(test)]
mod accept_tests {
    use super::*;

    #[test]
    fn accept_with_quality_parameters_matches_json() {
        assert_eq!(
            format_from_accept("application/xml;q=0.9, application/json; charset=utf-8, */*;q=0.8"),
            Some(pm_dsd::format::JSON)
        );
    }

    #[test]
    fn import_accepts_mime_params_and_missing_checksum() {
        // Go's parseExport uses the MIME before ';' and ignores
        // filesig.ErrChecksumMissing, so a checksum-less export with a
        // parameterised content type must still import.
        let json = br#"{"type":"settings","config":{}}"#;
        let (parsed, format) =
            crate::parse_export::<crate::SettingsExport>(json, "application/json; charset=utf-8")
                .expect("parse json export without checksum");
        assert_eq!(format, pm_dsd::format::JSON);
        assert_eq!(parsed.type_, "settings");

        let yaml = b"type: settings\nconfig: {}\n";
        let (parsed, format) = crate::parse_export::<crate::SettingsExport>(yaml, "application/yaml")
            .expect("parse yaml export without checksum");
        assert_eq!(format, pm_dsd::format::YAML);
        assert_eq!(parsed.type_, "settings");
    }
}
