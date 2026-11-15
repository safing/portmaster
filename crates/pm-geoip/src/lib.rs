//! Compatible implementation of Go `service/intel/geoip` (without the updater).
//!
//! The crate reads MaxMind databases (GeoIP2/GeoLite2), enriches locations with
//! the country and region tables and estimates network proximity between
//! locations.
//!
//! # Compatibility notes
//!
//! * The country and region tables are stored as JSON in
//!   `data/geoip-data.json`. The file is generated from the Go sources by
//!   `tools/rust-fixtures` (go/ast) and verified against the running Go package
//!   (`GetCountryInfo`, `IsRegionalNeighbor`) before it is written.
//! * MMDB access uses the `maxminddb` crate. Go waits up to one second for the
//!   update worker to fetch a database; without the updater, [`GeoIp::get_location`]
//!   simply reports that no database is available.
//! * The update worker, artifact handling (`service/updates`) and the
//!   `intel/geoip/countries` API endpoint are not ported. [`GeoIp::replace_database`]
//!   is the entry point a future worker will call.
//! * `AutonomousSystemNumber` is an `u32` (as stored in MMDB) instead of Go's
//!   `uint`.

mod database;

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::OnceLock;

use serde::{Deserialize, Serialize};

pub use database::{Error, GeoIp};

/// Earth circumference in kilometers.
pub const EARTH_CIRCUMFERENCE_IN_KM: f64 = 40100.0;
/// Default accuracy radius of locations without one.
pub const DEFAULT_LOCATION_ACCURACY: u16 = 100;
/// Default accuracy radius of country centers.
pub const DEFAULT_COUNTRY_BASED_ACCURACY: u16 = 200;

const WEIGHT_COUNTRY_MATCH: f32 = 10.0;
const WEIGHT_REGION_MATCH: f32 = 10.0;
const WEIGHT_REGIONAL_NEIGHBOR_MATCH: f32 = 10.0;
const WEIGHT_ASN_MATCH: f32 = 10.0;
const WEIGHT_AS_ORG_MATCH: f32 = 10.0;
const WEIGHT_COORDINATE_DISTANCE: f32 = 50.0;

/// Geographic coordinates and their estimated accuracy.
#[derive(Debug, Clone, Copy, Default, PartialEq, Serialize, Deserialize)]
pub struct Coordinates {
    #[serde(rename = "AccuracyRadius")]
    pub accuracy_radius: u16,
    #[serde(rename = "Latitude")]
    pub latitude: f64,
    #[serde(rename = "Longitude")]
    pub longitude: f64,
}

/// Additional information about a continent.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ContinentInfo {
    #[serde(rename = "Code")]
    pub code: String,
    #[serde(rename = "Region")]
    pub region: String,
    #[serde(rename = "Name")]
    pub name: String,
}

/// Additional information about a country.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct CountryInfo {
    #[serde(rename = "Code")]
    pub code: String,
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Center")]
    pub center: Coordinates,
    #[serde(rename = "Continent")]
    pub continent: ContinentInfo,
}

/// A geographic region and its neighboring regions.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Region {
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Neighbors")]
    pub neighbors: Vec<String>,
}

/// Geographical and network location of an IP address.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Location {
    #[serde(rename = "Country")]
    pub country: CountryInfo,
    #[serde(rename = "Coordinates")]
    pub coordinates: Coordinates,
    #[serde(rename = "AutonomousSystemNumber")]
    pub asn: u32,
    #[serde(rename = "AutonomousSystemOrganization")]
    pub as_org: String,
    #[serde(rename = "IsAnycast")]
    pub is_anycast: bool,
    #[serde(rename = "IsSatelliteProvider")]
    pub is_satellite_provider: bool,
    #[serde(rename = "IsAnonymousProxy")]
    pub is_anonymous_proxy: bool,
}

impl Location {
    /// Returns the country information of the location.
    pub fn country_info(&self) -> Option<&'static CountryInfo> {
        country_info(&self.country.code)
    }

    /// Returns whether the location is a regional neighbor of `other`.
    pub fn is_regional_neighbor(&self, other: &Location) -> bool {
        if self.country.continent.region.is_empty() || other.country.continent.region.is_empty() {
            return false;
        }
        match regions().get(&self.country.continent.region) {
            Some(region) => region
                .neighbors
                .iter()
                .any(|neighbor| neighbor == &other.country.continent.region),
            None => false,
        }
    }

    /// Estimates the distance between two network locations.
    ///
    /// Returns a proximity value between 0 (far away) and 100 (nearby).
    pub fn estimate_network_proximity(&self, to: &Location) -> f32 {
        let mut proximity: f32 = 0.0;

        if !self.country.code.is_empty() && self.country.code == to.country.code {
            proximity +=
                WEIGHT_COUNTRY_MATCH + WEIGHT_REGION_MATCH + WEIGHT_REGIONAL_NEIGHBOR_MATCH;
        } else if !self.country.continent.region.is_empty()
            && self.country.continent.region == to.country.continent.region
        {
            proximity += WEIGHT_REGION_MATCH + WEIGHT_REGIONAL_NEIGHBOR_MATCH;
        } else if self.is_regional_neighbor(to) {
            proximity += WEIGHT_REGIONAL_NEIGHBOR_MATCH;
        }

        if self.asn == to.asn && self.asn != 0 {
            // Rely more on the ASN data, as it is more accurate than the ASOrg
            // data, especially when combining location data from multiple
            // sources.
            proximity += WEIGHT_ASN_MATCH + WEIGHT_AS_ORG_MATCH;
        } else if self.as_org == to.as_org && self.asn != 0 && !as_org_unknown(&self.as_org) {
            proximity += WEIGHT_AS_ORG_MATCH;
        }

        // Check coordinates and adjust the accuracy value.
        let mut accuracy = self.coordinates.accuracy_radius;
        if (self.coordinates.latitude == 0.0 && self.coordinates.longitude == 0.0)
            || (to.coordinates.latitude == 0.0 && to.coordinates.longitude == 0.0)
        {
            // Without coordinates there is nothing more to compare.
            return proximity;
        }
        if to.coordinates.accuracy_radius > accuracy {
            accuracy = to.coordinates.accuracy_radius;
        }
        if accuracy == 0 {
            accuracy = DEFAULT_LOCATION_ACCURACY;
        }

        let km = haversine_km(
            self.coordinates.latitude,
            self.coordinates.longitude,
            to.coordinates.latitude,
            to.coordinates.longitude,
        );

        if km <= 100.0 && accuracy <= 100 {
            // Full value for highly accurate coordinates within 100km.
            proximity += WEIGHT_COORDINATE_DISTANCE;
        } else {
            // Take a percentage, with a penalty for low accuracy.
            let proximity_in_percent = (EARTH_CIRCUMFERENCE_IN_KM - km) / EARTH_CIRCUMFERENCE_IN_KM;
            let accuracy_modifier = 1.0 - f64::from(accuracy) / 2000.0;
            proximity += (WEIGHT_COORDINATE_DISTANCE as f64
                * proximity_in_percent
                * accuracy_modifier) as f32;
        }

        proximity
    }

    /// Adds missing country information to the location, like Go
    /// `Location.AddCountryInfo`.
    pub fn add_country_info(&mut self) {
        if self.country.code.is_empty() {
            return;
        }

        // Reset data for anycast.
        if self.is_anycast {
            self.country.code = "__".to_string();
            self.coordinates.latitude = 0.0;
            self.coordinates.longitude = 0.0;
        }

        let Some(info) = country_info(&self.country.code) else {
            return;
        };
        self.country = info.clone();

        // Use the country center as the location if coordinates are unset.
        if self.coordinates.latitude == 0.0 && self.coordinates.longitude == 0.0 {
            self.coordinates = info.center;
        }
    }
}

/// Returns the country information of the given country code.
pub fn get_country_info(country_code: &str) -> CountryInfo {
    country_info(country_code).cloned().unwrap_or_default()
}

/// Returns the country information of the given code, if known.
pub fn country_info(country_code: &str) -> Option<&'static CountryInfo> {
    countries().get(country_code)
}

/// Returns all known country information, indexed by ISO-A2 code.
pub fn countries() -> &'static HashMap<String, CountryInfo> {
    &data().countries
}

/// Returns all known regions, indexed by region ID.
pub fn regions() -> &'static HashMap<String, Region> {
    &data().regions
}

#[derive(Deserialize)]
struct GeoData {
    countries: HashMap<String, CountryInfo>,
    regions: HashMap<String, Region>,
}

fn data() -> &'static GeoData {
    static DATA: OnceLock<GeoData> = OnceLock::new();
    DATA.get_or_init(|| {
        serde_json::from_str(include_str!("../data/geoip-data.json"))
            .expect("embedded geoip data is valid")
    })
}

/// Returns whether the AS organization name means that it is unknown.
pub fn as_org_unknown(as_org: &str) -> bool {
    const UNKNOWN: [&str; 7] = [
        "",
        "not routed",
        "unknown",
        "nil",
        "null",
        "undef",
        "undefined",
    ];
    let as_org = as_org.to_lowercase();
    UNKNOWN.iter().any(|unknown| *unknown == as_org)
}

/// Calculates the numerical distance between two IP addresses.
///
/// Returns a proximity value between 0 (far away) and 100 (nearby). `version`
/// must be `4` or `6`.
pub fn primitive_network_proximity(from: IpAddr, to: IpAddr, version: u8) -> i32 {
    let from = ip_to_16(from);
    let to = ip_to_16(to);

    match version {
        4 => {
            let a = u32::from_be_bytes(from[12..16].try_into().expect("slice length"));
            let b = u32::from_be_bytes(to[12..16].try_into().expect("slice length"));
            let mut diff = f64::from(a.abs_diff(b));
            diff /= 256.0;
            ((1.0 - diff / 16_777_216.0) * 100.0) as i32
        }
        6 => {
            let a = u64::from_be_bytes(from[..8].try_into().expect("slice length"));
            let b = u64::from_be_bytes(to[..8].try_into().expect("slice length"));
            let diff = a.abs_diff(b) as f64;
            ((1.0 - diff / 18_446_744_073_709_552_000.0) * 100.0) as i32
        }
        _ => 0,
    }
}

/// Returns the 16-byte form of an IP address, like Go's `net.IP`.
fn ip_to_16(ip: IpAddr) -> [u8; 16] {
    match ip {
        IpAddr::V4(ipv4) => ipv4.to_ipv6_mapped().octets(),
        IpAddr::V6(ipv6) => ipv6.octets(),
    }
}

/// Returns the great-circle distance between two coordinates in kilometers.
fn haversine_km(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let lat1 = lat1.to_radians();
    let lon1 = lon1.to_radians();
    let lat2 = lat2.to_radians();
    let lon2 = lon2.to_radians();

    let diff_lat = lat2 - lat1;
    let diff_lon = lon2 - lon1;

    let a =
        (diff_lat / 2.0).sin().powi(2) + lat1.cos() * lat2.cos() * (diff_lon / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().atan2((1.0 - a).sqrt());
    c * 6371.0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn country_data_is_loaded() {
        assert!(countries().len() > 200);
        assert!(regions().len() > 20);

        let germany = get_country_info("DE");
        assert_eq!(germany.code, "DE");
        assert_eq!(germany.name, "Germany");
        assert_eq!(germany.continent.code, "EU");
        assert_eq!(germany.continent.region, "EU-W");
        assert_eq!(germany.continent.name, "Europe");
        assert_eq!(
            germany.center.accuracy_radius,
            DEFAULT_COUNTRY_BASED_ACCURACY
        );

        let anycast = get_country_info("__");
        assert_eq!(anycast.name, "Anycast");

        // Unknown codes return the zero value, like Go.
        assert_eq!(get_country_info("ZZ"), CountryInfo::default());
    }

    #[test]
    fn anycast_is_remapped() {
        let mut location = Location {
            country: CountryInfo {
                code: "US".to_string(),
                ..CountryInfo::default()
            },
            coordinates: Coordinates {
                latitude: 40.0,
                longitude: -70.0,
                accuracy_radius: 10,
            },
            is_anycast: true,
            ..Location::default()
        };
        location.add_country_info();
        assert_eq!(location.country.code, "__");
        assert_eq!(location.country.name, "Anycast");
        assert_eq!(location.coordinates.latitude, 0.0);
        assert_eq!(location.coordinates.longitude, 0.0);
        // The anycast center is used, as the coordinates were reset.
        assert_eq!(
            location.coordinates.accuracy_radius,
            DEFAULT_COUNTRY_BASED_ACCURACY
        );

        // Locations without a country are left alone.
        let mut empty = Location::default();
        empty.add_country_info();
        assert_eq!(empty, Location::default());
    }

    #[test]
    fn unknown_as_orgs_are_detected() {
        assert!(as_org_unknown(""));
        assert!(as_org_unknown("Not Routed"));
        assert!(as_org_unknown("UNKNOWN"));
        assert!(!as_org_unknown("Cloudflare, Inc."));
    }

    #[test]
    fn primitive_proximity_is_bounded() {
        let one: IpAddr = "1.1.1.1".parse().unwrap();
        let two: IpAddr = "1.1.1.2".parse().unwrap();
        assert_eq!(primitive_network_proximity(one, one, 4), 100);
        assert_eq!(primitive_network_proximity(one, two, 4), 99);
        assert_eq!(primitive_network_proximity(one, two, 0), 0);
    }
}
