//! Block reasons for filter list matches, a port of Go
//! `service/intel/block_reason.go`.
//!
//! The Go type also implements `nsutil.RRProvider` and adds TXT records that
//! explain the block. That part needs the DNS message types and is not ported.

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

/// An entity that has been matched against filter lists.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ListMatch {
    #[serde(rename = "Entity")]
    pub entity: String,
    #[serde(
        rename = "ActiveLists",
        serialize_with = "serialize_empty_as_null",
        deserialize_with = "crate::deserialize_null_default"
    )]
    pub active_lists: Vec<String>,
    #[serde(
        rename = "InactiveLists",
        serialize_with = "serialize_empty_as_null",
        deserialize_with = "crate::deserialize_null_default"
    )]
    pub inactive_lists: Vec<String>,
}

/// Serializes an empty slice as `null`, like Go does for a nil slice.
fn serialize_empty_as_null<S: serde::Serializer>(
    lists: &[String],
    serializer: S,
) -> std::result::Result<S::Ok, S::Error> {
    if lists.is_empty() {
        serializer.serialize_none()
    } else {
        serializer.collect_seq(lists)
    }
}

impl ListMatch {
    /// Creates a list match.
    pub fn new(entity: &str, active_lists: Vec<String>, inactive_lists: Vec<String>) -> ListMatch {
        ListMatch {
            entity: entity.to_string(),
            active_lists,
            inactive_lists,
        }
    }
}

impl std::fmt::Display for ListMatch {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let inactive = if self.inactive_lists.is_empty() {
            String::new()
        } else {
            format!(
                " and in deactivated lists {}",
                self.inactive_lists.join(", ")
            )
        };
        write!(
            f,
            "{} in activated lists {}{}",
            self.entity,
            self.active_lists.join(","),
            inactive
        )
    }
}

/// A list of filter list matches that blocked an entity.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(transparent)]
pub struct ListBlockReason(pub Vec<ListMatch>);

impl ListBlockReason {
    /// Creates a block reason from list matches.
    pub fn new(matches: Vec<ListMatch>) -> ListBlockReason {
        ListBlockReason(matches)
    }

    /// Returns whether the reason has no matches.
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }

    /// Returns the JSON form of the reason, like Go `MarshalJSON`.
    ///
    /// The reason is wrapped into a `filterlists` field so that it can be
    /// attached to other reason contexts.
    pub fn to_json(&self) -> Value {
        json!({ "filterlists": self.0 })
    }
}

impl std::fmt::Display for ListBlockReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if self.0.is_empty() {
            return Ok(());
        }
        let matches: Vec<String> = self.0.iter().map(ToString::to_string).collect();
        f.write_str(&matches.join(" and "))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn matches_are_formatted_like_go() {
        let match_ = ListMatch::new(
            "example.com.",
            vec!["list-a".to_string(), "list-b".to_string()],
            vec!["list-c".to_string()],
        );
        assert_eq!(
            match_.to_string(),
            "example.com. in activated lists list-a,list-b and in deactivated lists list-c"
        );

        // Without inactive lists the suffix is omitted.
        let match_ = ListMatch::new("192.0.2.10", vec!["list-b".to_string()], Vec::new());
        assert_eq!(match_.to_string(), "192.0.2.10 in activated lists list-b");

        let reason = ListBlockReason::new(vec![
            ListMatch::new("example.com.", vec!["list-a".to_string()], Vec::new()),
            ListMatch::new("192.0.2.10", vec!["list-b".to_string()], Vec::new()),
        ]);
        assert_eq!(
            reason.to_string(),
            "example.com. in activated lists list-a and 192.0.2.10 in activated lists list-b"
        );

        // An empty reason formats as an empty string.
        assert_eq!(ListBlockReason::default().to_string(), "");
        assert!(ListBlockReason::default().is_empty());
    }
}
