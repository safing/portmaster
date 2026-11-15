//! Domain scope tables of Go `scopes.go`, copied verbatim.

pub use crate::url::SPECIAL_SERVICE_DOMAINS;

/// Domain used by Portmaster for special addressing (Go `InternalSpecialUseDomain`).
pub const INTERNAL_SPECIAL_USE_DOMAIN: &str = "portmaster.home.arpa.";

/// Localhost domain: answered with 127.0.0.1 and ::1 (RFC 6761).
pub const LOCALHOST_DOMAIN: &str = ".localhost.";

/// Invalid domain: always NXDOMAIN (RFC 6761).
pub const INVALID_DOMAIN: &str = ".invalid.";

/// Internal special-use domains, sent to the environment resolver.
pub fn internal_special_use_domains() -> Vec<String> {
    vec![format!(".{INTERNAL_SPECIAL_USE_DOMAIN}")]
}

/// Multicast DNS domains (RFC 6762).
pub const MULTICAST_DOMAINS: &[&str] = &[
    ".local.",
    ".254.169.in-addr.arpa.",
    ".8.e.f.ip6.arpa.",
    ".9.e.f.ip6.arpa.",
    ".a.e.f.ip6.arpa.",
    ".b.e.f.ip6.arpa.",
];

/// Special-use domain names (IANA special-use domain names).
pub const SPECIAL_USE_DOMAINS: &[&str] = &[
    // RFC8375: Designated for non-unique use in residential home networks.
    ".home.arpa.",
    // RFC6762 (Appendix G): Non-official, but officially listed, private use domains.
    ".intranet.",
    ".internal.",
    ".private.",
    ".corp.",
    ".home.",
    ".lan.",
    // RFC6761: IPv4 private-address reverse-mapping domains.
    ".10.in-addr.arpa.",
    ".16.172.in-addr.arpa.",
    ".17.172.in-addr.arpa.",
    ".18.172.in-addr.arpa.",
    ".19.172.in-addr.arpa.",
    ".20.172.in-addr.arpa.",
    ".21.172.in-addr.arpa.",
    ".22.172.in-addr.arpa.",
    ".23.172.in-addr.arpa.",
    ".24.172.in-addr.arpa.",
    ".25.172.in-addr.arpa.",
    ".26.172.in-addr.arpa.",
    ".27.172.in-addr.arpa.",
    ".28.172.in-addr.arpa.",
    ".29.172.in-addr.arpa.",
    ".30.172.in-addr.arpa.",
    ".31.172.in-addr.arpa.",
    ".168.192.in-addr.arpa.",
    // RFC4193: IPv6 private-address reverse-mapping domains.
    ".d.f.ip6.arpa.",
    ".c.f.ip6.arpa.",
    // RFC6761: Special use domains for documentation and testing.
    ".example.",
    ".example.com.",
    ".example.net.",
    ".example.org.",
    ".test.",
];

/// Returns whether the dot-prefixed FQDN is in one of the scopes.
pub fn domain_in_scope<S: AsRef<str>>(dot_prefixed_fqdn: &str, scope_list: &[S]) -> bool {
    scope_list
        .iter()
        .any(|scope| dot_prefixed_fqdn.ends_with(scope.as_ref()))
}

/// Formats a scope list for messages, like Go `formatScopeList`.
pub fn format_scope_list(list: &[&str]) -> String {
    list.iter()
        .map(|domain| domain.trim_end_matches('.'))
        .collect::<Vec<_>>()
        .join(", ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scope_matching_is_suffix_based() {
        assert!(domain_in_scope(".printer.local.", MULTICAST_DOMAINS));
        assert!(!domain_in_scope(".notlocal.", MULTICAST_DOMAINS));
        assert!(domain_in_scope(".a.b.home.arpa.", SPECIAL_USE_DOMAINS));
        assert!(domain_in_scope(".example.onion.", SPECIAL_SERVICE_DOMAINS));
        assert!(!domain_in_scope(".example.com.", SPECIAL_SERVICE_DOMAINS));
        assert!(domain_in_scope(".", &["."]));
    }

    #[test]
    fn table_sizes_match_go() {
        assert_eq!(MULTICAST_DOMAINS.len(), 6);
        assert_eq!(SPECIAL_USE_DOMAINS.len(), 32);
        assert_eq!(SPECIAL_SERVICE_DOMAINS.len(), 39);
    }

    #[test]
    fn scope_list_formatting() {
        assert_eq!(format_scope_list(&[".a.", ".b."]), ".a, .b");
    }
}
