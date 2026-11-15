//! Port of Go `namerecord.go`: persisted DNS cache records.

use hickory_proto::rr::RecordType;
use pm_database::{Interface, Options as DbOptions};
use pm_query::Query as DbQuery;
use pm_record::{Key, Meta, Record};
use serde::{Deserialize, Deserializer, Serialize};

use crate::model::ResolverInfo;
use crate::rrcache::{qtype_string, RRCache};

/// How much longer than the TTL name records are kept in the database.
pub const DATABASE_OVERTIME: i64 = 86400 * 14;

/// Key prefix of name records in the cache database.
pub const NAME_RECORDS_KEY_PREFIX: &str = "cache:intel/nameRecord/";

/// Errors of the name record store.
#[derive(Debug, thiserror::Error)]
pub enum NameRecordError {
    #[error("database entry not found")]
    NotFound,
    #[error("could not save NameRecord, missing Domain and/or Question")]
    MissingKey,
    #[error("record is invalid (outdated format)")]
    Invalid,
    #[error("{0}")]
    Other(String),
}

impl From<pm_database::Error> for NameRecordError {
    fn from(err: pm_database::Error) -> Self {
        match err {
            pm_database::Error::NotFound => NameRecordError::NotFound,
            other => NameRecordError::Other(other.to_string()),
        }
    }
}

fn null_as_empty<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<String>, D::Error> {
    Ok(Option::<Vec<String>>::deserialize(d)?.unwrap_or_default())
}

/// Helper struct to `RRCache` to better save data to the database. Fields and
/// their order are the persisted JSON of Go `NameRecord`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct NameRecord {
    #[serde(rename = "Domain", default)]
    pub domain: String,
    #[serde(rename = "Question", default)]
    pub question: String,
    #[serde(rename = "RCode", default)]
    pub rcode: i64,
    #[serde(rename = "Answer", default, deserialize_with = "null_as_empty")]
    pub answer: Vec<String>,
    #[serde(rename = "Ns", default, deserialize_with = "null_as_empty")]
    pub ns: Vec<String>,
    #[serde(rename = "Extra", default, deserialize_with = "null_as_empty")]
    pub extra: Vec<String>,
    #[serde(rename = "Expires", default)]
    pub expires: i64,
    #[serde(rename = "Resolver", default)]
    pub resolver: Option<ResolverInfo>,
}

impl NameRecord {
    /// Returns whether the record is valid and may be used.
    pub fn is_valid(&self) -> bool {
        self.resolver.as_ref().is_some_and(|r| !r.r#type.is_empty())
    }

    /// Returns the database key of the record.
    pub fn key(&self) -> String {
        make_name_record_key(&self.domain, &self.question)
    }

    /// Serializes the record like Go `json.Marshal`.
    pub fn to_json(&self) -> Result<Vec<u8>, NameRecordError> {
        pm_dsd::gojson::to_vec(self).map_err(|e| NameRecordError::Other(e.to_string()))
    }
}

/// Returns the database key of a name record.
pub fn make_name_record_key(domain: &str, question: &str) -> String {
    format!("{NAME_RECORDS_KEY_PREFIX}{domain}{question}")
}

/// Access to the name records in the cache database.
pub struct NameRecordStore {
    db: Interface,
}

impl NameRecordStore {
    /// Creates a store on top of a database interface. Use [`record_interface_options`].
    pub fn new(db: Interface) -> NameRecordStore {
        NameRecordStore { db }
    }

    /// Returns a raw database record, like the Go `dns/cache` endpoint does.
    pub fn get_raw(&self, query: &str) -> Result<Record, NameRecordError> {
        Ok(self.db.get(&format!("{NAME_RECORDS_KEY_PREFIX}{query}"))?)
    }

    /// Gets a name record, together with its metadata.
    pub fn get(&self, domain: &str, question: &str) -> Result<(NameRecord, Meta), NameRecordError> {
        let record = self.db.get(&make_name_record_key(domain, question))?;
        let name_record: NameRecord = record
            .decode()
            .map_err(|e| NameRecordError::Other(e.to_string()))?;
        if !name_record.is_valid() {
            return Err(NameRecordError::Invalid);
        }
        Ok((name_record, record.meta))
    }

    /// Loads and converts a cache entry, like Go `GetRRCache`.
    pub fn get_rrcache(
        &self,
        domain: &str,
        question: RecordType,
    ) -> Result<RRCache, NameRecordError> {
        let (name_record, meta) = self.get(domain, &qtype_string(question))?;
        RRCache::from_name_record(&name_record, question, meta.modified)
            .map_err(NameRecordError::Other)
    }

    /// Saves the name record. The database expiry is the record expiry plus two
    /// weeks (see the notes on the expiry in the crate docs).
    pub fn save(&self, name_record: &NameRecord) -> Result<(), NameRecordError> {
        if name_record.domain.is_empty() || name_record.question.is_empty() {
            return Err(NameRecordError::MissingKey);
        }
        let mut meta = Meta::default();
        meta.set_absolute_expiry(name_record.expires + DATABASE_OVERTIME);
        let record = Record {
            key: Key::parse(&name_record.key()),
            meta,
            format: pm_dsd::format::JSON,
            data: name_record.to_json()?,
        };
        Ok(self.db.put(record)?)
    }

    /// Saves a cache entry if it is cacheable.
    pub fn save_rrcache(&self, rrcache: &RRCache) -> Result<(), NameRecordError> {
        if !rrcache.cacheable() {
            return Ok(());
        }
        let name_record = rrcache.to_name_record().map_err(NameRecordError::Other)?;
        self.save(&name_record)
    }

    /// Deletes a name record from the cache database.
    pub fn reset(&self, domain: &str, question: &str) -> Result<(), NameRecordError> {
        Ok(self.db.delete(&make_name_record_key(domain, question))?)
    }

    /// Deletes all name records, returning how many were removed.
    pub fn clear(&self) -> Result<usize, NameRecordError> {
        let query = DbQuery::new(NAME_RECORDS_KEY_PREFIX)
            .check()
            .map_err(|e| NameRecordError::Other(e.to_string()))?;
        let records = self.db.query(&query)?;
        let mut count = 0;
        for record in records {
            match self.db.delete(&record.key.to_string()) {
                Ok(()) => count += 1,
                Err(pm_database::Error::NotFound) => {}
                Err(err) => return Err(err.into()),
            }
        }
        Ok(count)
    }
}

/// Database interface options of the name record store: local and internal.
pub fn record_interface_options() -> DbOptions {
    DbOptions {
        local: true,
        internal: true,
        ..DbOptions::default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn invalid_without_resolver() {
        let mut nr = NameRecord {
            domain: "a.".into(),
            question: "A".into(),
            rcode: 0,
            answer: vec![],
            ns: vec![],
            extra: vec![],
            expires: 0,
            resolver: None,
        };
        assert!(!nr.is_valid());
        let json = r#"{"Domain":"a.","Question":"A","RCode":0,"Answer":null,"Expires":1,"Resolver":{"Name":"","Type":"dns","Source":"system","IP":"1.1.1.1","Domain":"","IPScope":4,"Port":53}}"#;
        nr = serde_json::from_str(json).unwrap();
        assert!(nr.is_valid());
        assert!(nr.answer.is_empty());
    }
}
