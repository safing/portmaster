//! Port of Go `endpoints/reason.go`.

use std::collections::BTreeMap;
use std::fmt;

use serde::Serialize;

/// Why an endpoint rule permitted or denied a connection.
///
/// Serializes like the Go `reason` struct (the description is unexported in Go).
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Reason {
    #[serde(skip)]
    pub description: String,
    #[serde(rename = "Filter")]
    pub filter: String,
    #[serde(rename = "Value")]
    pub value: String,
    #[serde(rename = "Permitted")]
    pub permitted: bool,
    #[serde(rename = "Extra")]
    pub extra: BTreeMap<String, serde_json::Value>,
}

impl fmt::Display for Reason {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let prefix = if self.permitted {
            "allowed by rule: "
        } else {
            "denied by rule: "
        };
        // Filter always starts with "+ " or "- ".
        let rule = self.filter.get(2..).unwrap_or_default();
        write!(f, "{prefix}{} {rule}", self.description)
    }
}
