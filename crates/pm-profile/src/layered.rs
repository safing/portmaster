//! Layered profiles (Go `service/profile/profile-layered.go`).
//!
//! A [`LayeredProfile`] resolves every setting of a connection: the local
//! profile's value if it is set, else the global configuration value.
//!
//! The SPN is removed: there is no SPN usage policy matching and no transit
//! or exit hub policies. The `use_spn` and `spn_routing_algorithm` options are
//! still resolved, as they are plain profile options.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, RwLock};

use pm_config::{Config, Perspective};
use pm_endpoints::{parse_endpoints, Endpoints, EpResult, Reason};
use pm_intel::{Entity, ListBlockReason};
use serde_json::Value;

use crate::options::*;
use crate::profile::{
    DEFAULT_ACTION_ASK, DEFAULT_ACTION_ASK_VALUE, DEFAULT_ACTION_BLOCK, DEFAULT_ACTION_BLOCK_VALUE,
    DEFAULT_ACTION_NOT_SET, DEFAULT_ACTION_PERMIT, DEFAULT_ACTION_PERMIT_VALUE,
};
use crate::{GlobalProfileConfig, Profile, SOURCE_LOCAL};

/// The parsed settings of one profile layer, like the parsed fields of a Go
/// `Profile`.
struct Layer {
    profile: Arc<Profile>,
    perspective: Perspective,
    default_action: u8,
    endpoints: Endpoints,
    service_endpoints: Endpoints,
    split_tun_usage_policy: Endpoints,
    /// Whether the profile sets filter lists (and they could be resolved).
    filter_lists_set: bool,
    filter_list_ids: Vec<String>,
    /// First error of parsing the config.
    error: Option<String>,
}

impl Layer {
    /// Parses the config of the profile, like Go `parseConfig`.
    ///
    /// Invalid values are reported in `error`; everything else stays usable.
    fn parse(profile: Arc<Profile>, config: &Arc<Config>, global: &GlobalProfileConfig) -> Layer {
        let (perspective, perspective_error) =
            config.perspective(&Value::Object(profile.config.clone()));
        let mut last_error = perspective_error.map(|err| err.to_string());

        let mut default_action = DEFAULT_ACTION_NOT_SET;
        if let Some(action) = perspective.get_string(CFG_OPTION_DEFAULT_ACTION_KEY) {
            match action.as_str() {
                DEFAULT_ACTION_PERMIT_VALUE => default_action = DEFAULT_ACTION_PERMIT,
                DEFAULT_ACTION_ASK_VALUE => default_action = DEFAULT_ACTION_ASK,
                DEFAULT_ACTION_BLOCK_VALUE => default_action = DEFAULT_ACTION_BLOCK,
                _ => last_error = Some(format!("default action \"{action}\" invalid")),
            }
        }

        let mut parse = |key: &str| {
            let Some(list) = perspective.get_string_array(key) else {
                return Endpoints::default();
            };
            let (endpoints, err) = parse_endpoints(&list);
            if let Some(err) = err {
                last_error = Some(err.0);
            }
            endpoints
        };
        let endpoints = parse(CFG_OPTION_ENDPOINTS_KEY);
        let service_endpoints = parse(CFG_OPTION_SERVICE_ENDPOINTS_KEY);

        let mut filter_lists_set = false;
        let mut filter_list_ids = Vec::new();
        if let Some(list) = perspective.get_string_array(CFG_OPTION_FILTER_LISTS_KEY) {
            // Like Go, a failed resolve only leaves the lists unset.
            if let Ok(ids) = global.resolve_list_ids(&list) {
                filter_list_ids = ids;
                filter_lists_set = true;
            }
        }

        let mut parse = |key: &str| {
            let Some(list) = perspective.get_string_array(key) else {
                return Endpoints::default();
            };
            let (endpoints, err) = parse_endpoints(&list);
            if let Some(err) = err {
                last_error = Some(err.0);
            }
            endpoints
        };
        let split_tun_usage_policy = parse(CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY);

        Layer {
            profile,
            perspective,
            default_action,
            endpoints,
            service_endpoints,
            split_tun_usage_policy,
            filter_lists_set,
            filter_list_ids,
            error: last_error,
        }
    }
}

struct Inner {
    layers: Vec<Arc<Layer>>,
    revision: u64,
    /// Config generation the layers were built against.
    global_generation: u64,
}

/// A view on a local profile combined with the global configuration.
///
/// Share it with an `Arc`. The settings are read through to the global
/// configuration on every call, so they always reflect the live global value;
/// Go caches them until the next revision.
pub struct LayeredProfile {
    config: Arc<Config>,
    global: Arc<GlobalProfileConfig>,
    inner: RwLock<Inner>,
    security_level: AtomicU64,
}

macro_rules! bool_options {
    ($($(#[$doc:meta])* $name:ident => $key:expr, $global:ident;)*) => {$(
        $(#[$doc])*
        pub fn $name(&self) -> bool {
            self.layered_bool($key, &self.global.$global)
        }
    )*};
}

impl LayeredProfile {
    /// Creates a layered profile from a local profile and the global config.
    pub fn new(
        local_profile: Arc<Profile>,
        config: &Arc<Config>,
        global: &Arc<GlobalProfileConfig>,
    ) -> LayeredProfile {
        let layer = Arc::new(Layer::parse(local_profile, config, global));
        LayeredProfile {
            config: Arc::clone(config),
            global: Arc::clone(global),
            inner: RwLock::new(Inner {
                layers: vec![layer],
                revision: 1,
                global_generation: config.generation(),
            }),
            security_level: AtomicU64::new(0),
        }
    }

    fn layers(&self) -> Vec<Arc<Layer>> {
        self.inner.read().expect("lock poisoned").layers.clone()
    }

    fn layered_bool(&self, key: &str, global: &(dyn Fn() -> bool + Send + Sync)) -> bool {
        for layer in self.layers() {
            if let Some(value) = layer.perspective.get_bool(key) {
                return value;
            }
        }
        global()
    }

    bool_options! {
        disable_auto_permit => CFG_OPTION_DISABLE_AUTO_PERMIT_KEY, disable_auto_permit;
        block_scope_local => CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY, block_scope_local;
        block_scope_lan => CFG_OPTION_BLOCK_SCOPE_LAN_KEY, block_scope_lan;
        block_scope_internet => CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY, block_scope_internet;
        block_p2p => CFG_OPTION_BLOCK_P2P_KEY, block_p2p;
        block_inbound => CFG_OPTION_BLOCK_INBOUND_KEY, block_inbound;
        remove_out_of_scope_dns => CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY, remove_out_of_scope_dns;
        remove_blocked_dns => CFG_OPTION_REMOVE_BLOCKED_DNS_KEY, remove_blocked_dns;
        filter_sub_domains => CFG_OPTION_FILTER_SUB_DOMAINS_KEY, filter_sub_domains;
        filter_cnames => CFG_OPTION_FILTER_CNAME_KEY, filter_cname;
        prevent_bypassing => CFG_OPTION_PREVENT_BYPASSING_KEY, prevent_bypassing;
        domain_heuristics => CFG_OPTION_DOMAIN_HEURISTICS_KEY, domain_heuristics;
        use_spn => CFG_OPTION_USE_SPN_KEY, use_spn;
        enable_history => CFG_OPTION_ENABLE_HISTORY_KEY, enable_history;
        use_split_tun => CFG_OPTION_SPLIT_TUN_USE_KEY, split_tun_use;
    }

    /// Returns the SPN routing algorithm.
    pub fn spn_routing_algorithm(&self) -> String {
        self.layered_string(
            CFG_OPTION_ROUTING_ALGORITHM_KEY,
            &self.global.routing_algorithm,
        )
    }

    /// Returns the name of the network interface used for split tunneling.
    pub fn split_tun_interface(&self) -> String {
        self.layered_string(
            CFG_OPTION_SPLIT_TUN_INTERFACE_KEY,
            &self.global.split_tun_interface,
        )
    }

    /// Returns the split tunneling upstream proxy URL.
    pub fn split_tun_proxy(&self) -> String {
        self.layered_string(CFG_OPTION_SPLIT_TUN_PROXY_KEY, &self.global.split_tun_proxy)
    }

    /// Returns how many days of history are kept.
    pub fn keep_history(&self) -> i64 {
        for layer in self.layers() {
            if let Some(value) = layer.perspective.get_int(CFG_OPTION_KEEP_HISTORY_KEY) {
                return value;
            }
        }
        (self.global.keep_history)()
    }

    fn layered_string(&self, key: &str, global: &(dyn Fn() -> String + Send + Sync)) -> String {
        for layer in self.layers() {
            if let Some(value) = layer.perspective.get_string(key) {
                return value;
            }
        }
        global()
    }

    /// Returns the database key of the first layer that sets the config key.
    /// An empty string means that the global config is effective.
    pub fn get_profile_source(&self, config_key: &str) -> String {
        for layer in self.layers() {
            if layer.perspective.has(config_key) {
                return layer.profile.key();
            }
        }
        String::new()
    }

    /// Returns the local profile.
    pub fn local_profile(&self) -> Arc<Profile> {
        let inner = self.inner.read().expect("lock poisoned");
        Arc::clone(&inner.layers[0].profile)
    }

    /// Returns the first error that occurred parsing the local profile config.
    pub fn config_error(&self) -> Option<String> {
        let inner = self.inner.read().expect("lock poisoned");
        inner.layers[0].error.clone()
    }

    /// Returns the current revision counter.
    pub fn revision(&self) -> u64 {
        self.inner.read().expect("lock poisoned").revision
    }

    /// Returns the highest security level of all layers. It is never raised
    /// (the security level feature was removed), so it is always 0.
    pub fn security_level(&self) -> u8 {
        self.security_level.load(Ordering::Relaxed) as u8
    }

    /// Returns whether a layer is outdated or the global config changed.
    pub fn needs_update(&self) -> bool {
        let inner = self.inner.read().expect("lock poisoned");
        inner.global_generation != self.config.generation()
            || inner.layers.iter().any(|layer| layer.profile.is_outdated())
    }

    /// Replaces outdated layers and re-layers after a global config change,
    /// like Go `Update`. Returns the (possibly bumped) revision counter.
    ///
    /// `load_local` loads the current version of the local profile with the
    /// given ID, like Go `GetLocalProfile`. If it fails, the outdated layer is
    /// kept and the update is retried the next time.
    pub fn update(&self, mut load_local: impl FnMut(&str) -> Result<Profile, String>) -> u64 {
        let mut inner = self.inner.write().expect("lock poisoned");
        let mut changed = false;

        for index in 0..inner.layers.len() {
            let layer = Arc::clone(&inner.layers[index]);
            if !layer.profile.is_outdated() {
                continue;
            }
            // Updating profiles outside of the local source is not supported.
            if layer.profile.source != SOURCE_LOCAL {
                layer.profile.outdated.store(false, Ordering::SeqCst);
                continue;
            }
            changed = true;
            if let Ok(profile) = load_local(&layer.profile.id) {
                inner.layers[index] =
                    Arc::new(Layer::parse(Arc::new(profile), &self.config, &self.global));
            }
        }

        let generation = self.config.generation();
        if inner.global_generation != generation {
            changed = true;
        }
        if changed {
            inner.global_generation = generation;
            inner.revision += 1;
        }
        inner.revision
    }

    /// Returns the active default action (`DEFAULT_ACTION_*`).
    pub fn default_action(&self) -> u8 {
        for layer in self.layers() {
            if layer.default_action > DEFAULT_ACTION_NOT_SET {
                return layer.default_action;
            }
        }
        self.global.default_action_value()
    }

    fn match_layers(
        &self,
        select: impl Fn(&Layer) -> &Endpoints,
        global: Arc<Endpoints>,
        entity: &mut Entity,
    ) -> (EpResult, Option<Reason>) {
        for layer in self.layers() {
            let endpoints = select(&layer);
            if endpoints.is_set() {
                let (result, reason) = endpoints.match_entity(entity);
                if result.is_decision() {
                    return (result, reason);
                }
            }
        }
        global.match_entity(entity)
    }

    /// Matches the entity against the endpoint rules of the local profile,
    /// then the global ones.
    pub fn match_endpoint(&self, entity: &mut Entity) -> (EpResult, Option<Reason>) {
        self.match_layers(|l| &l.endpoints, self.global.endpoints(), entity)
    }

    /// Matches the remote entity of an inbound connection against the service
    /// endpoint rules. Reverse resolving is enabled on the entity first.
    pub fn match_service_endpoint(&self, entity: &mut Entity) -> (EpResult, Option<Reason>) {
        entity.enable_reverse_resolving();
        self.match_layers(
            |l| &l.service_endpoints,
            self.global.service_endpoints(),
            entity,
        )
    }

    /// Matches the entity against the split tunnel usage policies.
    pub fn match_split_tun_usage_policy(&self, entity: &mut Entity) -> (EpResult, Option<Reason>) {
        self.match_layers(
            |l| &l.split_tun_usage_policy,
            self.global.split_tun_usage_policy(),
            entity,
        )
    }

    /// Matches the entity against the filter lists of the first layer that
    /// sets filter lists, or the global lists.
    ///
    /// Like in Go, the caller must have loaded the entity's lists
    /// ([`Entity::load_lists`]) after this call enabled sub-domain and CNAME
    /// resolving. The reason of a denial is the [`ListBlockReason`]; the Rust
    /// `Reason` of endpoint rules is a different type.
    pub fn match_filter_lists(&self, entity: &mut Entity) -> (EpResult, Option<ListBlockReason>) {
        entity.resolve_sub_domain_lists(self.filter_sub_domains());
        entity.enable_cname_check(self.filter_cnames());

        for layer in self.layers() {
            // Search for the first layer that has filter lists set.
            if layer.filter_lists_set {
                if entity.match_lists(&layer.filter_list_ids) {
                    return (EpResult::Denied, Some(entity.list_block_reason()));
                }
                return (EpResult::NoMatch, None);
            }
        }

        let global_lists = self.global.filter_lists();
        if !global_lists.is_empty() && entity.match_lists(&global_lists) {
            return (EpResult::Denied, Some(entity.list_block_reason()));
        }
        (EpResult::NoMatch, None)
    }
}
