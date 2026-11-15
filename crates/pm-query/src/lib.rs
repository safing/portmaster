//! Compatible implementation of the Go `base/database/query` package.
//!
//! A query looks like `query <db>:<key prefix> where <conditions> orderby <key>
//! limit <n> offset <n>`; conditions are built from operators such as `==`,
//! `f>=`, `sameas`, `contains`, `in`, `matches`, `is` and `exists`.
//!
//! # Compatibility notes
//!
//! * Keys, prefixes and string values are byte strings, because Go strings are
//!   byte strings and the Go parser can cut the last byte off a trailing
//!   multi-byte token. Comparisons and printing therefore work on bytes; the
//!   public getters return lossily decoded `String`s.
//! * Regexes are compiled with the `regex` crate (also RE2-style). Go and Rust
//!   differ in error messages and in handling of invalid UTF-8 patterns, so a
//!   regex that does not compile produces a compatible prefix but a different
//!   detail message.
//! * JSON payloads are parsed strictly. Go's accessor uses gjson, which accepts
//!   malformed JSON; a malformed payload simply does not match here.

mod condition;
mod gostrconv;
mod operators;
mod parser;

use pm_record::Record;

pub use condition::{Condition, Value};
pub use operators::{
    CONTAINS, ENDS_WITH, EQUALS, ERROR_PRESENT, EXISTS, FLOAT_EQUALS, FLOAT_GREATER_THAN,
    FLOAT_GREATER_THAN_OR_EQUAL, FLOAT_LESS_THAN, FLOAT_LESS_THAN_OR_EQUAL, GREATER_THAN,
    GREATER_THAN_OR_EQUAL, IN, IS, LESS_THAN, LESS_THAN_OR_EQUAL, MATCHES, SAME_AS, STARTS_WITH,
};
pub use parser::{extract_snippets, parse_query, Snippet};

/// Errors returned while parsing or checking a query.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    #[error("unexpected end at position {0}")]
    UnexpectedEnd(usize),
    #[error("queries must start with \"query\"")]
    MissingQueryWord,
    #[error("duplicate \"{clause}\" clause found at position {position}")]
    DuplicateClause { clause: String, position: usize },
    #[error("unknown clause \"{clause}\" at position {position}")]
    UnknownClause { clause: String, position: usize },
    #[error("unknown operator at position {0}")]
    UnknownOperator(usize),
    #[error("could not parse integer ({value}) at position {position}")]
    Integer { value: String, position: usize },
    #[error("you may not mix \"and\" and \"or\" (position: {0})")]
    MixedAndOr(usize),
    #[error("parenthesis ('\"') may not be used within words, please escape with '\\' (position: {0})")]
    QuoteInWord(usize),
    #[error("{0}")]
    Condition(String),
}

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, Error>;

/// A compiled query.
#[derive(Debug, Clone, PartialEq)]
pub struct Query {
    checked: bool,
    db_name: Vec<u8>,
    db_key_prefix: Vec<u8>,
    where_: Option<Condition>,
    order_by: Vec<u8>,
    limit: usize,
    offset: usize,
}

impl Query {
    /// Creates a query for the given `db:key prefix`, like Go `query.New`.
    pub fn new(prefix: &str) -> Query {
        Query::new_raw(prefix.as_bytes())
    }

    pub(crate) fn new_raw(prefix: &[u8]) -> Query {
        let (db_name, db_key_prefix) = match prefix.iter().position(|&b| b == b':') {
            Some(pos) => (&prefix[..pos], &prefix[pos + 1..]),
            None => (prefix, &prefix[prefix.len()..]),
        };
        Query {
            checked: false,
            db_name: db_name.to_vec(),
            db_key_prefix: db_key_prefix.to_vec(),
            where_: None,
            order_by: Vec::new(),
            limit: 0,
            offset: 0,
        }
    }

    /// Adds filtering, like Go `Query.Where`.
    #[must_use]
    pub fn where_(mut self, condition: Condition) -> Query {
        self.where_ = Some(condition);
        self
    }

    /// Orders results by the given key, like Go `Query.OrderBy`.
    #[must_use]
    pub fn order_by(mut self, key: &str) -> Query {
        self.order_by = key.as_bytes().to_vec();
        self
    }

    /// Limits the number of results, like Go `Query.Limit`.
    #[must_use]
    pub fn limit(mut self, limit: usize) -> Query {
        self.limit = limit;
        self
    }

    /// Sets the query offset, like Go `Query.Offset`.
    #[must_use]
    pub fn offset(mut self, offset: usize) -> Query {
        self.offset = offset;
        self
    }

    /// Checks the query for errors, like Go `Query.Check`.
    pub fn check(mut self) -> Result<Query> {
        if self.checked {
            return Ok(self);
        }
        if let Some(condition) = &self.where_ {
            condition.check()?;
        }
        self.checked = true;
        Ok(self)
    }

    /// Returns whether the query was checked.
    pub fn is_checked(&self) -> bool {
        self.checked
    }

    /// Returns whether the query has a `where` condition.
    pub fn has_where_condition(&self) -> bool {
        self.where_.is_some()
    }

    /// Returns the `where` condition, if any.
    pub fn where_condition(&self) -> Option<&Condition> {
        self.where_.as_ref()
    }

    /// Returns the order key, if any.
    pub fn order_by_key(&self) -> Option<String> {
        if self.order_by.is_empty() {
            None
        } else {
            Some(String::from_utf8_lossy(&self.order_by).into_owned())
        }
    }

    /// Returns the limit; `0` means unlimited, like in Go.
    pub fn limit_value(&self) -> usize {
        self.limit
    }

    /// Returns the offset.
    pub fn offset_value(&self) -> usize {
        self.offset
    }

    /// Checks whether the query matches a database key (without the database name).
    pub fn matches_key(&self, db_key: &str) -> bool {
        db_key.as_bytes().starts_with(&self.db_key_prefix)
    }

    /// Checks whether the query matches a record value only.
    pub fn matches_record(&self, record: &Record) -> bool {
        match &self.where_ {
            None => true,
            Some(condition) => match record.accessor() {
                Some(accessor) => condition.complies(&accessor),
                None => false,
            },
        }
    }

    /// Checks whether the query matches a record, key and value.
    pub fn matches(&self, record: &Record) -> bool {
        self.matches_key(&record.key.key) && self.matches_record(record)
    }

    /// Returns the name of the database.
    pub fn database_name(&self) -> String {
        String::from_utf8_lossy(&self.db_name).into_owned()
    }

    /// Returns the key prefix within the database.
    pub fn database_key_prefix(&self) -> String {
        String::from_utf8_lossy(&self.db_key_prefix).into_owned()
    }

    /// Returns the full `db:prefix` as written in the query.
    pub fn prefix(&self) -> String {
        let mut out = self.db_name.clone();
        out.push(b':');
        out.extend_from_slice(&self.db_key_prefix);
        String::from_utf8_lossy(&out).into_owned()
    }

    /// Returns the string representation, byte-equal to Go `Query.Print`.
    pub fn print(&self) -> String {
        let mut out = Vec::new();
        out.extend_from_slice(b"query ");
        out.extend_from_slice(&self.db_name);
        out.push(b':');
        out.extend_from_slice(&self.db_key_prefix);

        if let Some(condition) = &self.where_ {
            let mut where_ = Vec::new();
            condition.fmt_bytes(&mut where_);
            if !where_.is_empty() {
                // Go strips the outer parentheses of a grouped condition.
                if where_.len() >= 2 && where_[0] == b'(' {
                    where_ = where_[1..where_.len() - 1].to_vec();
                }
                out.extend_from_slice(b" where ");
                out.extend_from_slice(&where_);
            }
        }

        if !self.order_by.is_empty() {
            out.extend_from_slice(b" orderby ");
            out.extend_from_slice(&self.order_by);
        }
        if self.limit > 0 {
            out.extend_from_slice(format!(" limit {}", self.limit).as_bytes());
        }
        if self.offset > 0 {
            out.extend_from_slice(format!(" offset {}", self.offset).as_bytes());
        }

        String::from_utf8_lossy(&out).into_owned()
    }
}

impl std::fmt::Display for Query {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.print())
    }
}
