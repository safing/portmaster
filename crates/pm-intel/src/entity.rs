//! A remote endpoint and its filter list matches, a port of Go
//! `service/intel/entity.go`.
//!
//! An entity describes a connection target by domain, IP, ASN and country and
//! loads the filter lists that contain it.
//!
//! # Compatibility notes
//!
//! * Go stores the location in a `sync.Once` and callers synchronize the entity
//!   themselves. Here all methods that load data take `&mut self`; callers keep
//!   the same responsibility for sharing.
//! * The GeoIP source is a [`LocationProvider`] so entities can be tested
//!   without a MaxMind database. [`pm_geoip::GeoIp`] implements it.
//! * `splitDomain` uses the `psl` crate. The public suffix list is a snapshot,
//!   like Go's compiled table; both may lag behind publicsuffix.org by a few
//!   commits. The behavior for the rules that are present is the same.
//! * The reverse resolver callback replaces Go's package-level
//!   `reverseResolver` (which the nameserver package sets).
//! * List occurences are kept in a `BTreeMap`, so matches are reported in a
//!   stable, sorted order. Go iterates a map and therefore reports them in a
//!   random order.
//! * `Entity` does not implement `Debug` for the whole struct (trait objects);
//!   use [`Entity::describe`] for logging.

use std::collections::{BTreeMap, HashSet};
use std::net::IpAddr;
use std::sync::Arc;

use pm_geoip::{Coordinates, CountryInfo, Location};
use pm_netstate::{ip_scope, IpScope};

use crate::block_reason::{ListBlockReason, ListMatch};
use crate::FilterLists;

/// Resolves a domain name for an IP address.
pub type ReverseResolver = Arc<dyn Fn(&str) -> Result<String, String> + Send + Sync>;

/// Provides GeoIP locations for IP addresses.
///
/// [`pm_geoip::GeoIp`] implements this trait; tests can provide fixed data.
pub trait LocationProvider: Send + Sync {
    /// Returns the enriched location of an IP address.
    fn get_location(&self, ip: IpAddr) -> Result<Location, pm_geoip::Error>;
}

impl LocationProvider for pm_geoip::GeoIp {
    fn get_location(&self, ip: IpAddr) -> Result<Location, pm_geoip::Error> {
        pm_geoip::GeoIp::get_location(self, ip)
    }
}

/// A location provider with fixed data, for tests.
#[derive(Default)]
pub struct StaticLocations {
    locations: BTreeMap<IpAddr, Location>,
}

impl StaticLocations {
    /// Creates an empty provider.
    pub fn new() -> StaticLocations {
        StaticLocations::default()
    }

    /// Adds a location for an IP address.
    pub fn insert(&mut self, ip: IpAddr, location: Location) {
        self.locations.insert(ip, location);
    }
}

impl LocationProvider for StaticLocations {
    fn get_location(&self, ip: IpAddr) -> Result<Location, pm_geoip::Error> {
        match self.locations.get(&ip) {
            Some(location) => Ok(location.clone()),
            None => Err(pm_geoip::Error::DatabaseNotAvailable),
        }
    }
}

/// A remote endpoint in all its different forms.
pub struct Entity {
    // Process and connection data, set by the caller.
    /// IP address of the connection.
    pub ip: Option<IpAddr>,
    /// Network scope of the IP address.
    pub ip_scope: IpScope,
    /// Protocol number of the connection.
    pub protocol: u8,
    /// Remote port of the connection.
    pub port: u16,
    dst_port: u16,
    /// Target domain of the connection.
    pub domain: String,
    /// Domain the IP address points to; resolved on demand.
    pub reverse_domain: String,
    /// Domain names that the domain resolves to.
    pub cname: Vec<String>,

    // Location data.
    /// Country the IP address is located in.
    pub country: String,
    /// Approximate coordinates of the IP address.
    pub coordinates: Option<Coordinates>,
    /// Autonomous system number of the IP address.
    pub asn: u32,
    /// Owner of the autonomous system.
    pub as_org: String,
    /// Error message if fetching the location failed.
    pub location_error: String,

    // Filter list data.
    /// IDs of the sources that block the entity.
    pub blocked_by_lists: Vec<String>,
    /// Entities that were matched (domain, IP, ASN, country).
    pub blocked_entities: Vec<String>,
    /// Mapping of entities to the sources that contain them.
    pub list_occurences: BTreeMap<String, Vec<String>>,
    /// Error message if loading the lists failed.
    pub lists_error: String,

    // Configuration.
    resolve_sub_domain_lists: bool,
    check_cnames: bool,
    reverse_resolve_enabled: bool,

    // Dependencies.
    filter_lists: Arc<FilterLists>,
    locations: Arc<dyn LocationProvider>,
    reverse_resolver: Option<ReverseResolver>,

    // Lazy state.
    location: Option<Location>,
    location_attempted: bool,
    reverse_resolve_attempted: bool,
    domain_list_loaded: bool,
    domain_list_attempted: bool,
    ip_list_loaded: bool,
    ip_list_attempted: bool,
    country_list_loaded: bool,
    country_list_attempted: bool,
    asn_list_loaded: bool,
    asn_list_attempted: bool,
}

impl Entity {
    /// Creates an entity with the given filter lists and location source.
    pub fn new(filter_lists: Arc<FilterLists>, locations: Arc<dyn LocationProvider>) -> Entity {
        Entity {
            ip: None,
            ip_scope: IpScope::Undefined,
            protocol: 0,
            port: 0,
            dst_port: 0,
            domain: String::new(),
            reverse_domain: String::new(),
            cname: Vec::new(),
            country: String::new(),
            coordinates: None,
            asn: 0,
            as_org: String::new(),
            location_error: String::new(),
            blocked_by_lists: Vec::new(),
            blocked_entities: Vec::new(),
            list_occurences: BTreeMap::new(),
            lists_error: String::new(),
            resolve_sub_domain_lists: false,
            check_cnames: false,
            reverse_resolve_enabled: false,
            filter_lists,
            locations,
            reverse_resolver: None,
            location: None,
            location_attempted: false,
            reverse_resolve_attempted: false,
            domain_list_loaded: false,
            domain_list_attempted: false,
            ip_list_loaded: false,
            ip_list_attempted: false,
            country_list_loaded: false,
            country_list_attempted: false,
            asn_list_loaded: false,
            asn_list_attempted: false,
        }
    }

    /// Sets the reverse resolver callback.
    #[must_use]
    pub fn with_reverse_resolver(mut self, resolver: ReverseResolver) -> Entity {
        self.reverse_resolver = Some(resolver);
        self
    }

    /// Initializes internal metadata, like Go `Init`.
    ///
    /// The IP scope is derived from the IP address; a non-zero `dst_port`
    /// overrides the remote port for endpoint matching.
    pub fn init(&mut self, dst_port: u16) {
        self.ip_scope = match self.ip {
            Some(ip) => ip_scope(ip),
            None => IpScope::Undefined,
        };
        self.dst_port = if dst_port > 0 { dst_port } else { self.port };
    }

    /// Returns the destination port used for endpoint matching.
    pub fn dst_port(&self) -> u16 {
        self.dst_port
    }

    /// Returns a one-line description of the entity, for logging.
    pub fn describe(&self) -> String {
        let ip = self
            .ip
            .map(|ip| ip.to_string())
            .unwrap_or_else(|| "-".to_string());
        format!(
            "{}:{} domain={:?} cname={:?} country={:?} asn={}",
            ip, self.port, self.domain, self.cname, self.country, self.asn
        )
    }

    /// Fetches location data and list entries, like Go `FetchData`.
    pub fn fetch_data(&mut self) {
        self.fetch_location();
        self.load_lists();
    }

    /// Fetches the GeoIP location of the entity.
    ///
    /// Only global IP addresses are looked up.
    pub fn fetch_location(&mut self) {
        if self.location_attempted {
            return;
        }
        self.location_attempted = true;

        let Some(ip) = self.ip else {
            return;
        };
        if !self.ip_scope.is_global() {
            return;
        }

        match self.locations.get_location(ip) {
            Ok(location) => {
                self.country = location.country.code.clone();
                self.coordinates = Some(location.coordinates);
                self.asn = location.asn;
                self.as_org = location.as_org.clone();
                tracing::trace!(
                    ip = %ip,
                    country = %location.country.code,
                    asn = location.asn,
                    "intel: located IP"
                );
                self.location = Some(location);
            }
            Err(error) => {
                tracing::warn!(ip = %ip, %error, "intel: failed to get location data");
                self.location_error = error.to_string();
            }
        }
    }

    /// Returns the raw location data and whether it is set.
    pub fn get_location(&mut self) -> (Option<Location>, bool) {
        self.fetch_location();
        (self.location.clone(), self.location.is_some())
    }

    /// Returns the two letter country code and whether it is set.
    pub fn get_country(&mut self) -> (String, bool) {
        self.fetch_location();
        if !self.location_error.is_empty() {
            return (String::new(), false);
        }
        (self.country.clone(), true)
    }

    /// Returns the country information, if the location is known.
    pub fn get_country_info(&mut self) -> Option<CountryInfo> {
        self.fetch_location();
        if !self.location_error.is_empty() {
            return None;
        }
        self.location
            .as_ref()
            .map(|location| location.country.clone())
    }

    /// Returns the AS number and whether it is set.
    pub fn get_asn(&mut self) -> (u32, bool) {
        self.fetch_location();
        if !self.location_error.is_empty() {
            return (0, false);
        }
        (self.asn, true)
    }

    /// Enables or disables list lookups for sub-domains.
    pub fn resolve_sub_domain_lists(&mut self, enabled: bool) {
        if self.domain_list_loaded && enabled != self.resolve_sub_domain_lists {
            tracing::warn!(
                domain = %self.domain,
                "intel: tried to change sub-domain resolving after the lists were fetched"
            );
        }
        self.resolve_sub_domain_lists = enabled;
    }

    /// Enables or disables list lookups for the CNAMEs of the domain.
    pub fn enable_cname_check(&mut self, enabled: bool) {
        if self.domain_list_loaded && enabled != self.check_cnames {
            tracing::warn!(
                domain = %self.domain,
                "intel: tried to change CNAME resolving after the lists were fetched"
            );
        }
        self.check_cnames = enabled;
    }

    /// Returns whether CNAMEs are checked.
    pub fn cname_check_enabled(&self) -> bool {
        self.check_cnames
    }

    /// Enables reverse resolving of the domain from the IP on demand.
    pub fn enable_reverse_resolving(&mut self) {
        self.reverse_resolve_enabled = true;
    }

    fn reverse_resolve(&mut self) {
        if self.reverse_resolve_attempted {
            return;
        }
        self.reverse_resolve_attempted = true;

        let Some(ip) = self.ip else {
            return;
        };
        let Some(resolver) = self.reverse_resolver.clone() else {
            return;
        };

        match resolver(&ip.to_string()) {
            Ok(domain) => self.reverse_domain = domain,
            Err(error) => {
                tracing::warn!(ip = %ip, %error, "intel: failed to reverse resolve");
            }
        }
    }

    /// Returns the domain and whether it is set.
    pub fn get_domain(&mut self, may_use_reverse_domain: bool) -> (String, bool) {
        if may_use_reverse_domain && self.reverse_resolve_enabled {
            self.reverse_resolve();
            if self.reverse_domain.is_empty() {
                return (String::new(), false);
            }
            return (self.reverse_domain.clone(), true);
        }

        if self.domain.is_empty() {
            return (String::new(), false);
        }
        (self.domain.clone(), true)
    }

    /// Returns the IP and whether it is set.
    pub fn get_ip(&self) -> (Option<IpAddr>, bool) {
        (self.ip, self.ip.is_some())
    }

    /// Resets the list data, like Go `ResetLists`.
    pub fn reset_lists(&mut self) {
        self.blocked_by_lists.clear();
        self.blocked_entities.clear();
        self.list_occurences.clear();
        self.lists_error.clear();

        self.domain_list_loaded = false;
        self.domain_list_attempted = false;
        self.ip_list_loaded = false;
        self.ip_list_attempted = false;
        self.country_list_loaded = false;
        self.country_list_attempted = false;
        self.asn_list_loaded = false;
        self.asn_list_attempted = false;
        self.resolve_sub_domain_lists = false;
        self.check_cnames = false;
    }

    /// Searches all filter lists for all occurrences of the entity, like Go
    /// `LoadLists`.
    pub fn load_lists(&mut self) {
        self.load_domain_lists();
        self.load_asn_lists();
        self.load_ip_lists();
        self.load_country_lists();
    }

    fn merge_list(&mut self, key: &str, list: &[String]) {
        if list.is_empty() {
            return;
        }
        let entry = self.list_occurences.entry(key.to_string()).or_default();
        let mut merged: HashSet<String> = entry.iter().cloned().collect();
        merged.extend(list.iter().cloned());
        let mut merged: Vec<String> = merged.into_iter().collect();
        merged.sort();
        *entry = merged;
    }

    fn load_domain_lists(&mut self) {
        if self.domain_list_loaded || self.domain_list_attempted {
            return;
        }

        let (domain, ok) = self.get_domain(false);
        if !ok {
            return;
        }
        self.domain_list_attempted = true;

        let mut domains_to_inspect = vec![domain];
        if self.check_cnames && !self.cname.is_empty() {
            domains_to_inspect.extend(self.cname.iter().cloned());
        }

        let mut domains = Vec::new();
        if self.resolve_sub_domain_lists {
            for domain in &domains_to_inspect {
                domains.extend(split_domain(domain));
            }
        } else {
            domains.extend(domains_to_inspect);
        }
        let domains = distinct(domains);

        for domain in domains {
            match self.filter_lists.lookup_domain(&domain) {
                Ok(list) => self.merge_list(&domain, &list),
                Err(error) => {
                    tracing::error!(domain = %domain, %error, "intel: failed to get domain blocklists");
                    self.lists_error = error.to_string();
                    return;
                }
            }
        }
        self.domain_list_loaded = true;
    }

    fn load_asn_lists(&mut self) {
        if self.asn_list_loaded || self.asn_list_attempted {
            return;
        }

        let (asn, ok) = self.get_asn();
        if !ok || asn == 0 {
            return;
        }
        self.asn_list_attempted = true;

        let asn = asn.to_string();
        match self.filter_lists.lookup_asn(&asn) {
            Ok(list) => self.merge_list(&asn, &list),
            Err(error) => {
                tracing::error!(asn = %asn, %error, "intel: failed to get ASN blocklist");
                self.lists_error = error.to_string();
                return;
            }
        }
        self.asn_list_loaded = true;
    }

    fn load_country_lists(&mut self) {
        if self.country_list_loaded || self.country_list_attempted {
            return;
        }

        let (country, ok) = self.get_country();
        if !ok || country.is_empty() {
            return;
        }
        self.country_list_attempted = true;

        match self.filter_lists.lookup_country(&country) {
            Ok(list) => self.merge_list(&country, &list),
            Err(error) => {
                tracing::error!(country = %country, %error, "intel: failed to get country blocklist");
                self.lists_error = error.to_string();
                return;
            }
        }
        self.country_list_loaded = true;
    }

    fn load_ip_lists(&mut self) {
        if self.ip_list_loaded || self.ip_list_attempted {
            return;
        }

        let (Some(ip), true) = self.get_ip() else {
            return;
        };
        if !self.ip_scope.is_global() {
            return;
        }
        self.ip_list_attempted = true;

        let ip_string = ip.to_string();
        match self.filter_lists.lookup_ip(ip) {
            Ok(list) => self.merge_list(&ip_string, &list),
            Err(error) => {
                tracing::error!(ip = %ip_string, %error, "intel: failed to get IP blocklist");
                self.lists_error = error.to_string();
                return;
            }
        }
        self.ip_list_loaded = true;
    }

    /// Matches the entity lists against the given source IDs.
    ///
    /// Updates [`Entity::blocked_by_lists`], [`Entity::blocked_entities`] and
    /// returns whether any list matched.
    pub fn match_lists(&mut self, lists: &[String]) -> bool {
        if lists.is_empty() {
            return false;
        }

        self.blocked_by_lists.clear();
        self.blocked_entities.clear();

        let allowed: HashSet<&String> = lists.iter().collect();
        for (key, key_lists) in &self.list_occurences {
            for list_id in key_lists {
                if allowed.contains(list_id) {
                    self.blocked_by_lists.push(list_id.clone());
                    self.blocked_entities.push(key.clone());
                }
            }
        }

        self.blocked_by_lists = distinct(self.blocked_by_lists.clone());
        self.blocked_entities = distinct(self.blocked_entities.clone());

        !self.blocked_by_lists.is_empty()
    }

    /// Returns the block reason for the entity, like Go `ListBlockReason`.
    pub fn list_block_reason(&self) -> ListBlockReason {
        let active: HashSet<&String> = self.blocked_by_lists.iter().collect();
        let mut matches = Vec::with_capacity(self.blocked_entities.len());

        for entity in &self.blocked_entities {
            let Some(entity_lists) = self.list_occurences.get(entity) else {
                continue;
            };
            let mut active_lists = Vec::new();
            let mut inactive_lists = Vec::new();
            for list in entity_lists {
                if active.contains(list) {
                    active_lists.push(list.clone());
                } else {
                    inactive_lists.push(list.clone());
                }
            }
            matches.push(ListMatch::new(entity, active_lists, inactive_lists));
        }

        ListBlockReason::new(matches)
    }
}

/// Splits a domain into all sub-domains up to the public suffix, like Go
/// `splitDomain`.
///
/// The public suffix list comes from the `psl` crate. IP addresses and domains
/// without sub-domains are returned unchanged.
pub fn split_domain(domain: &str) -> Vec<String> {
    // Go checks for IP addresses first and returns them unchanged.
    if domain.parse::<IpAddr>().is_ok() {
        return vec![domain.to_string()];
    }

    let d = domain.strip_suffix('.').unwrap_or(domain);
    let Some(typed) = psl::suffix(d.as_bytes()) else {
        return vec![domain.to_string()];
    };
    let icann = typed.typ() == Some(psl::Type::Icann);
    let Some(suffix) = std::str::from_utf8(typed.as_bytes()).ok() else {
        return vec![domain.to_string()];
    };

    if suffix == d {
        return vec![domain.to_string()];
    }

    let prefix = &d[..d.len() - suffix.len()];
    let labels: Vec<&str> = prefix
        .split('.')
        .filter(|label| !label.is_empty())
        .collect();

    let mut domains = Vec::with_capacity(labels.len() + 1);
    for index in 0..labels.len() {
        domains.push(format!("{}.{}.", labels[index..].join("."), suffix));
    }
    // If the suffix is not a real TLD, but a public suffix, add it as well.
    if !icann {
        domains.push(format!("{suffix}."));
    }

    domains
}

/// Returns the distinct values of the slice, preserving order, like Go
/// `makeDistinct`.
fn distinct(values: Vec<String>) -> Vec<String> {
    let mut seen = HashSet::with_capacity(values.len());
    let mut result = Vec::with_capacity(values.len());
    for value in values {
        if seen.insert(value.clone()) {
            result.push(value);
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn domains_are_split_like_go() {
        let cases: &[(&str, &[&str])] = &[
            (
                "www.books.amazon.co.uk.",
                &[
                    "www.books.amazon.co.uk.",
                    "books.amazon.co.uk.",
                    "amazon.co.uk.",
                ],
            ),
            ("amazon.co.uk.", &["amazon.co.uk."]),
            (
                "books.amazon.co.uk.",
                &["books.amazon.co.uk.", "amazon.co.uk."],
            ),
            ("www.google.com", &["www.google.com.", "google.com."]),
            ("example.com.", &["example.com."]),
            ("a.b.ck", &["a.b.ck."]),
            ("www.ck", &["www.ck."]),
            ("ck", &["ck"]),
            ("cromulent", &["cromulent"]),
            ("foo.appspot.com", &["foo.appspot.com.", "appspot.com."]),
            (
                "deep.foo.appspot.com",
                &["deep.foo.appspot.com.", "foo.appspot.com.", "appspot.com."],
            ),
            ("192.0.2.1", &["192.0.2.1"]),
            ("", &[""]),
            (".", &["."]),
            ("localhost", &["localhost"]),
            ("XN--80AK6AA92E.com", &["XN--80AK6AA92E.com."]),
            (
                "test.blogspot.com",
                &["test.blogspot.com.", "blogspot.com."],
            ),
            (
                "a.b.c.d.e.f.co.jp.",
                &[
                    "a.b.c.d.e.f.co.jp.",
                    "b.c.d.e.f.co.jp.",
                    "c.d.e.f.co.jp.",
                    "d.e.f.co.jp.",
                    "e.f.co.jp.",
                    "f.co.jp.",
                ],
            ),
            (
                "foo.bar.example.unknowntld",
                &[
                    "foo.bar.example.unknowntld.",
                    "bar.example.unknowntld.",
                    "example.unknowntld.",
                    "unknowntld.",
                ],
            ),
            (
                "www.city.kawasaki.jp",
                &["www.city.kawasaki.jp.", "city.kawasaki.jp."],
            ),
            ("city.kawasaki.jp", &["city.kawasaki.jp."]),
        ];

        for (domain, expected) in cases {
            assert_eq!(split_domain(domain), *expected, "{domain}");
        }
    }

    #[test]
    fn distinct_keeps_the_first_occurrence() {
        assert_eq!(
            distinct(vec![
                "b".to_string(),
                "a".to_string(),
                "b".to_string(),
                "c".to_string()
            ]),
            vec!["b", "a", "c"]
        );
    }
}
