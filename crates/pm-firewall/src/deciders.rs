//! Port of the deciders of Go `service/firewall/master.go`.
//!
//! Every decider returns `true` if it came to a conclusion and set the
//! verdict of the connection. Reason texts are shown in the UI and must stay
//! byte-equal to Go.

use std::net::IpAddr;

use pm_endpoints::{parse_endpoints, Endpoints, EpResult};
use pm_netenv::OnlineStatus;
use pm_netstate::IpScope;
use pm_network::{Connection, ConnectionType, Verdict};
use pm_profile::options::*;
use pm_profile::{LayeredProfile, DEFAULT_ACTION_PERMIT};
use serde_json::Value;

use crate::bypass::{prevent_bypassing, BypassResponder};
use crate::env::FirewallEnv;
use crate::{dga, is_system_resolver, levenshtein};

pub const NO_REASON_OPTION_KEY: &str = "";
pub const CFG_OPTION_CUSTOM_LIST_FILE_KEY: &str = "filter/customListFile";

const P2P_FILTER_LISTS: [&str; 1] = ["17-P2P"];

/// A decider; returns whether it came to a conclusion.
pub type Decider = fn(&mut Connection, &LayeredProfile, &dyn FirewallEnv) -> bool;

/// The deciders in the order they run.
pub const DEFAULT_DECIDERS: [Decider; 13] = [
    check_portmaster_connection,
    check_if_broadcast_reply,
    check_connection_type,
    check_connection_scope,
    check_endpoint_lists,
    check_invalid_ip,
    check_resolver_scope,
    check_connectivity_domain,
    check_bypass_prevention,
    check_filter_lists,
    check_custom_filter_list,
    check_domain_heuristics,
    check_auto_permit_related,
];

fn context_of<T: serde::Serialize>(reason: &T) -> Option<Value> {
    serde_json::to_value(reason).ok()
}

/// Allows all connections that originate from the Portmaster itself.
pub fn check_portmaster_connection(
    conn: &mut Connection,
    _p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    let Some(process) = &conn.process else {
        return false;
    };
    // Ignore other processes.
    if process.pid != env.own_pid() {
        return false;
    }
    let Some(entity) = &conn.entity else {
        return false;
    };

    // Ignore inbound connections if non-local.
    if conn.inbound {
        let Some(ip) = entity.ip else {
            return false;
        };
        match env.is_my_ip(ip) {
            Ok(true) => {}
            Ok(false) => return false,
            Err(error) => {
                tracing::debug!(
                    "filter: failed to check if {ip} is own IP for granting own connection: {error}"
                );
                return false;
            }
        }
    }

    tracing::info!("filter: granting own connection {conn}");
    conn.accept("connection by Portmaster", NO_REASON_OPTION_KEY);
    conn.internal = true;
    true
}

/// Go `packet.IPProtocol.String`.
fn ip_protocol_name(protocol: u8) -> String {
    match protocol {
        255 => "RAW".to_string(),
        6 => "TCP".to_string(),
        17 => "UDP".to_string(),
        136 => "UDPLite".to_string(),
        1 => "ICMP".to_string(),
        58 => "ICMPv6".to_string(),
        2 => "IGMP".to_string(),
        other => format!("<unknown protocol, {other}>"),
    }
}

/// Go `net.JoinHostPort` for an IP.
fn join_host_port(ip: IpAddr, port: u16) -> String {
    match ip {
        IpAddr::V4(ip) => format!("{ip}:{port}"),
        IpAddr::V6(ip) => format!("[{ip}]:{port}"),
    }
}

pub fn check_if_broadcast_reply(
    conn: &mut Connection,
    _p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    // Only check inbound connections.
    if !conn.inbound {
        return false;
    }
    // Only check if the process has been identified.
    if !conn.process.as_deref().is_some_and(|p| p.is_identified()) {
        return false;
    }
    let Some(ip) = conn.entity.as_ref().and_then(|e| e.ip) else {
        return false;
    };

    // Check if the remote IP is part of a local network.
    let local_net = match env.local_network(ip) {
        Ok(Some(net)) => net,
        Ok(None) => return false,
        Err(error) => {
            tracing::warn!("filter: failed to get local network: {error}");
            return false;
        }
    };

    // Search for a matching requesting connection.
    let Some(request) = env.multicast_request(conn, &local_net) else {
        return false;
    };

    let reason = format!(
        "response to multi/broadcast query to {}/{}",
        ip_protocol_name(request.protocol),
        join_host_port(request.ip, request.port)
    );
    conn.accept(&reason, "");
    true
}

pub fn check_connection_type(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    let inbound = conn.inbound;
    let conn_type = conn.conn_type;
    let Some(entity) = conn.entity.as_mut() else {
        return false;
    };

    // Block incoming connections, if not from localhost.
    if p.block_inbound() && inbound && !entity.ip_scope.is_localhost() {
        conn.drop_conn("inbound connections blocked", CFG_OPTION_BLOCK_INBOUND_KEY);
        return true;
    }

    // Check for P2P and related connections.
    if p.block_p2p() && !inbound {
        // Block anything that is in the P2P filter list.
        if entity.match_lists(&P2P_FILTER_LISTS.map(String::from)) {
            conn.block(
                "P2P assistive infrastructure blocked based on filter list",
                CFG_OPTION_BLOCK_P2P_KEY,
            );
            return true;
        }
        // Remaining P2P deciders only apply to IP connections.
        if conn_type != ConnectionType::Ip {
            return false;
        }
        // Block well known ports of P2P assistive infrastructure.
        let dst_port = entity.dst_port();
        if dst_port == 3478 || dst_port == 5349 {
            // STUN/TURN and STUN/TURN over TLS/DTLS
            conn.block(
                "P2P assistive infrastructure blocked based on port",
                CFG_OPTION_BLOCK_P2P_KEY,
            );
            return true;
        }
        // Block direct connections with no previous DNS request.
        if entity.ip_scope.is_global() && entity.domain.is_empty() {
            conn.block("direct connections (P2P) blocked", CFG_OPTION_BLOCK_P2P_KEY);
            return true;
        }
    }

    false
}

pub fn check_connectivity_domain(
    conn: &mut Connection,
    p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    let Some(entity) = &conn.entity else {
        return false;
    };
    // Only applies if a domain is available, the network status is Portal (or
    // more limited), for outgoing connections of apps that may use the Internet.
    if entity.domain.is_empty()
        || env.online_status() > OnlineStatus::Portal
        || conn.inbound
        || p.block_scope_internet()
    {
        return false;
    }
    if env.is_connectivity_domain(&entity.domain) {
        conn.accept(
            "special grant for connectivity domain during network bootstrap",
            NO_REASON_OPTION_KEY,
        );
        return true;
    }
    false
}

pub fn check_connection_scope(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    // If we are handling a DNS request, check if we can immediately block it.
    if conn.conn_type == ConnectionType::Dns {
        // DNS is expected to resolve to LAN or Internet addresses.
        // Localhost queries are immediately responded to by the nameserver.
        if p.block_scope_internet() && p.block_scope_lan() {
            conn.block(
                "Internet and LAN access blocked",
                CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY,
            );
            return true;
        }
        return false;
    }

    let Some(entity) = &conn.entity else {
        return false;
    };
    // Check if the network scope is permitted.
    match entity.ip_scope {
        IpScope::Global | IpScope::GlobalMulticast => {
            if p.block_scope_internet() {
                conn.deny("Internet access blocked", CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY);
                return true;
            }
        }
        IpScope::SiteLocal | IpScope::LinkLocal | IpScope::LocalMulticast => {
            if p.block_scope_lan() {
                conn.block("LAN access blocked", CFG_OPTION_BLOCK_SCOPE_LAN_KEY);
                return true;
            }
        }
        IpScope::HostLocal => {
            if p.block_scope_local() {
                conn.block("Localhost access blocked", CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY);
                return true;
            }
        }
        // Block invalid and undefined IPs _after_ the rules.
        IpScope::Undefined | IpScope::Invalid => {}
    }
    false
}

/// Parses the endpoint rules a local profile sets for an option key.
///
/// The config of a profile is a nested map; a flat key is accepted too.
fn local_endpoints(p: &LayeredProfile, key: &str) -> Endpoints {
    let profile = p.local_profile();
    let config = &profile.config;
    let mut value: Option<&Value> = config.get(key);
    if value.is_none() {
        let mut parts = key.split('/');
        value = parts.next().and_then(|first| config.get(first));
        for part in parts {
            value = value.and_then(|v| v.get(part));
        }
    }
    let Some(Value::Array(items)) = value else {
        return Endpoints::default();
    };
    let list: Vec<&str> = items.iter().filter_map(Value::as_str).collect();
    parse_endpoints(&list).0
}

/// Special version of the endpoint check for DNS queries by the system
/// resolver: only the endpoint list of the local profile is used, the
/// resulting connection is blocked later when the original requesting process
/// is known.
fn check_endpoint_lists_for_system_resolver_dns(
    conn: &mut Connection,
    p: &LayeredProfile,
) -> bool {
    let option_key = if conn.inbound {
        CFG_OPTION_SERVICE_ENDPOINTS_KEY
    } else {
        CFG_OPTION_ENDPOINTS_KEY
    };
    let endpoints = local_endpoints(p, option_key);
    if !endpoints.is_set() {
        return false;
    }
    let Some(entity) = conn.entity.as_mut() else {
        return false;
    };
    let (result, reason) = endpoints.match_entity(entity);
    apply_endpoint_result(conn, result, reason, option_key)
}

fn apply_endpoint_result(
    conn: &mut Connection,
    result: EpResult,
    reason: Option<pm_endpoints::Reason>,
    option_key: &str,
) -> bool {
    let text = reason.as_ref().map(ToString::to_string).unwrap_or_default();
    let context = reason.as_ref().and_then(context_of);
    match result {
        EpResult::Denied | EpResult::MatchError => {
            let verdict = if conn.inbound {
                Verdict::Drop
            } else {
                Verdict::Block
            };
            conn.set_verdict(verdict, &text, option_key, context);
            true
        }
        EpResult::Permitted => {
            conn.set_verdict(Verdict::Accept, &text, option_key, context);
            true
        }
        EpResult::NoMatch => false,
    }
}

pub fn check_endpoint_lists(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    // DNS requests from the system resolver require a special decision process,
    // because the original requesting process is not known.
    if conn.conn_type == ConnectionType::Dns && conn.process.as_deref().is_some_and(is_system_resolver)
    {
        return check_endpoint_lists_for_system_resolver_dns(conn, p);
    }

    let inbound = conn.inbound;
    let Some(entity) = conn.entity.as_mut() else {
        return false;
    };
    let (result, reason, option_key) = if inbound {
        let (result, reason) = p.match_service_endpoint(entity);
        (result, reason, CFG_OPTION_SERVICE_ENDPOINTS_KEY)
    } else {
        let (result, reason) = p.match_endpoint(entity);
        (result, reason, CFG_OPTION_ENDPOINTS_KEY)
    };
    apply_endpoint_result(conn, result, reason, option_key)
}

pub fn check_invalid_ip(
    conn: &mut Connection,
    _p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    // Only applies to IP connections.
    if conn.conn_type != ConnectionType::Ip {
        return false;
    }
    // Block invalid and undefined IPs.
    if matches!(
        conn.entity.as_ref().map(|e| e.ip_scope),
        Some(IpScope::Undefined | IpScope::Invalid)
    ) {
        conn.deny("invalid IP", NO_REASON_OPTION_KEY);
        return true;
    }
    false
}

pub fn check_resolver_scope(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    // Only applies to IP connections whose IP address was resolved.
    if conn.conn_type != ConnectionType::Ip || !p.remove_out_of_scope_dns() {
        return false;
    }
    let (Some(resolver), Some(entity)) = (&conn.resolver, &conn.entity) else {
        return false;
    };

    if resolver.ip_scope.is_global()
        && (entity.ip_scope.is_lan() || entity.ip_scope.is_localhost())
    {
        // Block global resolvers from returning LAN/Localhost IPs.
        conn.block(
            "DNS server horizon violation: global DNS server returned local IP address",
            CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY,
        );
        return true;
    }
    if resolver.ip_scope.is_lan() && entity.ip_scope.is_localhost() {
        // Block LAN resolvers from returning Localhost IPs.
        conn.block(
            "DNS server horizon violation: LAN DNS server returned localhost IP address",
            CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY,
        );
        return true;
    }
    false
}

pub fn check_bypass_prevention(
    conn: &mut Connection,
    p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    if !p.prevent_bypassing() {
        return false;
    }
    let (result, reason, _responder): (_, _, Option<BypassResponder>) =
        prevent_bypassing(conn, env);
    let text = format!("bypass prevention: {reason}");
    match result {
        // Also block on MatchError to be on the safe side.
        EpResult::Denied | EpResult::MatchError => {
            conn.block(&text, CFG_OPTION_PREVENT_BYPASSING_KEY);
            true
        }
        EpResult::Permitted => {
            conn.accept(&text, CFG_OPTION_PREVENT_BYPASSING_KEY);
            true
        }
        EpResult::NoMatch => false,
    }
}

pub fn check_filter_lists(
    conn: &mut Connection,
    p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    let Some(entity) = conn.entity.as_mut() else {
        return false;
    };
    let (result, reason) = p.match_filter_lists(entity);
    match result {
        EpResult::Denied => {
            // If the connection matches a filter list, check if the "unbreak"
            // list matches too and abort blocking.
            let unbreak = env.unbreak_filter_list_ids();
            if entity
                .blocked_by_lists
                .iter()
                .any(|blocked| unbreak.contains(blocked))
            {
                tracing::debug!("filter: unbreak filter matched, ignoring other filter list matches");
                return false;
            }
            let Some(reason) = reason else {
                return false;
            };
            let text = reason
                .0
                .iter()
                .map(ToString::to_string)
                .collect::<Vec<_>>()
                .join(" and ");
            conn.set_verdict(
                if conn.inbound {
                    Verdict::Drop
                } else {
                    Verdict::Block
                },
                &text,
                CFG_OPTION_FILTER_LISTS_KEY,
                Some(reason.to_json()),
            );
            true
        }
        EpResult::NoMatch => false,
        other => {
            tracing::debug!("filter: filter lists returned unsupported verdict: {other}");
            false
        }
    }
}

pub fn check_custom_filter_list(
    conn: &mut Connection,
    p: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> bool {
    let custom = env.custom_lists();
    if !custom.is_loaded() {
        return false;
    }
    let Some(entity) = &conn.entity else {
        return false;
    };
    let deny_key = CFG_OPTION_CUSTOM_LIST_FILE_KEY;

    // Block if the domain name appears in the custom filter list (check for
    // subdomains if enabled).
    if !entity.domain.is_empty() {
        let (found, matched) = custom.lookup_domain(&entity.domain, p.filter_sub_domains());
        if found {
            let reason = format!(
                "domain {} matches {matched} in custom filter list",
                entity.domain
            );
            conn.deny(&reason, deny_key);
            return true;
        }
    }

    // Block if any of the CNAMEs appears in the custom filter list.
    if p.filter_cnames() {
        for cname in &entity.cname {
            let (found, matched) = custom.lookup_domain(cname, p.filter_sub_domains());
            if found {
                let reason = format!(
                    "domain alias (CNAME) {cname} matches {matched} in custom filter list"
                );
                conn.deny(&reason, deny_key);
                return true;
            }
        }
    }

    if entity.ip.is_some_and(|ip| custom.lookup_ip(ip)) {
        conn.deny("IP address is in the custom filter list", deny_key);
        return true;
    }
    if entity.asn != 0 && custom.lookup_asn(entity.asn) {
        conn.deny("AS is in the custom filter list", deny_key);
        return true;
    }
    if !entity.country.is_empty() && custom.lookup_country(&entity.country) {
        conn.deny("country is in the custom filter list", deny_key);
        return true;
    }
    false
}

pub fn check_domain_heuristics(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    let Some(entity) = &conn.entity else {
        return false;
    };
    // Don't check domain heuristics if no domain is available or they are off.
    if entity.domain.is_empty() || !p.domain_heuristics() {
        return false;
    }

    let trimmed = entity.domain.trim_end_matches('.');
    // Don't run the check if the domain is a TLD.
    let Some(etld1) = psl::domain_str(trimmed) else {
        return false;
    };

    let domain_to_check = etld1.split('.').next().unwrap_or_default();
    let score = dga::lms_score(domain_to_check);
    if score < 5.0 {
        tracing::debug!(
            "filter: possible data tunnel by {conn} in eTLD+1 {etld1}: {domain_to_check} has an lms score of {score:.2}"
        );
        conn.block(
            "possible DGA domain commonly used by malware",
            CFG_OPTION_DOMAIN_HEURISTICS_KEY,
        );
        return true;
    }

    // 100 is a somewhat arbitrary threshold to ensure we don't mess around
    // with CDN domain names too early. They use short second-level domains
    // that would trigger LMS checks but are too small to actually exfiltrate
    // data.
    if entity.domain.len() > etld1.len() + 100 {
        // Go slices the first len(eTLD+1) bytes of the domain here.
        let Some(prefix) = trimmed.get(..etld1.len()) else {
            return false;
        };
        let score = dga::lms_score_of_domain(prefix);
        if score < 10.0 {
            tracing::debug!(
                "filter: possible data tunnel by {conn} in subdomain of {}: {prefix} has an lms score of {score:.2}",
                entity.domain
            );
            conn.block(
                "possible data tunnel for covert communication and protection bypassing",
                CFG_OPTION_DOMAIN_HEURISTICS_KEY,
            );
            return true;
        }
    }
    false
}

pub fn check_auto_permit_related(
    conn: &mut Connection,
    p: &LayeredProfile,
    _env: &dyn FirewallEnv,
) -> bool {
    // Auto permit is disabled for default action permit or by option.
    if p.default_action() == DEFAULT_ACTION_PERMIT || p.disable_auto_permit() {
        return false;
    }
    if let Some(reason) = check_relation(conn) {
        conn.accept(&reason, CFG_OPTION_DISABLE_AUTO_PERMIT_KEY);
        return true;
    }
    false
}

/// Tries to find a relation between a process and a communication. This is for
/// a better out of the box experience and is _not_ meant to thwart intentional
/// malware.
fn check_relation(conn: &Connection) -> Option<String> {
    let entity = conn.entity.as_ref()?;
    // Don't check relation if no domain is available.
    if entity.domain.is_empty() {
        return None;
    }
    let process = conn.process.as_deref()?;
    // Don't check for unknown processes.
    if process.pid < 0 {
        return None;
    }

    let mut path_elements: Vec<&str> = process.path.split(std::path::MAIN_SEPARATOR).collect();
    // Only look at the last two path segments.
    if path_elements.len() > 2 {
        path_elements = path_elements.split_off(path_elements.len() - 2);
    }

    for domain_element in entity.domain.split('.') {
        let related = path_elements
            .iter()
            .copied()
            .chain([process.name.as_str(), process.exec_name.as_str()])
            .find(|element| levenshtein::match_score(domain_element, element) > 0.5);
        if let Some(process_element) = related {
            return Some(format!(
                "auto allowed: domain is related to process: {domain_element} is related to {process_element}"
            ));
        }
    }
    None
}
