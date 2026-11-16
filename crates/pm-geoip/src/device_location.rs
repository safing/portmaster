//! Device location model and estimation, ported from Go
//! `service/netenv/location.go`.
//!
//! Go keeps a global, machine-derived list of possible device locations. This
//! module ports the model (source accuracies, ordering, selection, IP scope
//! filtering and the timezone fallback) into `pm-geoip`: the Rust port has no
//! device-location home of its own yet. Everything that touches the OS
//! (interfaces, traceroute, time zone, network-change flag, GeoIP lookups) is
//! injected through [`LocationEnvironment`], so the logic is testable without
//! a real machine.
//!
//! # Deviations from Go
//!
//! * Go stores `*DeviceLocation` pointers in a slice; here the entries are
//!   owned values. [`DeviceLocations::copy`] clones them, while Go's `Copy`
//!   shares the entries (Go also reads the global list instead of the
//!   receiver; equivalent at its call sites).
//! * Go's nil receiver checks (`LocationOrNil`, `String`) have no Rust
//!   equivalent; the methods simply operate on `&self`.
//! * Go's `net.IP.Equal` treats IPv4 and IPv4-mapped IPv6 addresses as equal;
//!   Rust's `IpAddr` keeps them distinct, so [`DeviceLocations::add_location`]
//!   compares `IpAddr` values directly.
//! * Go's `sort.Sort` comparator is not a strict order for equally accurate
//!   entries; their relative order after sorting may therefore differ.
//! * The traceroute itself (ICMP sockets, packet interception) is not ported;
//!   [`LocationEnvironment::traceroute`] supplies the discovered IP instead.
//! * Failed lookups and traceroutes are logged in Go; here they are reported as
//!   `None` and the caller can log them.

use std::fmt;
use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Mutex, MutexGuard, PoisonError};

use pm_netstate::{ip_scope, IpScope};
use serde::de::Deserializer;
use serde::ser::Serializer;
use serde::{Deserialize, Serialize};

use crate::{Coordinates, GeoIp, Location};

/// IP version of a device location, matching Go `packet.IPVersion`.
///
/// Go's zero value is `0` (invalid); the Rust enum only has the two valid
/// versions.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum IpVersion {
    /// Go `packet.IPv4`.
    V4 = 4,
    /// Go `packet.IPv6`.
    V6 = 6,
}

impl IpVersion {
    /// Returns the version of the given address.
    pub fn from_ip(ip: IpAddr) -> Self {
        match ip {
            IpAddr::V4(_) => Self::V4,
            IpAddr::V6(_) => Self::V6,
        }
    }

    /// Returns `4` or `6`, like the Go constant value.
    pub fn as_u8(self) -> u8 {
        self as u8
    }
}

impl fmt::Display for IpVersion {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Self::V4 => "IPv4",
            Self::V6 => "IPv6",
        })
    }
}

impl Serialize for IpVersion {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_u8(self.as_u8())
    }
}

impl<'de> Deserialize<'de> for IpVersion {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        match u8::deserialize(deserializer)? {
            4 => Ok(Self::V4),
            6 => Ok(Self::V6),
            other => Err(serde::de::Error::custom(format!(
                "unknown IP version: {other}"
            ))),
        }
    }
}

/// Source of a device location, matching Go `netenv.DeviceLocationSource`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum DeviceLocationSource {
    /// Go `SourceInterface`.
    Interface,
    /// Go `SourcePeer`.
    Peer,
    /// Go `SourceUPNP`.
    Upnp,
    /// Go `SourceTraceroute`.
    Traceroute,
    /// Go `SourceTimezone`.
    Timezone,
    /// Go `SourceOther`.
    Other,
}

impl DeviceLocationSource {
    /// Returns the source accuracy, like Go `DeviceLocationSource.Accuracy`.
    pub fn accuracy(self) -> i32 {
        match self {
            Self::Interface => 6,
            Self::Peer => 5,
            Self::Upnp => 4,
            Self::Traceroute => 3,
            Self::Other => 2,
            Self::Timezone => 1,
        }
    }

    /// Returns the Go string value of the source.
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Interface => "interface",
            Self::Peer => "peer",
            Self::Upnp => "upnp",
            Self::Traceroute => "traceroute",
            Self::Timezone => "timezone",
            Self::Other => "other",
        }
    }
}

impl fmt::Display for DeviceLocationSource {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A single IP and its metadata, like Go `netenv.DeviceLocation`.
///
/// Go documents that entries must not be changed once created; treat the
/// public fields as read-only after construction.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DeviceLocation {
    #[serde(
        rename = "IP",
        serialize_with = "go_json::serialize_ip",
        deserialize_with = "go_json::deserialize_ip"
    )]
    pub ip: Option<IpAddr>,
    #[serde(rename = "IPVersion")]
    pub ip_version: IpVersion,
    #[serde(rename = "Location")]
    pub location: Option<Location>,
    #[serde(rename = "Source")]
    pub source: DeviceLocationSource,
    #[serde(rename = "SourceAccuracy")]
    pub source_accuracy: i32,
}

impl DeviceLocation {
    /// Returns whether this location is more accurate than `other`, using the
    /// same ordered rules as Go `DeviceLocation.IsMoreAccurateThan`.
    pub fn is_more_accurate_than(&self, other: &Self) -> bool {
        if self.source_accuracy > other.source_accuracy {
            // Higher source accuracy is better.
            return true;
        }
        if self.ip.is_some() && other.ip.is_none() {
            // Location based on IP is better than without.
            return true;
        }

        let (asn, code, latitude, longitude, accuracy) = match &self.location {
            Some(location) => (
                location.asn,
                location.country.code.as_str(),
                location.coordinates.latitude,
                location.coordinates.longitude,
                location.coordinates.accuracy_radius,
            ),
            None => (0, "", 0.0, 0.0, 0),
        };
        let other_location = other.location.as_ref();
        let other_asn = other_location.map_or(0, |location| location.asn);
        let other_code = other_location.map_or("", |location| location.country.code.as_str());
        let other_latitude = other_location.map_or(0.0, |location| location.coordinates.latitude);
        let other_longitude = other_location.map_or(0.0, |location| location.coordinates.longitude);
        let other_accuracy =
            other_location.map_or(0, |location| location.coordinates.accuracy_radius);

        if asn != 0 && other_asn == 0 {
            // Having an ASN is better than having none.
            return true;
        }
        if !code.is_empty() && other_code.is_empty() {
            // Having a country is better than having none.
            return true;
        }
        if (latitude != 0.0 || longitude != 0.0) && other_latitude == 0.0 && other_longitude == 0.0
        {
            // Having coordinates is better than having none.
            return true;
        }
        if accuracy < other_accuracy {
            // Smaller accuracy radius is better.
            return true;
        }

        false
    }

    /// Returns the GeoIP location, if present, like Go `LocationOrNil`.
    pub fn location_or_nil(&self) -> Option<&Location> {
        self.location.as_ref()
    }
}

impl fmt::Display for DeviceLocation {
    /// Formats the location like Go `DeviceLocation.String`.
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let Some(location) = &self.location else {
            return match &self.ip {
                Some(ip) => write!(f, "{ip}"),
                None => f.write_str("<nil>"),
            };
        };
        if self.source == DeviceLocationSource::Timezone {
            return write!(
                f,
                "TZ({:.0}/{:.0})",
                location.coordinates.latitude, location.coordinates.longitude
            );
        }
        write!(
            f,
            "{} (AS{} in {} - {})",
            self.ip
                .map_or_else(|| "<nil>".to_string(), |ip| ip.to_string()),
            location.asn,
            location.country.name,
            location.country.code,
        )
    }
}

/// Accuracy radius of a location derived from the system time zone.
pub const TIMEZONE_LOCATION_ACCURACY: u16 = 1000;
/// Latitude of a location derived from the system time zone.
pub const TIMEZONE_LOCATION_LATITUDE: f64 = 48.0;
/// Seconds in half a day; the timezone longitude spans +/-180 degrees.
const SECONDS_PER_HALF_DAY: f64 = 43_200.0;

/// Creates a location from a UTC offset, like Go `getLocationFromTimezone`.
///
/// The offset is injected because Go reads `time.Now().Zone()` from the real
/// machine; see [`LocationEnvironment::timezone_offset_seconds`].
pub fn location_from_timezone(ip_version: IpVersion, offset_seconds: i32) -> DeviceLocation {
    DeviceLocation {
        ip: None,
        ip_version,
        location: Some(Location {
            coordinates: Coordinates {
                accuracy_radius: TIMEZONE_LOCATION_ACCURACY,
                latitude: TIMEZONE_LOCATION_LATITUDE,
                longitude: f64::from(offset_seconds) / SECONDS_PER_HALF_DAY * 180.0,
            },
            ..Location::default()
        }),
        source: DeviceLocationSource::Timezone,
        source_accuracy: DeviceLocationSource::Timezone.accuracy(),
    }
}

/// Multiple device locations, like Go `netenv.DeviceLocations`.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct DeviceLocations {
    #[serde(rename = "All")]
    pub all: Vec<DeviceLocation>,
}

impl DeviceLocations {
    /// Returns the best (most accurate) location, like Go `Best`.
    pub fn best(&self) -> Option<&DeviceLocation> {
        self.all.first()
    }

    /// Returns the best IPv4 location, like Go `BestV4`.
    pub fn best_v4(&self) -> Option<&DeviceLocation> {
        self.all
            .iter()
            .find(|location| location.ip_version == IpVersion::V4)
    }

    /// Returns the best IPv6 location, like Go `BestV6`.
    pub fn best_v6(&self) -> Option<&DeviceLocation> {
        self.all
            .iter()
            .find(|location| location.ip_version == IpVersion::V6)
    }

    /// Returns whether there are no locations.
    pub fn is_empty(&self) -> bool {
        self.all.is_empty()
    }

    /// Copies the list, like Go `DeviceLocations.Copy`.
    ///
    /// Go shares the individual entries; Rust clones them.
    pub fn copy(&self) -> Self {
        self.clone()
    }

    /// Adds a location, replacing a less accurate one with the same IP and
    /// sorting the list best-first, like Go `DeviceLocations.AddLocation`.
    pub fn add_location(&mut self, location: DeviceLocation) {
        let mut exists = false;
        for (index, existing) in self.all.iter().enumerate() {
            if location.ip == existing.ip {
                exists = true;
                if location.is_more_accurate_than(existing) {
                    self.all[index] = location.clone();
                    break;
                }
            }
        }
        if !exists {
            self.all.push(location);
        }

        // Go's sort is unstable; keep the comparator strict so it cannot
        // violate the total-order requirement of Rust's sort implementations.
        self.all.sort_unstable_by(|a, b| {
            if a.is_more_accurate_than(b) {
                std::cmp::Ordering::Less
            } else if b.is_more_accurate_than(a) {
                std::cmp::Ordering::Greater
            } else {
                std::cmp::Ordering::Equal
            }
        });
    }

    /// Adds a location for a global IP address, like Go
    /// `DeviceLocations.AddIP`.
    ///
    /// `lookup` stands in for `geoip.GetLocation`; `None` means the lookup
    /// failed and is logged in Go. Non-global addresses and addresses without
    /// country data are ignored.
    pub fn add_ip(
        &mut self,
        ip: IpAddr,
        source: DeviceLocationSource,
        lookup: impl FnOnce(IpAddr) -> Option<Location>,
    ) -> Option<DeviceLocation> {
        if ip_scope(ip) != IpScope::Global {
            return None;
        }
        let location = lookup(ip)?;
        if location.country.code.is_empty() {
            return None;
        }

        let device_location = DeviceLocation {
            ip: Some(ip),
            ip_version: IpVersion::from_ip(ip),
            location: Some(location),
            source,
            source_accuracy: source.accuracy(),
        };
        self.add_location(device_location.clone());
        Some(device_location)
    }
}

/// Looks up an IP with a [`GeoIp`] handle, discarding errors like Go's
/// `AddIP` caller (which only logs them).
pub fn lookup_with(geoip: &GeoIp, ip: IpAddr) -> Option<Location> {
    geoip.get_location(ip).ok()
}

/// Machine-dependent inputs of device location estimation.
///
/// Each method corresponds to one Go dependency, so tests can provide fakes
/// without interfaces, ICMP sockets, a time zone database or GeoIP files.
pub trait LocationEnvironment {
    /// Go `locationNetworkChangedFlag.IsSet()`.
    fn network_changed(&self) -> bool;
    /// Go `locationNetworkChangedFlag.Refresh()`.
    fn refresh_network_changed(&self);
    /// Go `GetAssignedGlobalAddresses()`, as `(IPv4, IPv6)`.
    fn assigned_global_addresses(&self) -> (Vec<IpAddr>, Vec<IpAddr>);
    /// Go `IPv6Enabled()`.
    fn ipv6_enabled(&self) -> bool;
    /// Go `geoip.GetLocation(ip)`; `None` mirrors a logged lookup error.
    fn get_location(&self, ip: IpAddr) -> Option<Location>;
    /// Go `getLocationFromTraceroute`; `None` mirrors a logged error.
    fn traceroute(&self) -> Option<IpAddr>;
    /// Go `time.Now().Zone()`, as seconds east of UTC.
    fn timezone_offset_seconds(&self) -> i32;
}

/// Handles device location storage and refresh, like the global state in Go
/// `service/netenv/location.go`.
#[derive(Debug)]
pub struct DeviceLocationService<E: LocationEnvironment> {
    environment: E,
    locations: Mutex<DeviceLocations>,
    getting_locations: Mutex<()>,
    disable_network_derived_location: AtomicBool,
}

impl<E: LocationEnvironment> DeviceLocationService<E> {
    /// Creates a service with the given injected environment.
    pub fn new(environment: E) -> Self {
        Self {
            environment,
            locations: Mutex::new(DeviceLocations::default()),
            getting_locations: Mutex::new(()),
            disable_network_derived_location: AtomicBool::new(false),
        }
    }

    /// Returns the injected environment.
    pub fn environment(&self) -> &E {
        &self.environment
    }

    /// Enables or disables network-derived locations, like Go
    /// `DisableNetworkDerivedLocation`.
    pub fn disable_network_derived_location(&self, disable: bool) {
        self.disable_network_derived_location
            .store(disable, Ordering::Release);
    }

    /// Returns whether network-derived locations are disabled.
    pub fn network_derived_location_disabled(&self) -> bool {
        self.disable_network_derived_location
            .load(Ordering::Acquire)
    }

    /// Adds a possible location, like Go `SetInternetLocation`.
    pub fn set_internet_location(
        &self,
        ip: IpAddr,
        source: DeviceLocationSource,
    ) -> Option<DeviceLocation> {
        let mut locations = lock(&self.locations);
        let environment = &self.environment;
        locations.add_ip(ip, source, |ip| environment.get_location(ip))
    }

    /// Returns the possible device locations, like Go `GetInternetLocation`.
    ///
    /// The result is cached until the network-change flag is set. Refreshing
    /// gathers locations from interfaces, traceroute and (as a fallback) the
    /// system time zone, mirroring the Go order exactly.
    pub fn get_internet_location(&self) -> DeviceLocations {
        // Go holds gettingLocationsLock for the whole refresh.
        let _getting = lock(&self.getting_locations);

        if !self.environment.network_changed() {
            return lock(&self.locations).copy();
        }
        self.environment.refresh_network_changed();

        let mut refreshed = DeviceLocations::default();
        let mut v4ok = false;
        let mut v6ok = false;

        if !self.network_derived_location_disabled() {
            let (ipv4, ipv6) = self.environment.assigned_global_addresses();
            let environment = &self.environment;
            for ip in ipv4 {
                if refreshed
                    .add_ip(ip, DeviceLocationSource::Interface, |ip| {
                        environment.get_location(ip)
                    })
                    .is_some()
                {
                    v4ok = true;
                }
            }
            for ip in ipv6 {
                if refreshed
                    .add_ip(ip, DeviceLocationSource::Interface, |ip| {
                        environment.get_location(ip)
                    })
                    .is_some()
                {
                    v6ok = true;
                }
            }
        }

        if !v4ok {
            if !self.network_derived_location_disabled() {
                if let Some(ip) = self.environment.traceroute() {
                    let environment = &self.environment;
                    v4ok = refreshed
                        .add_ip(ip, DeviceLocationSource::Traceroute, |ip| {
                            environment.get_location(ip)
                        })
                        .is_some();
                }
            }
            if !v4ok {
                refreshed.add_location(location_from_timezone(
                    IpVersion::V4,
                    self.environment.timezone_offset_seconds(),
                ));
            }
        }
        if !v6ok && self.environment.ipv6_enabled() {
            refreshed.add_location(location_from_timezone(
                IpVersion::V6,
                self.environment.timezone_offset_seconds(),
            ));
        }
        if refreshed.is_empty() {
            refreshed.add_location(location_from_timezone(
                IpVersion::V4,
                self.environment.timezone_offset_seconds(),
            ));
        }

        let result = refreshed.copy();
        *lock(&self.locations) = refreshed;
        result
    }

    /// Returns the IP of the best location, like Go
    /// `GetApproximateInternetLocation`.
    pub fn approximate_internet_location(&self) -> Option<IpAddr> {
        self.get_internet_location().best()?.ip
    }
}

/// Go's mutexes do not poison; ignore poisoning to keep that behavior.
fn lock<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(PoisonError::into_inner)
}

/// JSON helpers matching Go's `encoding/json` output for `net.IP`.
mod go_json {
    use std::net::IpAddr;

    use serde::de::Deserializer;
    use serde::ser::Serializer;
    use serde::Deserialize;

    pub(super) fn serialize_ip<S: Serializer>(
        ip: &Option<IpAddr>,
        serializer: S,
    ) -> Result<S::Ok, S::Error> {
        match ip {
            Some(ip) => serializer.collect_str(ip),
            None => serializer.serialize_str(""),
        }
    }

    pub(super) fn deserialize_ip<'de, D: Deserializer<'de>>(
        deserializer: D,
    ) -> Result<Option<IpAddr>, D::Error> {
        let text = String::deserialize(deserializer)?;
        if text.is_empty() {
            return Ok(None);
        }
        text.parse().map(Some).map_err(serde::de::Error::custom)
    }
}

#[cfg(test)]
mod tests {
    use std::cell::RefCell;

    use super::*;
    use crate::get_country_info;

    fn location(country: &str, asn: u32, latitude: f64, longitude: f64, accuracy: u16) -> Location {
        Location {
            country: get_country_info(country),
            coordinates: Coordinates {
                accuracy_radius: accuracy,
                latitude,
                longitude,
            },
            asn,
            ..Location::default()
        }
    }

    fn device_location(
        ip: Option<&str>,
        version: IpVersion,
        loc: Option<Location>,
        source: DeviceLocationSource,
    ) -> DeviceLocation {
        DeviceLocation {
            ip: ip.map(|ip| ip.parse().expect("valid IP")),
            ip_version: version,
            location: loc,
            source,
            source_accuracy: source.accuracy(),
        }
    }

    #[test]
    fn source_and_version_match_go() {
        assert_eq!(DeviceLocationSource::Interface.accuracy(), 6);
        assert_eq!(DeviceLocationSource::Peer.accuracy(), 5);
        assert_eq!(DeviceLocationSource::Upnp.accuracy(), 4);
        assert_eq!(DeviceLocationSource::Traceroute.accuracy(), 3);
        assert_eq!(DeviceLocationSource::Other.accuracy(), 2);
        assert_eq!(DeviceLocationSource::Timezone.accuracy(), 1);
        assert_eq!(DeviceLocationSource::Upnp.as_str(), "upnp");

        assert_eq!(IpVersion::V4.as_u8(), 4);
        assert_eq!(IpVersion::V6.as_u8(), 6);
        assert_eq!(IpVersion::V4.to_string(), "IPv4");
        assert_eq!(IpVersion::V6.to_string(), "IPv6");
        assert_eq!(serde_json::to_string(&IpVersion::V6).unwrap(), "6");
    }

    #[test]
    fn accuracy_rules_follow_the_go_switch() {
        let base = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 13335, 0.0, 0.0, 100)),
            DeviceLocationSource::Peer,
        );

        // Higher source accuracy wins first, even if other data is better.
        let mut better_source = base.clone();
        better_source.source = DeviceLocationSource::Interface;
        better_source.source_accuracy = 6;
        assert!(better_source.is_more_accurate_than(&base));

        // IP beats no IP when source accuracy ties.
        let no_ip = device_location(
            None,
            IpVersion::V4,
            base.location.clone(),
            DeviceLocationSource::Peer,
        );
        assert!(base.is_more_accurate_than(&no_ip));
        assert!(!no_ip.is_more_accurate_than(&base));

        // ASN beats no ASN.
        let no_asn = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 0, 0.0, 0.0, 100)),
            DeviceLocationSource::Peer,
        );
        assert!(base.is_more_accurate_than(&no_asn));

        // Country beats no country.
        let no_country = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("", 13335, 0.0, 0.0, 100)),
            DeviceLocationSource::Peer,
        );
        assert!(base.is_more_accurate_than(&no_country));

        // Coordinates beat no coordinates.
        let with_coords = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 13335, 40.0, -70.0, 100)),
            DeviceLocationSource::Peer,
        );
        assert!(with_coords.is_more_accurate_than(&base));

        // Smaller accuracy radius wins.
        let precise = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 13335, 0.0, 0.0, 10)),
            DeviceLocationSource::Peer,
        );
        assert!(precise.is_more_accurate_than(&base));

        // Equal locations are not "more accurate" in either direction.
        assert!(!base.is_more_accurate_than(&base));
    }

    #[test]
    fn sorting_selection_and_replacement() {
        let mut locations = DeviceLocations::default();
        let v4 = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 13335, 40.0, -70.0, 10)),
            DeviceLocationSource::Interface,
        );
        let v6 = device_location(
            Some("2001:4860::1"),
            IpVersion::V6,
            Some(location("US", 15169, 40.0, -70.0, 100)),
            DeviceLocationSource::Interface,
        );
        // A timezone fallback has no IP/ASN/country and a large radius.
        let weak = location_from_timezone(IpVersion::V4, 0);

        locations.add_location(weak.clone());
        locations.add_location(v6.clone());
        locations.add_location(v4.clone());
        assert_eq!(locations.best(), Some(&v4));
        assert_eq!(locations.best_v4(), Some(&v4));
        assert_eq!(locations.best_v6(), Some(&v6));
        assert_eq!(locations.all.len(), 3);

        // A better location with the same IP replaces (and re-sorts) the old.
        let better_v6 = device_location(
            Some("2001:4860::1"),
            IpVersion::V6,
            Some(location("US", 15169, 40.0, -70.0, 5)),
            DeviceLocationSource::Interface,
        );
        locations.add_location(better_v6.clone());
        assert_eq!(locations.all.len(), 3);
        assert_eq!(locations.best(), Some(&better_v6));
        assert_eq!(locations.copy(), locations);
    }

    #[test]
    fn add_ip_filters_scope_and_unknown_countries() {
        let mut locations = DeviceLocations::default();
        let lookup = |ip: IpAddr| {
            if ip.is_ipv6() {
                Some(location("DE", 0, 0.0, 0.0, 100))
            } else {
                Some(location("US", 0, 0.0, 0.0, 100))
            }
        };

        assert!(locations
            .add_ip(
                "192.168.0.1".parse().unwrap(),
                DeviceLocationSource::Interface,
                lookup,
            )
            .is_none());
        assert!(locations
            .add_ip(
                "1.1.1.1".parse().unwrap(),
                DeviceLocationSource::Interface,
                |_| Some(Location::default()),
            )
            .is_none());
        let added = locations
            .add_ip(
                "2001:4860::1".parse().unwrap(),
                DeviceLocationSource::Traceroute,
                lookup,
            )
            .unwrap();
        assert_eq!(added.ip_version, IpVersion::V6);
        assert_eq!(added.source, DeviceLocationSource::Traceroute);
        assert_eq!(locations.all.len(), 1);
    }

    #[test]
    fn timezone_location_matches_go() {
        let utc = location_from_timezone(IpVersion::V4, 0);
        assert_eq!(utc.ip, None);
        assert_eq!(utc.ip_version, IpVersion::V4);
        assert_eq!(utc.source, DeviceLocationSource::Timezone);
        assert_eq!(utc.source_accuracy, 1);
        let loc = utc.location_or_nil().unwrap();
        assert_eq!(loc.coordinates.accuracy_radius, 1000);
        assert_eq!(loc.coordinates.latitude, 48.0);
        assert_eq!(loc.coordinates.longitude, 0.0);

        assert_eq!(
            location_from_timezone(IpVersion::V6, 3600)
                .location_or_nil()
                .unwrap()
                .coordinates
                .longitude,
            15.0
        );
        assert_eq!(
            location_from_timezone(IpVersion::V4, -18_000)
                .location_or_nil()
                .unwrap()
                .coordinates
                .longitude,
            -75.0
        );
    }

    #[test]
    fn string_matches_go() {
        let no_location = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            None,
            DeviceLocationSource::Interface,
        );
        assert_eq!(no_location.to_string(), "1.1.1.1");

        let timezone = location_from_timezone(IpVersion::V4, 3600);
        assert_eq!(timezone.to_string(), "TZ(48/15)");

        let regular = device_location(
            Some("1.1.1.1"),
            IpVersion::V4,
            Some(location("US", 13335, 0.0, 0.0, 100)),
            DeviceLocationSource::Interface,
        );
        assert_eq!(
            regular.to_string(),
            "1.1.1.1 (AS13335 in United States of America - US)"
        );
    }

    #[test]
    fn json_shape_matches_go() {
        let locations = DeviceLocations {
            all: vec![
                device_location(
                    Some("1.1.1.1"),
                    IpVersion::V4,
                    Some(location("US", 13335, 40.0, -70.0, 10)),
                    DeviceLocationSource::Interface,
                ),
                location_from_timezone(IpVersion::V6, 3600),
            ],
        };
        let encoded = serde_json::to_string(&locations).unwrap();
        let value: serde_json::Value = serde_json::from_str(&encoded).unwrap();

        assert_eq!(value["All"][0]["IP"], "1.1.1.1");
        assert_eq!(value["All"][0]["IPVersion"], 4);
        assert_eq!(value["All"][0]["Source"], "interface");
        assert_eq!(value["All"][0]["SourceAccuracy"], 6);
        assert_eq!(value["All"][0]["Location"]["AutonomousSystemNumber"], 13335);
        assert_eq!(value["All"][1]["IP"], "");
        assert_eq!(value["All"][1]["IPVersion"], 6);
        assert_eq!(value["All"][1]["Source"], "timezone");
        assert_eq!(value["All"][1]["SourceAccuracy"], 1);

        // Go's encoding/json emits struct fields in declaration order.
        assert!(
            encoded.starts_with(r#"{"All":[{"IP":"1.1.1.1","IPVersion":4,"Location":{"Country""#)
        );
        assert!(encoded.contains(r#""Source":"interface","SourceAccuracy":6}"#));

        let decoded: DeviceLocations = serde_json::from_str(&encoded).unwrap();
        assert_eq!(decoded, locations);
    }

    #[derive(Debug, Default)]
    struct FakeEnvironment {
        network_changed: RefCell<bool>,
        refreshes: RefCell<usize>,
        ipv4: Vec<IpAddr>,
        ipv6: Vec<IpAddr>,
        ipv6_enabled: bool,
        traceroute: Option<IpAddr>,
        offset_seconds: i32,
        countries: RefCell<std::collections::HashMap<IpAddr, String>>,
        lookups: RefCell<usize>,
    }

    impl FakeEnvironment {
        fn with_country(mut self, ip: &str, country: &str) -> Self {
            self.countries
                .get_mut()
                .insert(ip.parse().unwrap(), country.to_string());
            self
        }
    }

    impl LocationEnvironment for FakeEnvironment {
        fn network_changed(&self) -> bool {
            *self.network_changed.borrow()
        }
        fn refresh_network_changed(&self) {
            *self.refreshes.borrow_mut() += 1;
            *self.network_changed.borrow_mut() = false;
        }
        fn assigned_global_addresses(&self) -> (Vec<IpAddr>, Vec<IpAddr>) {
            (self.ipv4.clone(), self.ipv6.clone())
        }
        fn ipv6_enabled(&self) -> bool {
            self.ipv6_enabled
        }
        fn get_location(&self, ip: IpAddr) -> Option<Location> {
            *self.lookups.borrow_mut() += 1;
            let code = self.countries.borrow().get(&ip)?.clone();
            Some(location(&code, 0, 0.0, 0.0, 100))
        }
        fn traceroute(&self) -> Option<IpAddr> {
            self.traceroute
        }
        fn timezone_offset_seconds(&self) -> i32 {
            self.offset_seconds
        }
    }

    #[test]
    fn refresh_caches_and_uses_interfaces() {
        let environment = FakeEnvironment::default()
            .with_country("1.1.1.1", "US")
            .with_country("2001:4860::1", "DE");
        let mut environment = environment;
        environment.network_changed = RefCell::new(true);
        environment.ipv4 = vec!["1.1.1.1".parse().unwrap()];
        environment.ipv6 = vec!["2001:4860::1".parse().unwrap()];
        environment.ipv6_enabled = true;
        environment.offset_seconds = 3600;

        let service = DeviceLocationService::new(environment);
        let locations = service.get_internet_location();
        assert_eq!(locations.all.len(), 2);
        assert_eq!(
            locations.best_v4().unwrap().source,
            DeviceLocationSource::Interface
        );
        assert_eq!(
            locations.best_v6().unwrap().source,
            DeviceLocationSource::Interface
        );
        assert_eq!(*service.environment().refreshes.borrow(), 1);

        // Cached: no new lookups or refreshes while the flag is unset.
        let before = *service.environment().lookups.borrow();
        assert_eq!(service.get_internet_location(), locations);
        assert_eq!(*service.environment().lookups.borrow(), before);
        assert_eq!(*service.environment().refreshes.borrow(), 1);

        *service.environment().network_changed.borrow_mut() = true;
        service.get_internet_location();
        assert_eq!(*service.environment().refreshes.borrow(), 2);
    }

    #[test]
    fn traceroute_and_timezone_fallbacks() {
        // Successful traceroute: no IPv4 timezone fallback, but IPv6 fallback.
        let environment = FakeEnvironment {
            network_changed: RefCell::new(true),
            traceroute: Some("8.8.8.8".parse().unwrap()),
            ipv6_enabled: true,
            offset_seconds: -18_000,
            ..FakeEnvironment::default()
        }
        .with_country("8.8.8.8", "US");
        let service = DeviceLocationService::new(environment);
        let locations = service.get_internet_location();
        assert_eq!(locations.all.len(), 2);
        assert_eq!(
            locations.best_v4().unwrap().source,
            DeviceLocationSource::Traceroute
        );
        let v6 = locations.best_v6().unwrap();
        assert_eq!(v6.source, DeviceLocationSource::Timezone);
        assert_eq!(v6.location_or_nil().unwrap().coordinates.longitude, -75.0);

        // Failed traceroute: IPv4 timezone fallback, IPv6 disabled.
        let environment = FakeEnvironment {
            network_changed: RefCell::new(true),
            ..FakeEnvironment::default()
        };
        let service = DeviceLocationService::new(environment);
        let locations = service.get_internet_location();
        assert_eq!(locations.all.len(), 1);
        assert_eq!(
            locations.best().unwrap().source,
            DeviceLocationSource::Timezone
        );
        assert_eq!(locations.best().unwrap().ip_version, IpVersion::V4);
        assert_eq!(
            service.approximate_internet_location(),
            None,
            "timezone locations have no IP"
        );
    }

    #[test]
    fn network_derived_location_can_be_disabled() {
        let environment = FakeEnvironment {
            network_changed: RefCell::new(true),
            ipv4: vec!["1.1.1.1".parse().unwrap()],
            traceroute: Some("8.8.8.8".parse().unwrap()),
            ..FakeEnvironment::default()
        }
        .with_country("1.1.1.1", "US")
        .with_country("8.8.8.8", "US");
        let service = DeviceLocationService::new(environment);
        service.disable_network_derived_location(true);
        assert!(service.network_derived_location_disabled());

        let locations = service.get_internet_location();
        assert_eq!(locations.all.len(), 1);
        assert_eq!(
            locations.best().unwrap().source,
            DeviceLocationSource::Timezone
        );
        assert_eq!(*service.environment().lookups.borrow(), 0);
    }

    #[test]
    fn set_internet_location_filters() {
        let environment = FakeEnvironment::default().with_country("8.8.8.8", "US");
        let service = DeviceLocationService::new(environment);
        assert!(service
            .set_internet_location("127.0.0.1".parse().unwrap(), DeviceLocationSource::Peer)
            .is_none());
        assert!(service
            .set_internet_location("9.9.9.9".parse().unwrap(), DeviceLocationSource::Peer)
            .is_none());
        let added = service
            .set_internet_location("8.8.8.8".parse().unwrap(), DeviceLocationSource::Peer)
            .unwrap();
        assert_eq!(added.source, DeviceLocationSource::Peer);
        assert_eq!(
            service.get_internet_location().best().unwrap().ip,
            Some("8.8.8.8".parse().unwrap())
        );
    }
}
