//! Port of Go `config.go`: the `dns/*` options and typed access to them.

use std::sync::{Arc, Mutex};

use pm_config::{
    CachedValue, Config, ConfigOption, CATEGORY_ANNOTATION, DISPLAY_HINT_ANNOTATION,
    DISPLAY_HINT_ORDERED, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_DEVELOPER,
    EXPERTISE_LEVEL_EXPERT, EXPERTISE_LEVEL_USER, OPT_TYPE_BOOL, OPT_TYPE_INT,
    OPT_TYPE_STRING_ARRAY, QUICK_SETTINGS_ANNOTATION, RELEASE_LEVEL_STABLE, UNIT_ANNOTATION,
};
use serde_json::{json, Value};

use crate::env::CONNECTIVITY_DOMAINS;
use crate::model::{
    HTTPS_PROTOCOL, SERVER_SOURCE_CONFIGURED, SERVER_TYPE_DNS, SERVER_TYPE_DOH, SERVER_TYPE_DOT,
    SERVER_TYPE_TCP, TLS_PROTOCOL,
};
use crate::option_texts::NAMESERVERS_HELP_RAW;
use crate::scopes::{
    format_scope_list, internal_special_use_domains, MULTICAST_DOMAINS, SPECIAL_SERVICE_DOMAINS,
    SPECIAL_USE_DOMAINS,
};
use crate::url::create_resolver;

pub const CFG_OPTION_NAME_SERVERS_KEY: &str = "dns/nameservers";
pub const CFG_OPTION_NO_ASSIGNED_NAMESERVERS_KEY: &str = "dns/noAssignedNameservers";
pub const CFG_OPTION_USE_STALE_CACHE_KEY: &str = "dns/useStaleCache";
pub const CFG_OPTION_NO_MULTICAST_DNS_KEY: &str = "dns/noMulticastDNS";
pub const CFG_OPTION_NO_INSECURE_PROTOCOLS_KEY: &str = "dns/noInsecureProtocols";
pub const CFG_OPTION_DONT_RESOLVE_SPECIAL_DOMAINS_KEY: &str = "dns/dontResolveSpecialDomains";
pub const CFG_OPTION_NAMESERVER_RETRY_RATE_KEY: &str = "dns/nameserverRetryRate";

/// The display hint of options that follow the security level.
const DISPLAY_HINT_SECURITY_LEVEL: &str = "security level";

/// Default nameservers, used when nothing is configured.
pub const DEFAULT_NAME_SERVERS: &[&str] = &[
    "dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
    "dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",
];

/// Default of the nameserver retry rate in seconds.
pub const DEFAULT_NAMESERVER_RETRY_RATE: i64 = 300;

fn strings(list: &[&str]) -> Value {
    json!(list)
}

/// Returns the default nameservers as a list.
pub fn default_name_servers() -> Vec<String> {
    DEFAULT_NAME_SERVERS
        .iter()
        .map(|s| (*s).to_string())
        .collect()
}

fn quick_settings() -> Value {
    json!([
        {
            "Name": "Set Cloudflare (with Malware Filter)",
            "Value": [
                "dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip",
                "dot://cloudflare-dns.com?ip=1.0.0.2&name=Cloudflare&blockedif=zeroip",
            ],
            "Action": "replace",
        },
        {
            "Name": "Set Quad9",
            "Value": [
                "dot://dns.quad9.net?ip=9.9.9.9&name=Quad9&blockedif=empty",
                "dot://dns.quad9.net?ip=149.112.112.112&name=Quad9&blockedif=empty",
            ],
            "Action": "replace",
        },
        {
            "Name": "Set AdGuard",
            "Value": [
                "dot://dns.adguard.com?ip=94.140.14.14&name=AdGuard&blockedif=zeroip",
                "dot://dns.adguard.com?ip=94.140.15.15&name=AdGuard&blockedif=zeroip",
            ],
            "Action": "replace",
        },
        {
            "Name": "Set Foundation for Applied Privacy",
            "Value": ["dot://dot1.applied-privacy.net?ip=146.255.56.98&name=AppliedPrivacy"],
            "Action": "replace",
        },
        {
            "Name": "Add Cloudflare (as fallback)",
            "Value": [
                "dot://cloudflare-dns.com?ip=1.1.1.1&name=Cloudflare&blockedif=zeroip",
                "dot://cloudflare-dns.com?ip=1.0.0.1&name=Cloudflare&blockedif=zeroip",
            ],
            "Action": "merge-bottom",
        },
    ])
}

fn validate_nameservers(value: &Value) -> Result<(), String> {
    let Some(list) = value.as_array() else {
        return Err("invalid type".to_string());
    };
    for (i, entry) in list.iter().enumerate() {
        let Some(entry) = entry.as_str() else {
            return Err("invalid type".to_string());
        };
        if let Err(err) = create_resolver(entry, SERVER_SOURCE_CONFIGURED) {
            return Err(format!(
                "failed to parse DNS server \"{entry}\" (#{}): {err}",
                i + 1
            ));
        }
    }
    Ok(())
}

/// Registers the DNS options, like Go `prepConfig`.
///
/// The Go value migrations of the security-level options are not part of the
/// option definition export and are not applied here (see `pm-config`).
pub fn register_options(config: &Config) -> Result<(), pm_config::Error> {
    let order = |option: ConfigOption, order: i64, category: &str| {
        option
            .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(order))
            .with_annotation(CATEGORY_ANNOTATION, json!(category))
    };

    let mut nameservers = order(
        ConfigOption::new(
            "DNS Servers",
            CFG_OPTION_NAME_SERVERS_KEY,
            "DNS servers to use for resolving DNS requests.",
            OPT_TYPE_STRING_ARRAY,
            json!(DEFAULT_NAME_SERVERS),
        ),
        0,
        "Servers",
    )
    .with_expertise_level(EXPERTISE_LEVEL_USER)
    .with_release_level(RELEASE_LEVEL_STABLE)
    .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_ORDERED))
    .with_annotation(QUICK_SETTINGS_ANNOTATION, quick_settings())
    .with_annotation(
        "self:detail:internalSpecialUseDomains",
        json!(internal_special_use_domains()),
    )
    .with_annotation("self:detail:connectivityDomains", strings(CONNECTIVITY_DOMAINS))
    .with_validation_regex(&format!(
        "^({SERVER_TYPE_DOT}|{SERVER_TYPE_DOH}|{SERVER_TYPE_DNS}|{SERVER_TYPE_TCP}|{HTTPS_PROTOCOL}|{TLS_PROTOCOL})://.*"
    ))
    .with_validation_func(validate_nameservers);
    nameservers.help = NAMESERVERS_HELP_RAW.replace('"', "`");
    nameservers.sensitive = true;
    config.register(nameservers)?;

    config.register(
        order(
            ConfigOption::new(
                "Retry Failing DNS Servers",
                CFG_OPTION_NAMESERVER_RETRY_RATE_KEY,
                "Duration in seconds how often failing DNS server should be retried. This is done continuously in the background.",
                OPT_TYPE_INT,
                json!(DEFAULT_NAMESERVER_RETRY_RATE),
            ),
            32,
            "Servers",
        )
        .with_expertise_level(EXPERTISE_LEVEL_DEVELOPER)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_annotation(UNIT_ANNOTATION, json!("seconds"))
        .with_validation_regex("^[1-9][0-9]{1,5}$"),
    )?;

    config.register(
        order(
            ConfigOption::new(
                "Ignore System/Network Servers",
                CFG_OPTION_NO_ASSIGNED_NAMESERVERS_KEY,
                "Ignore DNS servers configured in your system or network. This may break domains from your local network.",
                OPT_TYPE_BOOL,
                json!(false),
            ),
            1,
            "Servers",
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_SECURITY_LEVEL))
        .with_migration(pm_config::migrate_security_level_to_boolean)
        .with_annotation("self:detail:specialUseDomains", strings(SPECIAL_USE_DOMAINS)),
    )?;

    config.register(
        order(
            ConfigOption::new(
                "Always Use DNS Cache",
                CFG_OPTION_USE_STALE_CACHE_KEY,
                "Always use the DNS cache, even if entries have expired. Expired entries are refreshed afterwards in the background. This can improve DNS resolving performance a lot, but may lead to occasional connection errors due to outdated DNS records.",
                OPT_TYPE_BOOL,
                json!(false),
            ),
            2,
            "Resolving",
        )
        .with_expertise_level(EXPERTISE_LEVEL_USER)
        .with_release_level(RELEASE_LEVEL_STABLE),
    )?;

    config.register(
        order(
            ConfigOption::new(
                "Ignore Multicast DNS",
                CFG_OPTION_NO_MULTICAST_DNS_KEY,
                "Do not resolve using Multicast DNS. This may break certain Plug and Play devices and services.",
                OPT_TYPE_BOOL,
                json!(false),
            ),
            3,
            "Resolving",
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_SECURITY_LEVEL))
        .with_migration(pm_config::migrate_security_level_to_boolean)
        .with_annotation("self:detail:multicastDomains", strings(MULTICAST_DOMAINS)),
    )?;

    config.register(
        order(
            ConfigOption::new(
                "Use Secure Protocols Only",
                CFG_OPTION_NO_INSECURE_PROTOCOLS_KEY,
                "Never resolve using insecure protocols, ie. plain DNS. This may break certain local DNS services, which always use plain DNS.",
                OPT_TYPE_BOOL,
                json!(false),
            ),
            4,
            "Resolving",
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_SECURITY_LEVEL))
        .with_migration(pm_config::migrate_security_level_to_boolean),
    )?;

    config.register(
        order(
            ConfigOption::new(
                "Block Unofficial TLDs",
                CFG_OPTION_DONT_RESOLVE_SPECIAL_DOMAINS_KEY,
                &format!(
                    "Block {}. Unofficial domains may pose a security risk. This setting does not affect .onion domains in the Tor Browser.",
                    format_scope_list(SPECIAL_SERVICE_DOMAINS)
                ),
                OPT_TYPE_BOOL,
                json!(true),
            ),
            16,
            "Resolving",
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_SECURITY_LEVEL))
        .with_migration(pm_config::migrate_security_level_to_boolean)
        .with_annotation(
            "self:detail:specialServiceDomains",
            strings(SPECIAL_SERVICE_DOMAINS),
        ),
    )?;

    Ok(())
}

/// Typed access to the resolver settings.
pub trait Settings: Send + Sync {
    /// Configured nameserver URLs (`dns/nameservers`).
    fn nameservers(&self) -> Vec<String>;
    /// Ignore system/network assigned nameservers.
    fn no_assigned_nameservers(&self) -> bool;
    /// Serve expired cache entries and refresh them in the background.
    fn use_stale_cache(&self) -> bool;
    /// Do not use multicast DNS.
    fn no_multicast_dns(&self) -> bool;
    /// Never use plain DNS.
    fn no_insecure_protocols(&self) -> bool;
    /// Block unofficial TLDs.
    fn dont_resolve_special_domains(&self) -> bool;
    /// Seconds between checks of failing resolvers.
    fn nameserver_retry_rate(&self) -> u64;
}

/// Settings read from a [`Config`].
pub struct ConfigSettings {
    config: Arc<Config>,
}

impl ConfigSettings {
    pub fn new(config: Arc<Config>) -> ConfigSettings {
        ConfigSettings { config }
    }

    /// Resolves a value like the Go getters: the user value (if the release
    /// level allows it), the active default, then the registered default.
    fn value(&self, key: &str) -> Option<CachedValue> {
        let state = self.config.option(key).ok()?;
        if state.definition().release_level <= self.config.release_level() {
            if let Some(value) = state.user_value() {
                return Some(value);
            }
        }
        if let Some(value) = state.active_default_value() {
            return Some(value);
        }
        Some(state.fallback_value())
    }

    fn bool(&self, key: &str, fallback: bool) -> bool {
        match self.value(key) {
            Some(CachedValue::Bool(value)) => value,
            _ => fallback,
        }
    }
}

impl Settings for ConfigSettings {
    fn nameservers(&self) -> Vec<String> {
        match self.value(CFG_OPTION_NAME_SERVERS_KEY) {
            Some(CachedValue::StringArray(value)) => value,
            _ => default_name_servers(),
        }
    }

    fn no_assigned_nameservers(&self) -> bool {
        self.bool(CFG_OPTION_NO_ASSIGNED_NAMESERVERS_KEY, false)
    }

    fn use_stale_cache(&self) -> bool {
        self.bool(CFG_OPTION_USE_STALE_CACHE_KEY, false)
    }

    fn no_multicast_dns(&self) -> bool {
        self.bool(CFG_OPTION_NO_MULTICAST_DNS_KEY, false)
    }

    fn no_insecure_protocols(&self) -> bool {
        self.bool(CFG_OPTION_NO_INSECURE_PROTOCOLS_KEY, false)
    }

    fn dont_resolve_special_domains(&self) -> bool {
        self.bool(CFG_OPTION_DONT_RESOLVE_SPECIAL_DOMAINS_KEY, false)
    }

    fn nameserver_retry_rate(&self) -> u64 {
        match self.value(CFG_OPTION_NAMESERVER_RETRY_RATE_KEY) {
            Some(CachedValue::Int(value)) if value > 0 => value as u64,
            _ => DEFAULT_NAMESERVER_RETRY_RATE as u64,
        }
    }
}

/// The values of [`StaticSettings`].
#[derive(Debug, Clone)]
pub struct SettingValues {
    pub nameservers: Vec<String>,
    pub no_assigned_nameservers: bool,
    pub use_stale_cache: bool,
    pub no_multicast_dns: bool,
    pub no_insecure_protocols: bool,
    pub dont_resolve_special_domains: bool,
    pub nameserver_retry_rate: u64,
}

impl Default for SettingValues {
    /// The registered defaults.
    fn default() -> Self {
        SettingValues {
            nameservers: default_name_servers(),
            no_assigned_nameservers: false,
            use_stale_cache: false,
            no_multicast_dns: false,
            no_insecure_protocols: false,
            dont_resolve_special_domains: true,
            nameserver_retry_rate: DEFAULT_NAMESERVER_RETRY_RATE as u64,
        }
    }
}

/// Settings with fixed, changeable values; for tests and embedding without a config.
#[derive(Default)]
pub struct StaticSettings {
    values: Mutex<SettingValues>,
}

impl StaticSettings {
    pub fn new(values: SettingValues) -> StaticSettings {
        StaticSettings {
            values: Mutex::new(values),
        }
    }

    /// Changes the values.
    pub fn update(&self, change: impl FnOnce(&mut SettingValues)) {
        change(&mut self.values.lock().expect("lock poisoned"));
    }

    fn get<T>(&self, read: impl FnOnce(&SettingValues) -> T) -> T {
        read(&self.values.lock().expect("lock poisoned"))
    }
}

impl Settings for StaticSettings {
    fn nameservers(&self) -> Vec<String> {
        self.get(|v| v.nameservers.clone())
    }
    fn no_assigned_nameservers(&self) -> bool {
        self.get(|v| v.no_assigned_nameservers)
    }
    fn use_stale_cache(&self) -> bool {
        self.get(|v| v.use_stale_cache)
    }
    fn no_multicast_dns(&self) -> bool {
        self.get(|v| v.no_multicast_dns)
    }
    fn no_insecure_protocols(&self) -> bool {
        self.get(|v| v.no_insecure_protocols)
    }
    fn dont_resolve_special_domains(&self) -> bool {
        self.get(|v| v.dont_resolve_special_domains)
    }
    fn nameserver_retry_rate(&self) -> u64 {
        self.get(|v| v.nameserver_retry_rate)
    }
}
