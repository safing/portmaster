//! Registration of the profile configuration options (Go `service/profile/config.go`).

use pm_config::{
    Config, ConfigOption, PossibleValue, CATEGORY_ANNOTATION, DISPLAY_HINT_ANNOTATION,
    DISPLAY_HINT_ONE_OF, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_DEVELOPER,
    EXPERTISE_LEVEL_EXPERT, EXPERTISE_LEVEL_USER, OPT_TYPE_BOOL, OPT_TYPE_INT, OPT_TYPE_STRING,
    OPT_TYPE_STRING_ARRAY, QUICK_SETTINGS_ANNOTATION, RELEASE_LEVEL_BETA, RELEASE_LEVEL_STABLE,
    SETTABLE_PER_APP_ANNOTATION, STACKABLE_ANNOTATION,
};
use pm_endpoints::{
    validate_endpoint_list, DISPLAY_HINT_ENDPOINT_LIST, ENDPOINT_LIST_VERDICT_NAMES_ANNOTATION,
    LIST_ENTRY_VALIDATION_REGEX,
};
use serde_json::{json, Value};

use crate::option_texts::*;
pub use crate::profile::{
    CFG_OPTION_DEFAULT_ACTION_KEY, CFG_OPTION_ENDPOINTS_KEY, CFG_OPTION_FILTER_LISTS_KEY,
    CFG_OPTION_SERVICE_ENDPOINTS_KEY, CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY,
    DEFAULT_ACTION_ASK_VALUE, DEFAULT_ACTION_BLOCK_VALUE, DEFAULT_ACTION_PERMIT_VALUE,
};

pub const CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY: &str = "filter/blockInternet";
pub const CFG_OPTION_BLOCK_SCOPE_LAN_KEY: &str = "filter/blockLAN";
pub const CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY: &str = "filter/blockLocal";
pub const CFG_OPTION_BLOCK_P2P_KEY: &str = "filter/blockP2P";
pub const CFG_OPTION_BLOCK_INBOUND_KEY: &str = "filter/blockInbound";
pub const CFG_OPTION_FILTER_SUB_DOMAINS_KEY: &str = "filter/includeSubdomains";
pub const CFG_OPTION_FILTER_CNAME_KEY: &str = "filter/includeCNAMEs";
pub const CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY: &str = "filter/removeOutOfScopeDNS";
pub const CFG_OPTION_REMOVE_BLOCKED_DNS_KEY: &str = "filter/removeBlockedDNS";
pub const CFG_OPTION_DOMAIN_HEURISTICS_KEY: &str = "filter/domainHeuristics";
pub const CFG_OPTION_PREVENT_BYPASSING_KEY: &str = "filter/preventBypassing";
pub const CFG_OPTION_DISABLE_AUTO_PERMIT_KEY: &str = "filter/disableAutoPermit";
pub const CFG_OPTION_ENABLE_HISTORY_KEY: &str = "history/enable";
pub const CFG_OPTION_KEEP_HISTORY_KEY: &str = "history/keep";
pub const CFG_OPTION_USE_SPN_KEY: &str = "spn/use";
pub const CFG_OPTION_SPN_USAGE_POLICY_KEY: &str = "spn/usagePolicy";
pub const CFG_OPTION_ROUTING_ALGORITHM_KEY: &str = "spn/routingAlgorithm";
pub const CFG_OPTION_TRANSIT_HUB_POLICY_KEY: &str = "spn/transitHubPolicy";
pub const CFG_OPTION_EXIT_HUB_POLICY_KEY: &str = "spn/exitHubPolicy";
pub const CFG_OPTION_SPLIT_TUN_USE_KEY: &str = "splittun/use";
pub const CFG_OPTION_SPLIT_TUN_INTERFACE_KEY: &str = "splittun/networkInterface";
pub const CFG_OPTION_SPLIT_TUN_PROXY_KEY: &str = "splittun/proxy";

/// Default routing profile ID.
pub const DEFAULT_ROUTING_PROFILE_ID: &str = "double-hop";
/// Display hint of options that follow the security level.
pub const DISPLAY_HINT_SECURITY_LEVEL: &str = "security level";
/// Default filter lists.
pub const DEFAULT_FILTER_LISTS: [&str; 4] = ["TRAC", "MAL", "BAD", "UNBREAK"];

const FILTER_LIST_ID_REGEX: &str = r"^[a-zA-Z0-9\-]+$";

fn replace_quotes(text: &str) -> String {
    text.replace('"', "`")
}

fn pv(name: &str, value: &str, description: &str) -> PossibleValue {
    PossibleValue {
        name: name.to_string(),
        description: description.to_string(),
        value: Value::String(value.to_string()),
    }
}

fn base(
    name: &str,
    key: &str,
    description: &str,
    opt_type: u8,
    default: Value,
    order: i64,
    category: &str,
) -> ConfigOption {
    ConfigOption::new(name, key, description, opt_type, default)
        .with_annotation(SETTABLE_PER_APP_ANNOTATION, json!(true))
        .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(order))
        .with_annotation(CATEGORY_ANNOTATION, json!(category))
}

fn security_level(option: ConfigOption) -> ConfigOption {
    option
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_SECURITY_LEVEL))
        .with_migration(pm_config::migrate_security_level_to_boolean)
}

// Go marshals maps sorted by key; keep the verdict names sorted.
fn endpoint_list(option: ConfigOption, help: &str, verdicts: [(&str, &str); 2]) -> ConfigOption {
    let names: serde_json::Map<String, Value> = verdicts
        .iter()
        .map(|(k, v)| ((*k).to_string(), json!(v)))
        .collect();
    let mut option = option
        .with_annotation(STACKABLE_ANNOTATION, json!(true))
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_ENDPOINT_LIST))
        .with_annotation(ENDPOINT_LIST_VERDICT_NAMES_ANNOTATION, Value::Object(names))
        .with_validation_regex(LIST_ENTRY_VALIDATION_REGEX)
        .with_validation_func(validate_list_value);
    option.help = help.to_string();
    option.sensitive = true;
    option
}

fn validate_list_value(value: &Value) -> Result<(), String> {
    let Some(items) = value.as_array() else {
        return Err("invalid type".to_string());
    };
    let list: Vec<&str> = items.iter().filter_map(Value::as_str).collect();
    validate_endpoint_list(&list).map_err(|err| err.0)
}

fn spn_quick_settings() -> Value {
    json!([{"Name": "Loading...", "Value": [""], "Action": "merge-top"}])
}

/// Registers all profile configuration options, like Go `registerConfiguration`.
pub fn register_options(config: &Config) -> Result<(), pm_config::Error> {
    let rules_help = replace_quotes(RULES_HELP_RAW);
    let spn_help = replace_quotes(SPN_HELP_RAW);
    let rules_verdicts = [("+", "Allow"), ("-", "Block")];
    let spn_verdicts = [("+", "Allow"), ("-", "Exclude")];

    config.register(
        base(
            "Default Network Action",
            CFG_OPTION_DEFAULT_ACTION_KEY,
            DESC_DEFAULT_ACTION,
            OPT_TYPE_STRING,
            json!(DEFAULT_ACTION_PERMIT_VALUE),
            1,
            "General",
        )
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_ONE_OF))
        .with_possible_values(vec![
            pv(
                "Allow",
                DEFAULT_ACTION_PERMIT_VALUE,
                "Allow all connections",
            ),
            pv("Block", DEFAULT_ACTION_BLOCK_VALUE, "Block all connections"),
            pv("Prompt", DEFAULT_ACTION_ASK_VALUE, "Prompt for decisions"),
        ]),
    )?;

    config.register(
        security_level(base(
            "Disable Auto Allow",
            CFG_OPTION_DISABLE_AUTO_PERMIT_KEY,
            DESC_DISABLE_AUTO_PERMIT,
            OPT_TYPE_BOOL,
            json!(true),
            65,
            "Advanced",
        ))
        .with_release_level(RELEASE_LEVEL_BETA),
    )?;

    config.register(
        base(
            "Enable Network History",
            CFG_OPTION_ENABLE_HISTORY_KEY,
            DESC_ENABLE_HISTORY,
            OPT_TYPE_BOOL,
            json!(false),
            96,
            "General",
        )
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_expertise_level(EXPERTISE_LEVEL_USER),
    )?;

    config.register(
        base(
            "Keep Network History",
            CFG_OPTION_KEEP_HISTORY_KEY,
            DESC_KEEP_HISTORY,
            OPT_TYPE_INT,
            json!(30),
            97,
            "General",
        )
        .with_annotation(pm_config::UNIT_ANNOTATION, json!("Days"))
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_expertise_level(EXPERTISE_LEVEL_USER),
    )?;

    config.register(endpoint_list(
        base(
            "Outgoing Rules",
            CFG_OPTION_ENDPOINTS_KEY,
            DESC_ENDPOINTS,
            OPT_TYPE_STRING_ARRAY,
            json!([]),
            32,
            "Rules",
        ),
        &rules_help,
        rules_verdicts,
    ))?;

    let service_quick = json!([
        {"Name": "Allow SSH", "Value": ["+ * tcp/22"], "Action": "merge-top"},
        {"Name": "Allow HTTP/s", "Value": ["+ * tcp/80", "+ * tcp/443"], "Action": "merge-top"},
        {"Name": "Allow RDP", "Value": ["+ * */3389"], "Action": "merge-top"},
        {"Name": "Allow all from LAN", "Value": ["+ LAN"], "Action": "merge-top"},
        {"Name": "Allow all from Internet", "Value": ["+ Internet"], "Action": "merge-top"},
        {"Name": "Block everything else", "Value": ["- *"], "Action": "merge-bottom"},
    ]);
    config.register(
        endpoint_list(
            base(
                "Incoming Rules",
                CFG_OPTION_SERVICE_ENDPOINTS_KEY,
                DESC_SERVICE_ENDPOINTS,
                OPT_TYPE_STRING_ARRAY,
                json!([]),
                33,
                "Rules",
            ),
            &rules_help,
            rules_verdicts,
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_annotation(QUICK_SETTINGS_ANNOTATION, service_quick),
    )?;

    config.register(
        base(
            "Filter Lists",
            CFG_OPTION_FILTER_LISTS_KEY,
            DESC_FILTER_LISTS,
            OPT_TYPE_STRING_ARRAY,
            json!(DEFAULT_FILTER_LISTS),
            34,
            "Filter Lists",
        )
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!("filter list"))
        .with_validation_regex(FILTER_LIST_ID_REGEX),
    )?;

    config.register(
        security_level(base(
            "Block Domain Aliases",
            CFG_OPTION_FILTER_CNAME_KEY,
            DESC_FILTER_CNAME,
            OPT_TYPE_BOOL,
            json!(true),
            48,
            "DNS Filtering",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT),
    )?;

    config.register(security_level(base(
        "Block Subdomains of Filter List Entries",
        CFG_OPTION_FILTER_SUB_DOMAINS_KEY,
        DESC_FILTER_SUB_DOMAINS,
        OPT_TYPE_BOOL,
        json!(true),
        36,
        "Filter Lists",
    )))?;

    config.register(
        security_level(base(
            "Force Block Device-Local Connections",
            CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY,
            DESC_BLOCK_SCOPE_LOCAL,
            OPT_TYPE_BOOL,
            json!(false),
            18,
            "Network Scope",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT),
    )?;

    config.register(security_level(base(
        "Force Block LAN",
        CFG_OPTION_BLOCK_SCOPE_LAN_KEY,
        DESC_BLOCK_SCOPE_LAN,
        OPT_TYPE_BOOL,
        json!(false),
        17,
        "Network Scope",
    )))?;

    config.register(security_level(base(
        "Force Block Internet Access",
        CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY,
        DESC_BLOCK_SCOPE_INTERNET,
        OPT_TYPE_BOOL,
        json!(false),
        16,
        "Network Scope",
    )))?;

    config.register(security_level(base(
        "Force Block P2P/Direct Connections",
        CFG_OPTION_BLOCK_P2P_KEY,
        DESC_BLOCK_P2P,
        OPT_TYPE_BOOL,
        json!(false),
        19,
        "Connection Types",
    )))?;

    config.register(security_level(base(
        "Force Block Incoming Connections",
        CFG_OPTION_BLOCK_INBOUND_KEY,
        DESC_BLOCK_INBOUND,
        OPT_TYPE_BOOL,
        json!(true),
        20,
        "Connection Types",
    )))?;

    config.register(
        security_level(base(
            "Enforce Global/Private Split-View",
            CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY,
            DESC_REMOVE_OUT_OF_SCOPE_DNS,
            OPT_TYPE_BOOL,
            json!(true),
            49,
            "DNS Filtering",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_DEVELOPER),
    )?;

    config.register(
        security_level(base(
            "Reject Blocked IPs",
            CFG_OPTION_REMOVE_BLOCKED_DNS_KEY,
            DESC_REMOVE_BLOCKED_DNS,
            OPT_TYPE_BOOL,
            json!(true),
            50,
            "DNS Filtering",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_DEVELOPER),
    )?;

    config.register(
        security_level(base(
            "Enable Domain Heuristics",
            CFG_OPTION_DOMAIN_HEURISTICS_KEY,
            DESC_DOMAIN_HEURISTICS,
            OPT_TYPE_BOOL,
            json!(true),
            51,
            "DNS Filtering",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT),
    )?;

    config.register(
        security_level(base(
            "Block Secure DNS Bypassing",
            CFG_OPTION_PREVENT_BYPASSING_KEY,
            DESC_PREVENT_BYPASSING,
            OPT_TYPE_BOOL,
            json!(true),
            64,
            "Advanced",
        ))
        .with_expertise_level(EXPERTISE_LEVEL_USER)
        .with_release_level(RELEASE_LEVEL_STABLE),
    )?;

    config.register(base(
        "Use SPN",
        CFG_OPTION_USE_SPN_KEY,
        DESC_USE_SPN,
        OPT_TYPE_BOOL,
        json!(true),
        129,
        "General",
    ))?;

    config.register(endpoint_list(
        base(
            "SPN Rules",
            CFG_OPTION_SPN_USAGE_POLICY_KEY,
            DESC_SPN_USAGE_POLICY,
            OPT_TYPE_STRING_ARRAY,
            json!([]),
            130,
            "General",
        ),
        &rules_help,
        spn_verdicts,
    ))?;

    config.register(
        endpoint_list(
            base(
                "Transit Node Rules",
                CFG_OPTION_TRANSIT_HUB_POLICY_KEY,
                DESC_TRANSIT_HUB_POLICY,
                OPT_TYPE_STRING_ARRAY,
                json!([]),
                146,
                "Routing",
            ),
            &spn_help,
            spn_verdicts,
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_annotation(QUICK_SETTINGS_ANNOTATION, spn_quick_settings()),
    )?;

    config.register(
        endpoint_list(
            base(
                "Exit Node Rules",
                CFG_OPTION_EXIT_HUB_POLICY_KEY,
                DESC_EXIT_HUB_POLICY,
                OPT_TYPE_STRING_ARRAY,
                json!([]),
                147,
                "Routing",
            ),
            &spn_help,
            spn_verdicts,
        )
        .with_annotation(QUICK_SETTINGS_ANNOTATION, spn_quick_settings()),
    )?;

    config.register(
        base(
            "Select SPN Routing Algorithm",
            CFG_OPTION_ROUTING_ALGORITHM_KEY,
            DESC_ROUTING_ALGORITHM,
            OPT_TYPE_STRING,
            json!(DEFAULT_ROUTING_PROFILE_ID),
            144,
            "Routing",
        )
        .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_ONE_OF))
        .with_possible_values(vec![
            pv("Plain VPN Mode", "home", "Always connect to the destination directly from the Home Hub. Only provides very basic privacy, as the Home Hub both knows where you are coming from and where you are connecting to."),
            pv("Speed Focused", "single-hop", "Optimize routes with a minimum of one hop. Provides good speeds. This will often use the Home Hub to connect to destinations near you, but will use more hops to far away destinations for better privacy over long distances."),
            pv("Balanced", "double-hop", "Optimize routes with a minimum of two hops. Provides good privacy as well as good speeds. No single node knows where you are coming from *and* where you are connecting to."),
            pv("Privacy Focused", "triple-hop", "Optimize routes with a minimum of three hops. Provides very good privacy. No single node knows where you are coming from *and* where you are connecting to - with an additional hop just to be sure."),
        ]),
    )?;

    config.register(base(
        "Use Split Tunnel",
        CFG_OPTION_SPLIT_TUN_USE_KEY,
        DESC_SPLIT_TUN_USE,
        OPT_TYPE_BOOL,
        json!(false),
        212,
        "General",
    ))?;

    let mut interface = base(
        "Network Interface",
        CFG_OPTION_SPLIT_TUN_INTERFACE_KEY,
        DESC_SPLIT_TUN_INTERFACE,
        OPT_TYPE_STRING,
        json!(""),
        214,
        "General",
    )
    .with_validation_func(|value| match value.as_str() {
        Some(s) if !s.is_empty() && s.trim().is_empty() => {
            Err("network interface cannot contain only whitespace characters".to_string())
        }
        _ => Ok(()),
    });
    interface.sensitive = true;
    config.register(interface)?;

    let mut proxy = base(
        "Upstream Proxy",
        CFG_OPTION_SPLIT_TUN_PROXY_KEY,
        DESC_SPLIT_TUN_PROXY,
        OPT_TYPE_STRING,
        json!(""),
        215,
        "General",
    )
    .with_validation_func(|value| match value.as_str() {
        Some(s) if !s.is_empty() => crate::proxy_url::validate_upstream_proxy_url(s),
        _ => Ok(()),
    });
    proxy.sensitive = true;
    config.register(proxy)?;

    config.register(endpoint_list(
        base(
            "Split Tunnel Rules",
            CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY,
            DESC_SPLIT_TUN_USAGE_POLICY,
            OPT_TYPE_STRING_ARRAY,
            json!([]),
            216,
            "General",
        ),
        &rules_help,
        spn_verdicts,
    ))?;

    Ok(())
}
