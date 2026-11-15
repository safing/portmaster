//! Behavior tests of the intel entity on top of the filter lists and GeoIP.

use std::net::IpAddr;
use std::sync::Arc;

use pm_database::Databases;
use pm_geoip::{Coordinates, Location};
use pm_intel::{cache_interface, Entity, EntryResource, FilterLists, ListEntry, StaticLocations};

fn setup() -> (Arc<Databases>, Arc<FilterLists>) {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .unwrap();
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .unwrap();

    let filter_lists = Arc::new(FilterLists::with_capacity(
        cache_interface(&databases),
        100,
        10,
        5,
        20,
        20,
    ));
    filter_lists.set_loaded(true);
    (databases, filter_lists)
}

fn store(lists: &FilterLists, type_: &str, entity: &str, sources: &[&str]) {
    lists
        .store_entry(
            &ListEntry {
                type_: type_.to_string(),
                entity: entity.to_string(),
                whitelist: false,
                resources: sources
                    .iter()
                    .map(|source| EntryResource {
                        source_id: source.to_string(),
                        resource_id: "r1".to_string(),
                    })
                    .collect(),
            },
            1_700_000_000,
        )
        .unwrap();
}

fn locations() -> Arc<StaticLocations> {
    let mut locations = StaticLocations::new();
    locations.insert(
        "1.1.1.1".parse().unwrap(),
        Location {
            country: pm_geoip::get_country_info("DE"),
            coordinates: Coordinates {
                accuracy_radius: 100,
                latitude: 51.0,
                longitude: 10.0,
            },
            asn: 64500,
            as_org: "Example Network".to_string(),
            ..Location::default()
        },
    );
    Arc::new(locations)
}

fn make_entity(lists: &Arc<FilterLists>, ip: &str, domain: &str) -> Entity {
    let mut entity = Entity::new(Arc::clone(lists), locations());
    entity.ip = Some(ip.parse().unwrap());
    entity.domain = domain.to_string();
    entity.port = 443;
    entity.init(0);
    entity
}

#[test]
fn loads_lists_and_matches_them() {
    let (_databases, lists) = setup();
    store(&lists, "domain", "www.example.com.", &["list-a", "list-b"]);
    store(&lists, "domain", "example.com.", &["list-c"]);
    store(&lists, "ipv4", "1.1.1.1", &["list-d"]);
    store(&lists, "asn", "64500", &["list-e"]);
    store(&lists, "country", "DE", &["list-f"]);

    let mut entity = make_entity(&lists, "1.1.1.1", "www.example.com.");
    assert_eq!(entity.ip_scope, pm_netstate::IpScope::Global);
    assert_eq!(entity.dst_port(), 443);

    entity.fetch_location();
    let (country, ok) = entity.get_country();
    assert!(ok);
    assert_eq!(country, "DE");
    assert_eq!(entity.get_asn(), (64500, true));
    assert_eq!(entity.country, "DE");
    assert_eq!(entity.asn, 64500);
    assert_eq!(entity.as_org, "Example Network");
    assert!(entity.coordinates.is_some());
    assert!(entity.location_error.is_empty());

    let info = entity.get_country_info().expect("country info");
    assert_eq!(info.name, "Germany");
    assert_eq!(info.continent.code, "EU");

    entity.load_lists();
    assert!(entity.lists_error.is_empty());
    let keys: Vec<&String> = entity.list_occurences.keys().collect();
    assert_eq!(keys, vec!["1.1.1.1", "64500", "DE", "www.example.com."]);
    assert_eq!(
        entity.list_occurences["www.example.com."],
        vec!["list-a", "list-b"]
    );

    // Matching two lists blocks the entity through them.
    assert!(entity.match_lists(&["list-a".to_string(), "list-d".to_string()]));
    assert_eq!(
        entity.blocked_by_lists,
        vec!["list-d".to_string(), "list-a".to_string()]
    );
    assert_eq!(
        entity.blocked_entities,
        vec!["1.1.1.1".to_string(), "www.example.com.".to_string()]
    );

    let reason = entity.list_block_reason();
    assert_eq!(
        reason.to_string(),
        "1.1.1.1 in activated lists list-d and www.example.com. in activated lists list-a and in deactivated lists list-b"
    );

    // No matches and empty input.
    assert!(!entity.match_lists(&[]));
    assert!(!entity.match_lists(&["unknown-list".to_string()]));
    assert!(entity.blocked_by_lists.is_empty());
    assert!(entity.list_block_reason().is_empty());
}

#[test]
fn subdomains_and_cnames_are_checked() {
    let (_databases, lists) = setup();
    store(&lists, "domain", "example.com.", &["list-a"]);
    store(&lists, "domain", "cname.example.com.", &["list-b"]);

    // Sub-domain resolving splits the domain up to the public suffix.
    let mut entity = make_entity(&lists, "1.1.1.1", "deep.www.example.com.");
    entity.resolve_sub_domain_lists(true);
    entity.load_lists();
    assert!(entity.list_occurences.contains_key("example.com."));
    assert!(!entity.list_occurences.contains_key("deep.www.example.com."));

    // CNAME checking includes the CNAMEs of the domain.
    let mut entity = make_entity(&lists, "1.1.1.1", "x.test.");
    entity.cname = vec!["cname.example.com.".to_string()];
    entity.enable_cname_check(true);
    assert!(entity.cname_check_enabled());
    entity.load_lists();
    assert!(entity.list_occurences.contains_key("cname.example.com."));
    assert_eq!(entity.list_occurences["cname.example.com."], vec!["list-b"]);

    // Without the flags only the domain itself is checked.
    let mut entity = make_entity(&lists, "1.1.1.1", "deep.www.example.com.");
    entity.load_lists();
    assert!(entity.list_occurences.is_empty());
}

#[test]
fn reverse_resolving_and_reset() {
    let (_databases, lists) = setup();
    store(&lists, "domain", "example.com.", &["list-a"]);

    let mut entity =
        make_entity(&lists, "1.1.1.1", "").with_reverse_resolver(Arc::new(|ip: &str| {
            if ip == "1.1.1.1" {
                Ok("reversed.example.".to_string())
            } else {
                Err("not found".to_string())
            }
        }));
    entity.enable_reverse_resolving();

    // Without reverse resolving there is no domain.
    assert_eq!(entity.get_domain(false), (String::new(), false));
    // With it, the reverse domain is used.
    assert_eq!(
        entity.get_domain(true),
        ("reversed.example.".to_string(), true)
    );

    entity.load_lists();
    assert!(entity.blocked_by_lists.is_empty());

    // Resetting clears all list data.
    entity.blocked_by_lists = vec!["list-a".to_string()];
    entity.blocked_entities = vec!["example.com.".to_string()];
    entity.reset_lists();
    assert!(entity.list_occurences.is_empty());
    assert!(entity.blocked_by_lists.is_empty());
    assert!(entity.blocked_entities.is_empty());
    assert!(!entity.cname_check_enabled());
}

#[test]
fn non_global_ips_are_not_located() {
    let (_databases, lists) = setup();
    store(&lists, "ipv4", "192.0.2.1", &["list-a"]);

    let mut entity = make_entity(&lists, "192.0.2.1", "example.com.");
    assert_eq!(entity.ip_scope, pm_netstate::IpScope::Invalid);

    entity.fetch_location();
    assert!(entity.get_location().0.is_none());
    assert!(entity.location_error.is_empty());
    assert_eq!(entity.get_country(), (String::new(), true));
    assert_eq!(entity.get_asn(), (0, true));

    entity.load_lists();
    assert!(!entity.list_occurences.contains_key("192.0.2.1"));

    // The IP getter reports the address.
    let (ip, ok) = entity.get_ip();
    assert!(ok);
    assert_eq!(ip, Some("192.0.2.1".parse::<IpAddr>().unwrap()));
}

#[test]
fn location_errors_are_reported() {
    let (_databases, lists) = setup();

    // The static location provider has no data for this address.
    let mut entity = make_entity(&lists, "1.1.1.1", "example.com.");
    entity.fetch_location();
    assert!(entity.get_location().0.is_some(), "the location is known");

    let mut entity = Entity::new(Arc::clone(&lists), Arc::new(StaticLocations::new()));
    entity.ip = Some("1.1.1.1".parse().unwrap());
    entity.init(0);
    entity.fetch_location();
    assert!(!entity.location_error.is_empty());
    assert_eq!(entity.get_country(), (String::new(), false));
    assert_eq!(entity.get_asn(), (0, false));
    assert!(entity.get_country_info().is_none());
    assert!(entity.get_location().0.is_none());
}

#[test]
fn destination_port_overrides_the_remote_port() {
    let (_databases, lists) = setup();
    let mut entity = make_entity(&lists, "1.1.1.1", "example.com.");
    assert_eq!(entity.dst_port(), 443);

    entity.init(8080);
    assert_eq!(entity.dst_port(), 8080);

    // Entities without an IP get an undefined scope.
    let mut bare = Entity::new(Arc::clone(&lists), Arc::new(StaticLocations::new()));
    bare.port = 53;
    bare.init(0);
    assert_eq!(bare.ip_scope, pm_netstate::IpScope::Undefined);
    assert_eq!(bare.dst_port(), 53);
}

#[test]
fn entities_can_be_described() {
    let (_databases, lists) = setup();
    let entity = make_entity(&lists, "1.1.1.1", "example.com.");
    let description = entity.describe();
    assert!(description.contains("1.1.1.1:443"), "{description}");
    assert!(description.contains("example.com."), "{description}");
}
