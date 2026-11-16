//! Install info, ported from Go `service/broadcasts/install_info.go`.

use pm_dsd::gotime;
use pm_query::Query;
use serde::{Deserialize, Deserializer, Serialize, Serializer};

use pm_database::Interface;
use pm_record::{Key, Meta, Record};

use crate::{Error, Result};

/// Database key of the install info record.
pub const INSTALL_INFO_DB_KEY: &str = "core:status/install-info";

/// Generic information about the install.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct InstallInfo {
    pub version: String,
    pub numeric_version: i64,
    /// Unix seconds of the install date; 0 is Go's zero time.
    pub time: i64,
    pub numeric_date: i64,
    pub days_since_install: i64,
    pub unix_timestamp: i64,
}

impl Serialize for InstallInfo {
    fn serialize<S: Serializer>(&self, serializer: S) -> std::result::Result<S::Ok, S::Error> {
        use serde::ser::SerializeStruct;
        let mut state = serializer.serialize_struct("InstallInfo", 6)?;
        state.serialize_field("Version", &self.version)?;
        state.serialize_field("NumericVersion", &self.numeric_version)?;
        state.serialize_field("Time", &gotime::format_rfc3339(self.time))?;
        state.serialize_field("NumericDate", &self.numeric_date)?;
        state.serialize_field("DaysSinceInstall", &self.days_since_install)?;
        state.serialize_field("UnixTimestamp", &self.unix_timestamp)?;
        state.end()
    }
}

impl<'de> Deserialize<'de> for InstallInfo {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> std::result::Result<Self, D::Error> {
        #[derive(Deserialize)]
        struct Fields {
            #[serde(default)]
            version: String,
            #[serde(default)]
            numeric_version: i64,
            #[serde(default)]
            time: String,
            #[serde(default)]
            numeric_date: i64,
            #[serde(default)]
            days_since_install: i64,
            #[serde(default)]
            unix_timestamp: i64,
        }
        let fields = Fields::deserialize(deserializer)?;
        Ok(InstallInfo {
            version: fields.version,
            numeric_version: fields.numeric_version,
            time: gotime::parse_rfc3339(&fields.time).unwrap_or(0),
            numeric_date: fields.numeric_date,
            days_since_install: fields.days_since_install,
            unix_timestamp: fields.unix_timestamp,
        })
    }
}

impl InstallInfo {
    /// Reads the install info from the database.
    pub fn get(db: &Interface) -> Result<InstallInfo> {
        let record = db.get(INSTALL_INFO_DB_KEY)?;
        Ok(record.decode::<InstallInfo>()?)
    }

    /// Persists the install info, like Go `save`.
    pub fn save(&self, db: &Interface) -> Result<()> {
        let record = Record::from_typed(Key::parse(INSTALL_INFO_DB_KEY), Meta::default(), self)?;
        db.put(record)?;
        Ok(())
    }

    /// Fills in missing data, like Go `checkAll`.
    pub fn check_all(&mut self, db: &Interface, build_version: &str, now: i64) {
        self.check_version(build_version);
        self.check_install_date(db, now);
    }

    fn check_version(&mut self, build_version: &str) {
        if !self.version.is_empty() && self.numeric_version > 0 {
            return;
        }
        self.version = build_version.to_string();
        if !build_version.is_empty() {
            match make_numeric_version(build_version) {
                Ok(numeric) => self.numeric_version = numeric,
                Err(error) => tracing::warn!(%error, "updates: failed to make numeric version"),
            }
        }
    }

    fn check_install_date(&mut self, db: &Interface, now: i64) {
        if self.unix_timestamp > 0
            && self.numeric_date > 0
            && self.days_since_install > 0
            && self.time > 0
        {
            return;
        }

        // Find the oldest created database entry, like Go.
        let mut oldest = now;
        let query = Query::new("core");
        match db.query(&query) {
            Ok(records) => {
                for record in records {
                    if oldest > record.meta.created {
                        oldest = record.meta.created;
                    }
                }
            }
            Err(error) => {
                tracing::warn!(%error, "updates: failed to query DB for install time");
                return;
            }
        }

        self.unix_timestamp = oldest;
        self.time = oldest;
        self.days_since_install = (now - oldest).max(0) / 86_400;
        if let Ok(numeric) = format_numeric_date(oldest) {
            self.numeric_date = numeric;
        }
    }
}

/// Formats a Unix timestamp as `YYYYMMDD`, like Go `time.Format("20060102")`.
pub fn format_numeric_date(unix_seconds: i64) -> Result<i64> {
    let text = gotime::format_rfc3339(unix_seconds);
    let date: String = text.chars().take(10).filter(|c| *c != '-').collect();
    date.parse()
        .map_err(|_| Error::Message(format!("failed to format numeric date from {text}")))
}

/// Makes a numeric version with the first three segments always using three
/// digits, like Go `MakeNumericVersion`.
pub fn make_numeric_version(version: &str) -> Result<i64> {
    // Remove comments and a leading "v", like ghodss/go-version tolerates.
    let version = version.split_whitespace().next().unwrap_or("");
    let version = version.strip_prefix('v').unwrap_or(version);
    if version.is_empty() {
        return Err(Error::Message("failed to parse core version: empty".to_string()));
    }

    let mut numeric: i64 = 0;
    for (index, segment) in version.split('.').enumerate().take(3) {
        let digits: String = segment.chars().take_while(|c| c.is_ascii_digit()).collect();
        if digits.is_empty() {
            return Err(Error::Message(format!(
                "failed to parse core version: Malformed version: {version}"
            )));
        }
        let mut number: i64 = digits.parse().map_err(|_| {
            Error::Message(format!("failed to parse core version: Malformed version: {version}"))
        })?;
        if number > 999 {
            number = 999;
        }
        numeric += number * match index {
            0 => 1_000_000,
            1 => 1_000,
            _ => 1,
        };
    }
    Ok(numeric)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn numeric_versions_match_go() {
        assert_eq!(make_numeric_version("0.9.0").unwrap(), 9_000);
        assert_eq!(make_numeric_version("v1.2.3 dev build").unwrap(), 1_002_003);
        assert_eq!(make_numeric_version("1.2.3.4").unwrap(), 1_002_003);
        assert_eq!(make_numeric_version("1000.0.0").unwrap(), 999_000_000);
        assert!(make_numeric_version("not-a-version").is_err());
    }

    #[test]
    fn numeric_date() {
        assert_eq!(format_numeric_date(1_700_000_000).unwrap(), 20_231_114);
    }
}
