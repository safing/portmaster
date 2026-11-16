//! Types of the `io.systemd.Resolve.Monitor` varlink protocol.
//!
//! Port of Go `service/firewall/interception/dnsmonitor/varlinktypes.go`.
//! Field names and JSON keys are kept verbatim, including the two `omitempty`
//! fields of a resource record that the monitor actually reads (`name` and
//! `address`). varlink encodes byte arrays such as `address` as base64 JSON
//! strings; a numeric array is accepted as well for robustness.

use base64::Engine;
use serde::de::{self, SeqAccess, Visitor};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::fmt;

/// Key identifying a DNS resource record (Go `ResourceKey`).
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceKey {
    #[serde(default)]
    pub class: i32,
    #[serde(default)]
    pub r#type: i32,
    #[serde(default)]
    pub name: String,
}

/// A DNS resource record (Go `ResourceRecord`). Only the fields used by the
/// monitor are kept; the rest of the varlink struct is intentionally omitted.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceRecord {
    #[serde(default)]
    pub key: ResourceKey,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(
        default,
        deserialize_with = "deserialize_optional_address",
        serialize_with = "serialize_optional_address",
        skip_serializing_if = "Option::is_none"
    )]
    pub address: Option<Vec<u8>>,
}

/// One answer of a query result (Go `Answer`).
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Answer {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rr: Option<ResourceRecord>,
    #[serde(default)]
    pub raw: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ifindex: Option<i32>,
}

/// A query result event (Go `QueryResult`).
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct QueryResult {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ready: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub state: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rcode: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub errno: Option<i32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub question: Option<Vec<ResourceKey>>,
    #[serde(default, rename = "collectedQuestions", skip_serializing_if = "Option::is_none")]
    pub collected_questions: Option<Vec<ResourceKey>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub answer: Option<Vec<Answer>>,
}

/// Deserializes a varlink `bytes` field: base64 string (what Go
/// `encoding/json` decodes into `[]byte`) or an array of byte values.
fn deserialize_optional_address<'de, D>(deserializer: D) -> Result<Option<Vec<u8>>, D::Error>
where
    D: Deserializer<'de>,
{
    struct AddressVisitor;

    impl<'de> Visitor<'de> for AddressVisitor {
        type Value = Option<Vec<u8>>;

        fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            f.write_str("a base64 string, an array of bytes, or null")
        }

        fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
            Ok(None)
        }

        fn visit_none<E: de::Error>(self) -> Result<Self::Value, E> {
            Ok(None)
        }

        fn visit_str<E: de::Error>(self, value: &str) -> Result<Self::Value, E> {
            base64::engine::general_purpose::STANDARD
                .decode(value)
                .map(Some)
                .map_err(de::Error::custom)
        }

        fn visit_bytes<E: de::Error>(self, value: &[u8]) -> Result<Self::Value, E> {
            Ok(Some(value.to_vec()))
        }

        fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
            let mut bytes = Vec::with_capacity(seq.size_hint().unwrap_or(0));
            while let Some(byte) = seq.next_element::<u8>()? {
                bytes.push(byte);
            }
            Ok(Some(bytes))
        }
    }

    deserializer.deserialize_any(AddressVisitor)
}

/// Serializes the address back as a base64 string, like Go `encoding/json`.
fn serialize_optional_address<S>(
    address: &Option<Vec<u8>>,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    match address {
        Some(bytes) => serializer.serialize_str(
            &base64::engine::general_purpose::STANDARD.encode(bytes),
        ),
        None => serializer.serialize_none(),
    }
}
