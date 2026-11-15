use std::net::IpAddr;
use std::sync::Arc;

use pm_database::Databases;
use pm_endpoints::{parse_endpoint, parse_endpoints, EpResult};
use pm_geoip::{Coordinates, Location};
use pm_intel::{cache_interface, Entity, EntryResource, FilterLists, ListEntry, StaticLocations};
use serde_json::Value;

const FIXTURE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/endpoints.json");

struct Env {
    _databases: Arc<Databases>,
    lists: Arc<FilterLists>,
    locations: Arc<StaticLocations>,
}

fn env(locations: StaticLocations) -> Env {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .unwrap();
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .unwrap();
    let lists = Arc::new(FilterLists::with_capacity(
        cache_interface(&databases),
        100,
        10,
        5,
        20,
        20,
    ));
    lists.set_loaded(true);
    Env {
        _databases: databases,
        lists,
        locations: Arc::new(locations),
    }
}

fn entity_from_spec(env: &Env, spec: &Value) -> Entity {
    let mut e = Entity::new(Arc::clone(&env.lists), env.locations.clone());
    let s = |k: &str| {
        spec.get(k)
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string()
    };
    if !s("ip").is_empty() {
        e.ip = Some(s("ip").parse().unwrap());
    }
    e.domain = s("domain");
    e.cname = spec
        .get("cname")
        .and_then(Value::as_array)
        .map(|a| a.iter().map(|v| v.as_str().unwrap().to_string()).collect())
        .unwrap_or_default();
    e.protocol = spec["protocol"].as_u64().unwrap() as u8;
    e.port = spec["port"].as_u64().unwrap() as u16;
    e.location_error = s("location_error");
    e.init(0);
    e.enable_cname_check(
        spec.get("cname_check")
            .and_then(Value::as_bool)
            .unwrap_or(false),
    );
    e
}

fn result_code(r: EpResult) -> u64 {
    r as u8 as u64
}

#[test]
fn rules_parse_render_and_match_like_go() {
    let fixture: Value = serde_json::from_str(&std::fs::read_to_string(FIXTURE).unwrap()).unwrap();
    let env = env(StaticLocations::new());
    let entities = fixture["entities"].as_array().unwrap();
    let mut checked = 0;

    for case in fixture["cases"].as_array().unwrap() {
        let rule = case["rule"].as_str().unwrap();
        let (eps, err) = parse_endpoints(&[rule]);
        if let Some(expected) = case.get("error") {
            assert_eq!(
                err.map(|e| e.0).as_deref(),
                expected.as_str(),
                "rule {rule:?}"
            );
            assert!(eps.0.is_empty());
            continue;
        }
        assert!(err.is_none(), "rule {rule:?} failed: {err:?}");
        let ep = &eps.0[0];
        assert_eq!(
            ep.to_string(),
            case["string"].as_str().unwrap(),
            "rule {rule:?}"
        );

        for spec in entities {
            let name = spec["name"].as_str().unwrap();
            let Some(expected) = case["results"].get(name) else {
                continue;
            };
            let mut entity = entity_from_spec(&env, spec);
            let (result, reason) = ep.matches(&mut entity);
            assert_eq!(
                result_code(result),
                expected["result"].as_u64().unwrap(),
                "rule {rule:?} entity {name}"
            );
            match (reason, expected.get("reason")) {
                (Some(reason), Some(text)) => {
                    assert_eq!(
                        reason.to_string(),
                        text.as_str().unwrap(),
                        "rule {rule:?} entity {name}"
                    );
                    let ctx = pm_dsd::gojson::to_vec(&reason).unwrap();
                    assert_eq!(
                        String::from_utf8(ctx).unwrap(),
                        expected["context"].as_str().unwrap(),
                        "rule {rule:?} entity {name}"
                    );
                }
                (None, None) => {}
                (got, want) => panic!("rule {rule:?} entity {name}: reason {got:?} vs {want:?}"),
            }
            checked += 1;
        }
    }
    assert!(checked > 1000, "only {checked} matches checked");

    for case in fixture["lists"].as_array().unwrap() {
        let rules: Vec<&str> = case["rules"]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r.as_str().unwrap())
            .collect();
        let (eps, err) = parse_endpoints(&rules);
        assert_eq!(
            err.map(|e| e.0).as_deref(),
            case.get("error").and_then(Value::as_str)
        );
        assert_eq!(eps.0.len() as u64, case["parsed"].as_u64().unwrap());
        assert_eq!(eps.to_string(), case["string"].as_str().unwrap());
        for spec in entities {
            let name = spec["name"].as_str().unwrap();
            let Some(expected) = case["results"].get(name) else {
                continue;
            };
            let mut entity = entity_from_spec(&env, spec);
            let (result, _) = eps.match_entity(&mut entity);
            assert_eq!(
                result_code(result),
                expected.as_u64().unwrap(),
                "{rules:?} entity {name}"
            );
        }
    }

    assert_eq!(
        fixture["validationRegex"].as_str().unwrap(),
        pm_endpoints::LIST_ENTRY_VALIDATION_REGEX
    );
}

fn located_env() -> Env {
    let mut locations = StaticLocations::new();
    locations.insert(
        "194.232.104.1".parse().unwrap(),
        Location {
            country: pm_geoip::get_country_info("AT"),
            coordinates: Coordinates::default(),
            asn: 1901,
            as_org: "Example AT".to_string(),
            ..Location::default()
        },
    );
    env(locations)
}

fn global_entity(env: &Env, ip: &str, protocol: u8, port: u16) -> Entity {
    let mut e = Entity::new(Arc::clone(&env.lists), env.locations.clone());
    e.ip = Some(ip.parse::<IpAddr>().unwrap());
    e.protocol = protocol;
    e.port = port;
    e.init(0);
    e
}

#[test]
fn geo_rules_match_with_location_data() {
    let env = located_env();
    let at = || global_entity(&env, "194.232.104.1", 6, 443);

    let cases = [
        (
            "+ AT",
            EpResult::Permitted,
            Some("allowed by rule: IP is located in AT"),
        ),
        (
            "- AT",
            EpResult::Denied,
            Some("denied by rule: IP is located in AT"),
        ),
        ("+ DE", EpResult::NoMatch, None),
        (
            "+ C:EU",
            EpResult::Permitted,
            Some("allowed by rule: IP is located in C:EU"),
        ),
        ("+ C:AS", EpResult::NoMatch, None),
        (
            "- AS1901",
            EpResult::Denied,
            Some("denied by rule: IP is part of AS AS1901"),
        ),
        ("+ AS1902", EpResult::NoMatch, None),
        ("+ AT UDP", EpResult::NoMatch, None),
        (
            "+ AS1901 TCP/443",
            EpResult::Permitted,
            Some("allowed by rule: IP is part of AS AS1901 TCP/HTTPS"),
        ),
    ];
    for (rule, want, reason) in cases {
        let ep = parse_endpoint(rule).unwrap();
        let (got, why) = ep.matches(&mut at());
        assert_eq!(got, want, "{rule}");
        assert_eq!(why.map(|r| r.to_string()).as_deref(), reason, "{rule}");
    }

    let ep = parse_endpoint("+ AT").unwrap();
    let (_, why) = ep.matches(&mut at());
    assert_eq!(why.unwrap().value, "Austria (AT)");

    // No location data: allow rules skip, deny rules fail safe.
    let mut unknown = global_entity(&env, "104.82.234.109", 6, 443);
    assert_eq!(
        parse_endpoint("+ AT").unwrap().matches(&mut unknown).0,
        EpResult::NoMatch
    );
    assert_eq!(
        parse_endpoint("- AT").unwrap().matches(&mut unknown).0,
        EpResult::MatchError
    );
    let (eps, _) = parse_endpoints(&["+ AS32590", "- *"]);
    assert_eq!(eps.match_entity(&mut unknown).0, EpResult::Denied);
}

#[test]
fn list_rules_report_the_filter_list() {
    let env = env(StaticLocations::new());
    env.lists
        .store_entry(
            &ListEntry {
                type_: "domain".to_string(),
                entity: "tracker.example.".to_string(),
                whitelist: false,
                resources: vec![EntryResource {
                    source_id: "TRAC".to_string(),
                    resource_id: "r1".to_string(),
                }],
            },
            1_700_000_000,
        )
        .unwrap();
    let mut e = Entity::new(Arc::clone(&env.lists), env.locations.clone());
    e.domain = "tracker.example.".to_string();
    e.protocol = 6;
    e.port = 443;
    e.init(0);
    e.load_lists();

    let ep = parse_endpoint("- L:TRAC,MAL").unwrap();
    let (result, reason) = ep.matches(&mut e);
    assert_eq!(result, EpResult::Denied);
    let reason = reason.unwrap();
    assert_eq!(
        reason.to_string(),
        "denied by rule: filterlist contains L:TRAC,MAL"
    );
    assert!(reason.extra.contains_key("filterlist"));

    assert_eq!(
        parse_endpoint("- L:MAL").unwrap().matches(&mut e).0,
        EpResult::NoMatch
    );
}

#[test]
fn comment_only_rule_is_an_error_not_a_panic() {
    assert!(parse_endpoint("+ # just a comment").is_err());
    assert!(parse_endpoint("+ * TCP/+80").is_err());
    assert!(parse_endpoint("+ * +6").is_err());
}
