//! MaxMind database access, a port of Go `service/intel/geoip/{database,lookup}.go`.
//!
//! The Go implementation keeps a worker that reloads databases from
//! `service/updates`. This crate only provides the database handle: callers
//! load (or replace) databases with [`GeoIp::replace_database`].

use std::net::IpAddr;
use std::sync::{Arc, RwLock};

use maxminddb::Reader;
use serde::Deserialize;

use crate::{Coordinates, CountryInfo, Location};

/// A reader over one of the MaxMind databases.
///
/// The Go implementation memory-maps the file; `open_readfile` loads it into
/// memory instead, which avoids `unsafe` in this crate.
pub type MmdbReader = Reader<Vec<u8>>;

/// Errors of the GeoIP database.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("geoip database not available")]
    DatabaseNotAvailable,
    #[error("maxminddb: {0}")]
    Maxmind(#[from] maxminddb::MaxMindDbError),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
}

/// Result type of the GeoIP database.
pub type Result<T> = std::result::Result<T, Error>;

/// The raw record of a MaxMind database, as used by the Go package struct tags.
#[derive(Debug, Default, Deserialize)]
struct MmdbRecord {
    #[serde(default)]
    country: MmdbCountry,
    #[serde(default)]
    location: MmdbLocation,
    #[serde(default)]
    autonomous_system_number: u32,
    #[serde(default)]
    autonomous_system_organization: String,
    #[serde(default)]
    is_anycast: bool,
    #[serde(default)]
    is_satellite_provider: bool,
    #[serde(default)]
    is_anonymous_proxy: bool,
}

#[derive(Debug, Default, Deserialize)]
struct MmdbCountry {
    #[serde(default, rename = "iso_code")]
    iso_code: String,
}

#[derive(Debug, Default, Deserialize)]
struct MmdbLocation {
    #[serde(default)]
    accuracy_radius: u16,
    #[serde(default, deserialize_with = "deserialize_float")]
    latitude: f64,
    #[serde(default, deserialize_with = "deserialize_float")]
    longitude: f64,
}

/// Deserializes a coordinate stored as either a 32-bit or a 64-bit float.
///
/// MaxMind databases store coordinates as `float` (32-bit), while Go's decoder
/// converts them into the `float64` struct fields transparently.
fn deserialize_float<'de, D: serde::Deserializer<'de>>(
    deserializer: D,
) -> std::result::Result<f64, D::Error> {
    struct FloatVisitor;

    impl<'de> serde::de::Visitor<'de> for FloatVisitor {
        type Value = f64;

        fn expecting(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            formatter.write_str("a floating point number")
        }

        fn visit_f32<E: serde::de::Error>(self, value: f32) -> std::result::Result<f64, E> {
            Ok(f64::from(value))
        }

        fn visit_f64<E: serde::de::Error>(self, value: f64) -> std::result::Result<f64, E> {
            Ok(value)
        }

        fn visit_i64<E: serde::de::Error>(self, value: i64) -> std::result::Result<f64, E> {
            Ok(value as f64)
        }

        fn visit_u64<E: serde::de::Error>(self, value: u64) -> std::result::Result<f64, E> {
            Ok(value as f64)
        }
    }

    deserializer.deserialize_any(FloatVisitor)
}

/// Holds the IPv4 and IPv6 GeoIP databases.
#[derive(Default)]
pub struct GeoIp {
    v4: RwLock<Option<Arc<MmdbReader>>>,
    v6: RwLock<Option<Arc<MmdbReader>>>,
}

impl GeoIp {
    /// Creates an empty GeoIP handle.
    pub fn new() -> GeoIp {
        GeoIp::default()
    }

    /// Replaces (or initially sets) the database for the given IP version,
    /// like Go `ReplaceDatabase`.
    ///
    /// The previous reader is dropped once the last lookup using it finished.
    pub fn replace_database(&self, path: impl AsRef<std::path::Path>, v6: bool) -> Result<()> {
        let reader = Reader::open_readfile(path)?;
        self.replace_reader(Arc::new(reader), v6);
        Ok(())
    }

    /// Replaces the database from an already opened reader.
    pub fn replace_reader(&self, reader: Arc<MmdbReader>, v6: bool) {
        let slot = if v6 { &self.v6 } else { &self.v4 };
        *slot.write().expect("lock poisoned") = Some(reader);
    }

    /// Returns whether a database is available for the given IP version,
    /// like Go `IsInitialized`.
    pub fn is_initialized(&self, v6: bool) -> bool {
        self.reader(v6).is_some()
    }

    /// Returns the reader for the given IP version, if a database is loaded.
    pub fn reader(&self, v6: bool) -> Option<Arc<MmdbReader>> {
        let slot = if v6 { &self.v6 } else { &self.v4 };
        slot.read().expect("lock poisoned").clone()
    }

    /// Returns the location of an IP address, like Go `GetLocation`.
    ///
    /// The result is enriched with the country information.
    pub fn get_location(&self, ip: IpAddr) -> Result<Location> {
        let v6 = ip.is_ipv6();
        let reader = self.reader(v6).ok_or(Error::DatabaseNotAvailable)?;
        // A missing record leaves the location empty, like Go.
        let record: MmdbRecord = reader.lookup(ip)?.decode()?.unwrap_or_default();

        let mut location = Location {
            country: CountryInfo {
                code: record.country.iso_code,
                ..CountryInfo::default()
            },
            coordinates: Coordinates {
                accuracy_radius: record.location.accuracy_radius,
                latitude: record.location.latitude,
                longitude: record.location.longitude,
            },
            asn: record.autonomous_system_number,
            as_org: record.autonomous_system_organization,
            is_anycast: record.is_anycast,
            is_satellite_provider: record.is_satellite_provider,
            is_anonymous_proxy: record.is_anonymous_proxy,
        };
        location.add_country_info();
        Ok(location)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_database_is_reported() {
        let geoip = GeoIp::new();
        assert!(!geoip.is_initialized(false));
        assert!(!geoip.is_initialized(true));
        match geoip.get_location("192.0.2.1".parse().unwrap()) {
            Err(Error::DatabaseNotAvailable) => {}
            other => panic!("unexpected result: {other:?}"),
        }
    }
}
