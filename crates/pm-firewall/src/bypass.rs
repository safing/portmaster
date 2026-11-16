//! Port of Go `service/firewall/bypassing.go`.

use pm_endpoints::EpResult;
use pm_network::{protocol, Connection, ConnectionType};

use crate::env::FirewallEnv;
use crate::is_system_resolver;

const RESOLVER_FILTER_LISTS: [&str; 1] = ["17-DNS"];

/// The DNS reply that belongs to a denied connection (Go `nsutil.Responder`).
///
/// Go keeps it in the reason context, where it serializes to `null`; the
/// connection model here cannot hold it, so it is returned separately.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BypassResponder {
    /// `nsutil.NxDomain()`
    NxDomain,
    /// `nsutil.BlockIP()`
    BlockIp,
}

/// Checks if the connection should be denied or permitted based on some
/// bypass protection checks.
pub fn prevent_bypassing(
    conn: &mut Connection,
    env: &dyn FirewallEnv,
) -> (EpResult, String, Option<BypassResponder>) {
    let no_match = (EpResult::NoMatch, String::new(), None);

    // Exclude incoming connections.
    if conn.inbound {
        return no_match;
    }
    let Some(entity) = conn.entity.as_mut() else {
        return no_match;
    };

    // Exclude ICMP.
    if entity.protocol == protocol::ICMP || entity.protocol == protocol::ICMPV6 {
        return no_match;
    }

    // Block firefox canary domain to disable DoH.
    if entity.domain.to_lowercase() == "use-application-dns.net." {
        return (
            EpResult::Denied,
            "blocked canary domain to prevent enabling of DNS-over-HTTPs".to_string(),
            Some(BypassResponder::NxDomain),
        );
    }

    let system_resolver = conn.process.as_deref().is_some_and(is_system_resolver);

    // Exclude DNS requests coming from the System Resolver.
    if conn.conn_type == ConnectionType::Dns && system_resolver {
        return no_match;
    }

    let resolver_disabled = env.resolver_disabled();

    // If the Portmaster resolver is disabled, allow requests going to the
    // system DNS resolver and all connections out of the System Resolver.
    if resolver_disabled {
        if system_resolver {
            return no_match;
        }
        if entity.port == 53 && entity.ip_scope.is_localhost() {
            return no_match;
        }
    }

    let port = entity.port;
    if looks_like_outgoing_dns_request(conn) && resolver_disabled {
        // Allow. The packet is analyzed and blocked if it is not a DNS
        // request, before it is sent.
        conn.inspecting = true;
        return no_match;
    }

    let Some(entity) = conn.entity.as_mut() else {
        return no_match;
    };
    if port == 53 {
        return (
            EpResult::Denied,
            "blocked DNS query, manual dns setup required".to_string(),
            Some(BypassResponder::BlockIp),
        );
    }
    // Port 853 is DNS over TLS; the other case is the secure DNS filter list.
    if port == 853 || entity.match_lists(&RESOLVER_FILTER_LISTS.map(String::from)) {
        env.report_secure_dns_bypass(conn.process.as_deref());
        return (
            EpResult::Denied,
            "blocked rogue connection to DNS resolver".to_string(),
            Some(BypassResponder::BlockIp),
        );
    }

    no_match
}

/// Outbound on remote port 53, UDP.
pub fn looks_like_outgoing_dns_request(conn: &Connection) -> bool {
    if conn.inbound {
        return false;
    }
    let Some(entity) = &conn.entity else {
        return false;
    };
    entity.port == 53 && conn.ip_protocol == protocol::UDP
}
