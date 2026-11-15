//! Global profile configuration (Go `service/profile/config-update.go`).

use std::sync::{Arc, RwLock};

use pm_config::Config;
use pm_endpoints::{parse_endpoints, Endpoints};

use crate::options::*;
use crate::profile::{DEFAULT_ACTION_ASK, DEFAULT_ACTION_BLOCK, DEFAULT_ACTION_PERMIT};

type Getter<T> = Box<dyn Fn() -> T + Send + Sync>;

/// Resolves filter list IDs (sources or categories) into distinct source IDs.
pub type ListResolver = Box<dyn Fn(&[String]) -> Result<Vec<String>, String> + Send + Sync>;

#[derive(Default)]
struct Parsed {
    default_action: u8,
    endpoints: Arc<Endpoints>,
    service_endpoints: Arc<Endpoints>,
    split_tun_usage_policy: Arc<Endpoints>,
    spn_usage_policy: Arc<Endpoints>,
    spn_transit_hub_policy: Arc<Endpoints>,
    spn_exit_hub_policy: Arc<Endpoints>,
    filter_lists: Vec<String>,
}

/// Global getters and parsed rule lists, the Rust form of the Go package
/// variables `cfgOption*` and `cfg*`.
pub struct GlobalProfileConfig {
    pub default_action: Getter<String>,
    pub block_scope_internet: Getter<bool>,
    pub block_scope_lan: Getter<bool>,
    pub block_scope_local: Getter<bool>,
    pub block_p2p: Getter<bool>,
    pub block_inbound: Getter<bool>,
    pub filter_sub_domains: Getter<bool>,
    pub filter_cname: Getter<bool>,
    pub remove_out_of_scope_dns: Getter<bool>,
    pub remove_blocked_dns: Getter<bool>,
    pub domain_heuristics: Getter<bool>,
    pub prevent_bypassing: Getter<bool>,
    pub disable_auto_permit: Getter<bool>,
    pub enable_history: Getter<bool>,
    pub keep_history: Getter<i64>,
    pub use_spn: Getter<bool>,
    pub routing_algorithm: Getter<String>,
    pub split_tun_use: Getter<bool>,
    pub split_tun_interface: Getter<String>,
    pub split_tun_proxy: Getter<String>,
    endpoints_list: Getter<Vec<String>>,
    service_endpoints_list: Getter<Vec<String>>,
    filter_lists_list: Getter<Vec<String>>,
    split_tun_usage_policy_list: Getter<Vec<String>>,
    spn_usage_policy_list: Getter<Vec<String>>,
    transit_hub_policy_list: Getter<Vec<String>>,
    exit_hub_policy_list: Getter<Vec<String>>,
    resolver: RwLock<Option<ListResolver>>,
    parsed: RwLock<Parsed>,
}

fn boxed<T>(f: impl Fn() -> T + Send + Sync + 'static) -> Getter<T> {
    Box::new(f)
}

// The config getters cache in `Cell`s and are not `Sync`; wrap them so every
// call reads through the shared config instead.
macro_rules! getter {
    ($config:expr, $method:ident, $key:expr, $fallback:expr) => {{
        let config = Arc::clone($config);
        boxed(move || config.$method($key, $fallback)())
    }};
}

impl GlobalProfileConfig {
    /// Creates the getters; the options must be registered. Call
    /// [`refresh`](Self::refresh) to parse the rule lists.
    pub fn new(config: &Arc<Config>) -> GlobalProfileConfig {
        GlobalProfileConfig {
            default_action: getter!(
                config,
                get_as_string,
                CFG_OPTION_DEFAULT_ACTION_KEY,
                DEFAULT_ACTION_PERMIT_VALUE
            ),
            block_scope_internet: getter!(
                config,
                get_as_bool,
                CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY,
                false
            ),
            block_scope_lan: getter!(config, get_as_bool, CFG_OPTION_BLOCK_SCOPE_LAN_KEY, false),
            block_scope_local: getter!(
                config,
                get_as_bool,
                CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY,
                false
            ),
            block_p2p: getter!(config, get_as_bool, CFG_OPTION_BLOCK_P2P_KEY, false),
            block_inbound: getter!(config, get_as_bool, CFG_OPTION_BLOCK_INBOUND_KEY, false),
            filter_sub_domains: getter!(
                config,
                get_as_bool,
                CFG_OPTION_FILTER_SUB_DOMAINS_KEY,
                true
            ),
            filter_cname: getter!(config, get_as_bool, CFG_OPTION_FILTER_CNAME_KEY, true),
            remove_out_of_scope_dns: getter!(
                config,
                get_as_bool,
                CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY,
                true
            ),
            remove_blocked_dns: getter!(
                config,
                get_as_bool,
                CFG_OPTION_REMOVE_BLOCKED_DNS_KEY,
                true
            ),
            domain_heuristics: getter!(config, get_as_bool, CFG_OPTION_DOMAIN_HEURISTICS_KEY, true),
            prevent_bypassing: getter!(config, get_as_bool, CFG_OPTION_PREVENT_BYPASSING_KEY, true),
            disable_auto_permit: getter!(
                config,
                get_as_bool,
                CFG_OPTION_DISABLE_AUTO_PERMIT_KEY,
                true
            ),
            enable_history: getter!(config, get_as_bool, CFG_OPTION_ENABLE_HISTORY_KEY, false),
            keep_history: getter!(config, get_as_int, CFG_OPTION_KEEP_HISTORY_KEY, 30),
            use_spn: getter!(config, get_as_bool, CFG_OPTION_USE_SPN_KEY, true),
            routing_algorithm: getter!(
                config,
                get_as_string,
                CFG_OPTION_ROUTING_ALGORITHM_KEY,
                DEFAULT_ROUTING_PROFILE_ID
            ),
            split_tun_use: getter!(config, get_as_bool, CFG_OPTION_SPLIT_TUN_USE_KEY, false),
            split_tun_interface: getter!(
                config,
                get_as_string,
                CFG_OPTION_SPLIT_TUN_INTERFACE_KEY,
                ""
            ),
            split_tun_proxy: getter!(config, get_as_string, CFG_OPTION_SPLIT_TUN_PROXY_KEY, ""),
            endpoints_list: getter!(config, get_as_string_array, CFG_OPTION_ENDPOINTS_KEY, &[]),
            service_endpoints_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_SERVICE_ENDPOINTS_KEY,
                &[]
            ),
            filter_lists_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_FILTER_LISTS_KEY,
                &DEFAULT_FILTER_LISTS
            ),
            split_tun_usage_policy_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY,
                &[]
            ),
            spn_usage_policy_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_SPN_USAGE_POLICY_KEY,
                &[]
            ),
            transit_hub_policy_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_TRANSIT_HUB_POLICY_KEY,
                &[]
            ),
            exit_hub_policy_list: getter!(
                config,
                get_as_string_array,
                CFG_OPTION_EXIT_HUB_POLICY_KEY,
                &[]
            ),
            resolver: RwLock::new(None),
            parsed: RwLock::new(Parsed::default()),
        }
    }

    /// Sets the filter list ID resolver; without one, IDs are used as is.
    pub fn set_list_resolver(&self, resolver: ListResolver) {
        *self.resolver.write().expect("lock poisoned") = Some(resolver);
    }

    /// Re-parses the rule lists and the default action, like Go
    /// `updateGlobalConfigProfile`. All lists are updated; the last error is
    /// returned.
    pub fn refresh(&self) -> Result<(), String> {
        let mut last_err = None;
        let mut parsed = Parsed::default();

        let action = (self.default_action)();
        parsed.default_action = match action.as_str() {
            DEFAULT_ACTION_PERMIT_VALUE => DEFAULT_ACTION_PERMIT,
            DEFAULT_ACTION_ASK_VALUE => DEFAULT_ACTION_ASK,
            DEFAULT_ACTION_BLOCK_VALUE => DEFAULT_ACTION_BLOCK,
            _ => {
                last_err = Some(format!("default action \"{action}\" invalid"));
                DEFAULT_ACTION_BLOCK
            }
        };

        let mut parse = |list: &Getter<Vec<String>>| {
            let (endpoints, err) = parse_endpoints(&list());
            if let Some(err) = err {
                last_err = Some(err.0);
            }
            Arc::new(endpoints)
        };
        parsed.endpoints = parse(&self.endpoints_list);
        parsed.service_endpoints = parse(&self.service_endpoints_list);
        parsed.split_tun_usage_policy = parse(&self.split_tun_usage_policy_list);
        parsed.spn_usage_policy = parse(&self.spn_usage_policy_list);
        parsed.spn_transit_hub_policy = parse(&self.transit_hub_policy_list);
        parsed.spn_exit_hub_policy = parse(&self.exit_hub_policy_list);

        let ids = (self.filter_lists_list)();
        parsed.filter_lists = match self.resolver.read().expect("lock poisoned").as_ref() {
            Some(resolve) => match resolve(&ids) {
                Ok(resolved) => resolved,
                Err(err) => {
                    last_err = Some(err);
                    Vec::new()
                }
            },
            None => ids,
        };

        *self.parsed.write().expect("lock poisoned") = parsed;
        last_err.map_or(Ok(()), Err)
    }

    fn read<T>(&self, f: impl FnOnce(&Parsed) -> T) -> T {
        f(&self.parsed.read().expect("lock poisoned"))
    }

    /// Returns the default action (`DEFAULT_ACTION_*`).
    pub fn default_action_value(&self) -> u8 {
        self.read(|p| p.default_action)
    }
    pub fn endpoints(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.endpoints))
    }
    pub fn service_endpoints(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.service_endpoints))
    }
    pub fn split_tun_usage_policy(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.split_tun_usage_policy))
    }
    pub fn spn_usage_policy(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.spn_usage_policy))
    }
    pub fn spn_transit_hub_policy(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.spn_transit_hub_policy))
    }
    pub fn spn_exit_hub_policy(&self) -> Arc<Endpoints> {
        self.read(|p| Arc::clone(&p.spn_exit_hub_policy))
    }
    pub fn filter_lists(&self) -> Vec<String> {
        self.read(|p| p.filter_lists.clone())
    }
}
