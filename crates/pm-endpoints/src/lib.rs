//! Port of Go `service/profile/endpoints`: parsing and matching of endpoint
//! rules such as `+ example.com`, `- 10.0.0.0/8 TCP/22` or `- L:TRAC,MAL`.

mod endpoint;
mod matchers;
mod reason;

use std::fmt;

use pm_intel::Entity;

pub use endpoint::{parse_endpoint, DomainMatchType, Endpoint, EndpointKind, IpNet};
pub use reason::Reason;

/// Marks an option as an endpoint list option (UI display hint).
pub const DISPLAY_HINT_ENDPOINT_LIST: &str = "endpoint list";

/// Annotation for the names the UI shows for endpoint list verdicts.
pub const ENDPOINT_LIST_VERDICT_NAMES_ANNOTATION: &str =
    "safing/portmaster:ui:endpoint-list:verdict-names";

/// Regex the UI and config use to sanity check endpoint list entries.
pub const LIST_ENTRY_VALIDATION_REGEX: &str = concat!(
    r"^(\+|\-) ",
    r"(! +)?",
    r"[A-z0-9\.:\-*/]+",
    r"( ",
    r"[A-z0-9*]+",
    r"(/[A-z0-9]+(\-[A-z0-9]+)?)?",
    r")?",
    r"( +#.*)?",
);

/// Result of matching an entity against an endpoint rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
#[repr(u8)]
pub enum EpResult {
    NoMatch = 0,
    MatchError = 1,
    Denied = 2,
    Permitted = 3,
}

impl EpResult {
    /// Returns whether the result decides the connection.
    pub fn is_decision(self) -> bool {
        matches!(
            self,
            EpResult::Denied | EpResult::Permitted | EpResult::MatchError
        )
    }
}

impl fmt::Display for EpResult {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            EpResult::NoMatch => "No Match",
            EpResult::MatchError => "Match Error",
            EpResult::Denied => "Denied",
            EpResult::Permitted => "Permitted",
        })
    }
}

/// Error returned for invalid endpoint definitions; the text matches Go.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("{0}")]
pub struct Error(pub String);

/// A list of endpoint rules, evaluated in order.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Endpoints(pub Vec<Endpoint>);

/// Parses a list of rules like Go `ParseEndpoints`: invalid entries are
/// skipped, and the error reports their count and the first one.
pub fn parse_endpoints<S: AsRef<str>>(entries: &[S]) -> (Endpoints, Option<Error>) {
    let mut endpoints = Vec::with_capacity(entries.len());
    let mut first_err = None;
    let mut err_count = 0usize;
    for entry in entries {
        match parse_endpoint(entry.as_ref()) {
            Ok(ep) => endpoints.push(ep),
            Err(err) => {
                err_count += 1;
                first_err.get_or_insert(err);
            }
        }
    }
    let err = first_err.map(|first| {
        Error(format!(
            "encountered {err_count} errors, first was: {first}"
        ))
    });
    (Endpoints(endpoints), err)
}

/// Validates an endpoint list config value, like Go
/// `ValidateEndpointListConfigOption`.
pub fn validate_endpoint_list<S: AsRef<str>>(entries: &[S]) -> Result<(), Error> {
    match parse_endpoints(entries).1 {
        Some(err) => Err(err),
        None => Ok(()),
    }
}

impl Endpoints {
    /// Returns whether any rule is set.
    pub fn is_set(&self) -> bool {
        !self.0.is_empty()
    }

    /// Returns the result of the first rule that matches the entity.
    pub fn match_entity(&self, entity: &mut Entity) -> (EpResult, Option<Reason>) {
        for ep in &self.0 {
            let (result, reason) = ep.matches(entity);
            if result != EpResult::NoMatch {
                return (result, reason);
            }
        }
        (EpResult::NoMatch, None)
    }

    /// Evaluates every rule against all entities before moving on to the
    /// next rule, like Go `MatchMulti`.
    pub fn match_multi(&self, entities: &mut [&mut Entity]) -> (EpResult, Option<Reason>) {
        for ep in &self.0 {
            for entity in entities.iter_mut() {
                let (result, reason) = ep.matches(entity);
                if result != EpResult::NoMatch {
                    return (result, reason);
                }
            }
        }
        (EpResult::NoMatch, None)
    }
}

impl fmt::Display for Endpoints {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("[")?;
        for (i, ep) in self.0.iter().enumerate() {
            if i > 0 {
                f.write_str(", ")?;
            }
            write!(f, "{ep}")?;
        }
        f.write_str("]")
    }
}
