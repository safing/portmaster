//! Behavior tests of the layered profile (local profile over global config).

use std::sync::Arc;

use pm_config::Config;
use pm_database::Databases;
use pm_endpoints::EpResult;
use pm_intel::{cache_interface, Entity, EntryResource, FilterLists, ListEntry, StaticLocations};
use pm_profile::options::*;
use pm_profile::{
    GlobalProfileConfig, LayeredProfile, Profile, DEFAULT_ACTION_ASK, DEFAULT_ACTION_BLOCK,
    DEFAULT_ACTION_PERMIT, SOURCE_LOCAL,
};
use serde_json::{json, Map, Value};

struct Env {
    config: Arc<Config>,
    global: Arc<GlobalProfileConfig>,
    _databases: Arc<Databases>,
    lists: Arc<FilterLists>,
}

fn env() -> Env {
    let config = Config::new();
    register_options(&config).unwrap();
    let global = Arc::new(GlobalProfileConfig::new(&config));
    global.refresh().unwrap();

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
        config,
        global,
        _databases: databases,
        lists,
    }
}

fn store(lists: &FilterLists, domain: &str, sources: &[&str]) {
    lists
        .store_entry(
            &ListEntry {
                type_: "domain".to_string(),
                entity: domain.to_string(),
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

fn local(config: Value) -> Arc<Profile> {
    Arc::new(Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        config: config.as_object().cloned().unwrap_or_else(Map::new),
        ..Profile::default()
    }))
}

fn layered(env: &Env, config: Value) -> LayeredProfile {
    LayeredProfile::new(local(config), &env.config, &env.global)
}

fn domain_entity(env: &Env, domain: &str) -> Entity {
    let mut entity = Entity::new(Arc::clone(&env.lists), Arc::new(StaticLocations::new()));
    entity.domain = domain.to_string();
    entity.protocol = 6;
    entity.port = 443;
    entity.init(0);
    entity
}

#[test]
fn local_bool_option_wins_over_global() {
    let env = env();
    // Registered global default of blockP2P is false.
    assert!(!(env.global.block_p2p)());
    let lp = layered(&env, json!({"filter": {"blockP2P": true}}));
    assert!(lp.block_p2p());

    // Local false beats a global true.
    env.config
        .set_config_option(CFG_OPTION_BLOCK_INBOUND_KEY, json!(true))
        .unwrap();
    let lp = layered(&env, json!({"filter": {"blockInbound": false}}));
    assert!(!lp.block_inbound());
}

#[test]
fn unset_local_option_falls_back_to_global() {
    let env = env();
    let lp = layered(&env, json!({}));
    assert!(!lp.block_p2p());
    assert!(lp.filter_sub_domains());

    env.config
        .set_config_option(CFG_OPTION_BLOCK_P2P_KEY, json!(true))
        .unwrap();
    assert!(lp.block_p2p());
    assert_eq!(lp.get_profile_source(CFG_OPTION_BLOCK_P2P_KEY), "");
}

#[test]
fn int_and_string_options_resolve() {
    let env = env();
    let lp = layered(
        &env,
        json!({"history": {"keep": 7}, "splittun": {"networkInterface": "eth9"}}),
    );
    assert_eq!(lp.keep_history(), 7);
    assert_eq!(lp.split_tun_interface(), "eth9");
    assert_eq!(lp.split_tun_proxy(), "");

    let lp = layered(&env, json!({}));
    assert_eq!(lp.keep_history(), 30);
    assert_eq!(lp.split_tun_interface(), "");
}

#[test]
fn profile_source_names_the_local_profile() {
    let env = env();
    let profile = local(json!({"filter": {"blockP2P": true}}));
    let lp = LayeredProfile::new(Arc::clone(&profile), &env.config, &env.global);
    assert_eq!(
        lp.get_profile_source(CFG_OPTION_BLOCK_P2P_KEY),
        profile.key()
    );
    assert_eq!(lp.get_profile_source(CFG_OPTION_BLOCK_INBOUND_KEY), "");
    assert_eq!(lp.local_profile().id, profile.id);
}

#[test]
fn default_action_layers() {
    let env = env();
    // Global default is permit.
    assert_eq!(
        layered(&env, json!({})).default_action(),
        DEFAULT_ACTION_PERMIT
    );
    assert_eq!(
        layered(&env, json!({"filter": {"defaultAction": "ask"}})).default_action(),
        DEFAULT_ACTION_ASK
    );
    env.config
        .set_config_option(CFG_OPTION_DEFAULT_ACTION_KEY, json!("block"))
        .unwrap();
    env.global.refresh().unwrap();
    assert_eq!(
        layered(&env, json!({})).default_action(),
        DEFAULT_ACTION_BLOCK
    );
    assert_eq!(layered(&env, json!({})).security_level(), 0);
}

#[test]
fn local_deny_wins_over_global_permit() {
    let env = env();
    env.config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["+ example.com"]))
        .unwrap();
    env.global.refresh().unwrap();
    let lp = layered(&env, json!({"filter": {"endpoints": ["- example.com"]}}));
    let mut entity = domain_entity(&env, "example.com.");
    assert_eq!(lp.match_endpoint(&mut entity).0, EpResult::Denied);
}

#[test]
fn no_local_match_falls_through_to_global() {
    let env = env();
    env.config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["- example.com"]))
        .unwrap();
    env.global.refresh().unwrap();
    let lp = layered(&env, json!({"filter": {"endpoints": ["+ other.org"]}}));
    let mut entity = domain_entity(&env, "example.com.");
    assert_eq!(lp.match_endpoint(&mut entity).0, EpResult::Denied);
    let mut entity = domain_entity(&env, "other.org.");
    assert_eq!(lp.match_endpoint(&mut entity).0, EpResult::Permitted);
    let mut entity = domain_entity(&env, "unknown.net.");
    assert_eq!(lp.match_endpoint(&mut entity).0, EpResult::NoMatch);
}

#[test]
fn service_endpoints_layer_like_endpoints() {
    let env = env();
    env.config
        .set_config_option(CFG_OPTION_SERVICE_ENDPOINTS_KEY, json!(["+ *"]))
        .unwrap();
    env.global.refresh().unwrap();
    let lp = layered(
        &env,
        json!({"filter": {"serviceEndpoints": ["- example.com"]}}),
    );
    assert_eq!(lp.config_error(), None);
    // Incoming rules match the reverse-resolved domain.
    let mut entity = domain_entity(&env, "");
    entity.reverse_domain = "example.com.".to_string();
    assert_eq!(lp.match_service_endpoint(&mut entity).0, EpResult::Denied);
    let mut entity = domain_entity(&env, "");
    entity.reverse_domain = "x.org.".to_string();
    assert_eq!(
        lp.match_service_endpoint(&mut entity).0,
        EpResult::Permitted
    );
}

#[test]
fn split_tun_usage_policy_layers() {
    let env = env();
    env.config
        .set_config_option(
            CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY,
            json!(["- example.com"]),
        )
        .unwrap();
    env.global.refresh().unwrap();
    let lp = layered(
        &env,
        json!({"splittun": {"usagePolicy": ["+ example.com"]}}),
    );
    let mut entity = domain_entity(&env, "example.com.");
    assert_eq!(
        lp.match_split_tun_usage_policy(&mut entity).0,
        EpResult::Permitted
    );
    let lp = layered(&env, json!({}));
    let mut entity = domain_entity(&env, "example.com.");
    assert_eq!(
        lp.match_split_tun_usage_policy(&mut entity).0,
        EpResult::Denied
    );
}

#[test]
fn filter_lists_use_local_set_or_global() {
    let env = env();
    store(&env.lists, "evil.com.", &["MAL"]);
    store(&env.lists, "ads.net.", &["TRAC"]);

    // Global default lists include MAL and TRAC.
    let lp = layered(&env, json!({}));
    let mut entity = domain_entity(&env, "evil.com.");
    entity.load_lists();
    let (result, reason) = lp.match_filter_lists(&mut entity);
    assert_eq!(result, EpResult::Denied);
    assert!(reason.is_some());

    // A local list set replaces the global one entirely.
    let lp = layered(&env, json!({"filter": {"lists": ["TRAC"]}}));
    let mut entity = domain_entity(&env, "evil.com.");
    entity.load_lists();
    assert_eq!(
        lp.match_filter_lists(&mut entity),
        (EpResult::NoMatch, None)
    );
    let mut entity = domain_entity(&env, "ads.net.");
    entity.load_lists();
    assert_eq!(lp.match_filter_lists(&mut entity).0, EpResult::Denied);
}

#[test]
fn filter_lists_apply_sub_domain_and_cname_switches() {
    let env = env();
    store(&env.lists, "evil.com.", &["MAL"]);

    let lp = layered(&env, json!({}));
    let mut entity = domain_entity(&env, "www.evil.com.");
    lp.match_filter_lists(&mut entity);
    assert!(entity.cname_check_enabled());
    entity.load_lists();
    assert_eq!(lp.match_filter_lists(&mut entity).0, EpResult::Denied);

    let lp = layered(
        &env,
        json!({"filter": {"includeSubdomains": false, "includeCNAMEs": false}}),
    );
    let mut entity = domain_entity(&env, "www.evil.com.");
    lp.match_filter_lists(&mut entity);
    assert!(!entity.cname_check_enabled());
    entity.load_lists();
    assert_eq!(lp.match_filter_lists(&mut entity).0, EpResult::NoMatch);
}

#[test]
fn needs_update_after_global_config_change() {
    let env = env();
    let lp = layered(&env, json!({}));
    assert!(!lp.needs_update());
    assert_eq!(lp.revision(), 1);

    env.config
        .set_config_option(CFG_OPTION_BLOCK_P2P_KEY, json!(true))
        .unwrap();
    assert!(lp.needs_update());

    assert_eq!(lp.update(|_| panic!("local profile is not outdated")), 2);
    assert_eq!(lp.revision(), 2);
    assert!(!lp.needs_update());
    // Nothing changed: the revision stays.
    assert_eq!(lp.update(|_| panic!("not called")), 2);
}

#[test]
fn outdated_local_profile_is_replaced() {
    let env = env();
    let profile = local(json!({"filter": {"blockP2P": false}}));
    let lp = LayeredProfile::new(Arc::clone(&profile), &env.config, &env.global);
    assert!(!lp.block_p2p());
    profile.mark_outdated();
    assert!(lp.needs_update());

    let id = profile.id.clone();
    let rev = lp.update(|loaded_id| {
        assert_eq!(loaded_id, id);
        Ok(Profile {
            id: id.clone(),
            source: SOURCE_LOCAL.to_string(),
            config: json!({"filter": {"blockP2P": true}})
                .as_object()
                .cloned()
                .unwrap(),
            ..Profile::default()
        })
    });
    assert_eq!(rev, 2);
    assert!(lp.block_p2p());
    assert!(!lp.needs_update());

    // A failing reload keeps the outdated layer, so it is retried.
    lp.local_profile().mark_outdated();
    let rev = lp.update(|_| Err("boom".to_string()));
    assert_eq!(rev, 3);
    assert!(lp.needs_update());
}
