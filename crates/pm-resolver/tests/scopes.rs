//! Tests of the resolver list building and the scope selection.

use std::net::IpAddr;
use std::sync::Arc;

use hickory_proto::rr::RecordType;
use pm_database::{Databases, MemoryStorage};
use pm_resolver::namerecord::{record_interface_options, NameRecordStore};
use pm_resolver::service::MissingResolversState;
use pm_resolver::{Nameserver, Query, ResolverService, StaticEnvironment, StaticSettings};

struct Setup {
    service: ResolverService,
    settings: Arc<StaticSettings>,
    env: Arc<StaticEnvironment>,
}

fn setup(nameservers: &[&str], system: &[(&str, &[&str])]) -> Setup {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .unwrap();
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
        .unwrap();
    let store = NameRecordStore::new(databases.interface(record_interface_options()));

    let settings = Arc::new(StaticSettings::default());
    settings.update(|s| s.nameservers = nameservers.iter().map(|n| (*n).to_string()).collect());
    let env = Arc::new(StaticEnvironment::default());
    env.update(|s| {
        s.nameservers = system
            .iter()
            .map(|(ip, search)| Nameserver {
                ip: ip.parse::<IpAddr>().unwrap(),
                search: search.iter().map(|d| (*d).to_string()).collect(),
            })
            .collect();
        s.connectivity_domains = vec!["neverssl.com.".to_string()];
    });

    let service = ResolverService::new(settings.clone(), env.clone(), store);
    service.load_resolvers();
    Setup {
        service,
        settings,
        env,
    }
}

fn ids(resolvers: &[Arc<pm_resolver::model::Resolver>]) -> Vec<String> {
    resolvers.iter().map(|r| r.info.id()).collect()
}

const G1: &str = "dns://8.8.8.8:53?name=g1";
const DOT: &str = "dot://dns.example.net?ip=9.9.9.9&name=dot";
const SEARCH_ONLY: &str = "dns://192.168.50.1:53?search=corp.acme.org&search-only";
const SEARCH: &str = "dns://192.168.60.1:53?search=lan.acme.org,acme.org";

fn select(s: &Setup, fqdn: &str, qtype: RecordType) -> (Vec<String>, &'static str, bool) {
    let (selected, source, try_all) = s.service.get_resolvers_in_scope(&Query::new(fqdn, qtype));
    (ids(&selected), source, try_all)
}

fn sel(s: &Setup, fqdn: &str) -> (Vec<String>, &'static str, bool) {
    select(s, fqdn, RecordType::A)
}

fn strs(list: &[&str]) -> Vec<String> {
    list.iter().map(|s| (*s).to_string()).collect()
}

#[test]
fn resolvers_are_built_from_config_and_system() {
    let s = setup(
        &[G1, DOT],
        &[("192.168.1.1", &["home.acme.org"]), ("8.8.8.8", &[])],
    );
    assert_eq!(
        ids(&s.service.global_resolvers()),
        strs(&[
            "dns://8.8.8.8:53#config",
            "dot://dns.example.net:853#config",
            "dns://192.168.1.1:53#system",
            // Same IP as a configured one, but a different source: kept.
            "dns://8.8.8.8:53#system",
        ])
    );
    assert_eq!(
        ids(&s.service.local_resolvers()),
        strs(&["dns://192.168.1.1:53#system"])
    );
    assert_eq!(
        ids(&s.service.assigned_resolvers()),
        strs(&["dns://192.168.1.1:53#system", "dns://8.8.8.8:53#system"])
    );
    // Search domains of system resolvers are only configured for LAN servers.
    let global = s.service.global_resolvers();
    assert_eq!(
        global[2].search.as_deref(),
        Some(&[".home.acme.org.".to_string()][..])
    );
    assert_eq!(global[3].search, None);
    assert_eq!(
        s.service.missing_resolvers_state(),
        MissingResolversState::None
    );
    assert!(!s.service.is_disabled());
}

#[test]
fn localhost_system_resolvers_are_skipped_but_configured_ones_are_kept() {
    let s = setup(&["dns://127.0.0.1:5353"], &[("127.0.0.53", &[])]);
    assert_eq!(
        ids(&s.service.global_resolvers()),
        strs(&["dns://127.0.0.1:5353#config"])
    );
}

#[test]
fn defaults_are_used_when_nothing_valid_is_configured() {
    let s = setup(&["ftp://nope", "dns://not-an-ip"], &[]);
    assert_eq!(
        s.service.missing_resolvers_state(),
        MissingResolversState::UsingDefaults
    );
    assert_eq!(
        ids(&s.service.global_resolvers()),
        strs(&[
            "dot://cloudflare-dns.com:853#config",
            "dot://cloudflare-dns.com:853#config",
        ])
    );

    // No servers configured at all: the resolver is disabled, defaults still load.
    let s = setup(&[], &[]);
    assert!(s.service.is_disabled());
    assert_eq!(s.service.global_resolvers().len(), 2);

    // The system servers are used when nothing is configured.
    let s = setup(&[], &[("192.168.1.1", &[])]);
    assert_eq!(
        ids(&s.service.global_resolvers()),
        strs(&["dns://192.168.1.1:53#system"])
    );
    assert_eq!(
        s.service.missing_resolvers_state(),
        MissingResolversState::None
    );
}

#[test]
fn search_scopes_are_sorted_longest_first() {
    let s = setup(&[G1, SEARCH, SEARCH_ONLY], &[]);
    let scopes: Vec<(String, Vec<String>)> = s
        .service
        .scopes()
        .into_iter()
        .map(|scope| (scope.domain, ids(&scope.resolvers)))
        .collect();
    assert_eq!(
        scopes,
        vec![
            (
                ".corp.acme.org.".to_string(),
                strs(&["dns://192.168.50.1:53#config"])
            ),
            (
                ".lan.acme.org.".to_string(),
                strs(&["dns://192.168.60.1:53#config"])
            ),
            (
                ".acme.org.".to_string(),
                strs(&["dns://192.168.60.1:53#config"])
            ),
        ]
    );
}

const G1_ID: &str = "dns://8.8.8.8:53#config";
const DOT_ID: &str = "dot://dns.example.net:853#config";
const SEARCH_ID: &str = "dns://192.168.60.1:53#config";
const SEARCH_ONLY_ID: &str = "dns://192.168.50.1:53#config";
const SYS1_ID: &str = "dns://192.168.1.1:53#system";
const SYS2_ID: &str = "dns://10.0.0.1:53#system";

fn table_setup() -> Setup {
    let s = setup(
        &[G1, DOT, SEARCH, SEARCH_ONLY],
        &[("192.168.1.1", &["home.acme.org"]), ("10.0.0.1", &[])],
    );
    // Allow special-service domains so that they reach the scope selection.
    s.settings
        .update(|v| v.dont_resolve_special_domains = false);
    s
}

#[test]
fn scope_selection_table() {
    let s = table_setup();
    let a = RecordType::A;

    // (fqdn, expected resolvers, source, try_all)
    let cases: Vec<(&str, Vec<&str>, &str, bool)> = vec![
        // Internal special-use domain: the environment resolver.
        (
            "router.local.portmaster.home.arpa.",
            vec!["env"],
            "env",
            false,
        ),
        // Connectivity domain: system resolvers only.
        ("neverssl.com.", vec![SYS1_ID, SYS2_ID], "system", false),
        // Global domain: all resolvers in order; the search-only one is out of scope.
        (
            "www.other.net.",
            vec![G1_ID, DOT_ID, SEARCH_ID, SYS1_ID, SYS2_ID],
            "config",
            false,
        ),
        // Search scopes first, then global; duplicates are removed.
        (
            "host.lan.acme.org.",
            vec![SEARCH_ID, G1_ID, DOT_ID, SYS1_ID, SYS2_ID],
            "config",
            false,
        ),
        // The search-only resolver is in scope for its own domain.
        (
            "host.corp.acme.org.",
            vec![SEARCH_ONLY_ID, SEARCH_ID, G1_ID, DOT_ID, SYS1_ID, SYS2_ID],
            "config",
            false,
        ),
        // Search domains of system resolvers.
        (
            "x.home.acme.org.",
            vec![SYS1_ID, SEARCH_ID, G1_ID, DOT_ID, SYS2_ID],
            "config",
            false,
        ),
        // Multicast: local, system, then mDNS; try all.
        (
            "printer.local.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID, "mdns"],
            "mdns",
            true,
        ),
        (
            "1.0.254.169.in-addr.arpa.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID, "mdns"],
            "mdns",
            true,
        ),
        // Special-use and special-service domains: local resolvers only; try all.
        (
            "nas.home.arpa.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID],
            "special",
            true,
        ),
        (
            "5.1.168.192.in-addr.arpa.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID],
            "special",
            true,
        ),
        (
            "site.example.org.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID],
            "special",
            true,
        ),
        (
            "hidden.onion.",
            vec![SEARCH_ID, SYS1_ID, SYS2_ID],
            "special",
            true,
        ),
        // The root domain is a global domain.
        (
            ".",
            vec![G1_ID, DOT_ID, SEARCH_ID, SYS1_ID, SYS2_ID],
            "config",
            false,
        ),
    ];

    for (fqdn, want, source, try_all) in cases {
        let (got, got_source, got_try_all) = select(&s, fqdn, a);
        assert_eq!(got, strs(&want), "{fqdn}");
        assert_eq!(got_source, source, "{fqdn}");
        assert_eq!(got_try_all, try_all, "{fqdn}");
    }
}

#[test]
fn settings_filter_the_selection() {
    let s = table_setup();

    // No insecure protocols: only DoT is left.
    s.settings.update(|v| v.no_insecure_protocols = true);
    assert_eq!(sel(&s, "www.other.net.").0, strs(&[DOT_ID]));
    // The environment resolver is compliant.
    assert_eq!(sel(&s, "x.portmaster.home.arpa.").0, strs(&["env"]));
    s.settings.update(|v| v.no_insecure_protocols = false);

    // No assigned nameservers: system resolvers are removed everywhere.
    s.settings.update(|v| v.no_assigned_nameservers = true);
    assert_eq!(
        sel(&s, "www.other.net.").0,
        strs(&[G1_ID, DOT_ID, SEARCH_ID])
    );
    // Connectivity domains fall back to local and global resolvers.
    assert_eq!(
        sel(&s, "neverssl.com."),
        (strs(&[SEARCH_ID, G1_ID, DOT_ID]), "system", false)
    );
    s.settings.update(|v| v.no_assigned_nameservers = false);

    // No multicast DNS: mDNS is removed.
    s.settings.update(|v| v.no_multicast_dns = true);
    assert_eq!(
        sel(&s, "printer.local.").0,
        strs(&[SEARCH_ID, SYS1_ID, SYS2_ID])
    );
    s.settings.update(|v| v.no_multicast_dns = false);

    // The connectivity branch needs system resolvers; without them it is a global domain.
    let s = setup(&[G1], &[]);
    s.env
        .update(|e| e.connectivity_domains = vec!["neverssl.com.".to_string()]);
    assert_eq!(sel(&s, "neverssl.com."), (strs(&[G1_ID]), "config", false));
}

#[test]
fn resolvers_do_not_resolve_their_own_domain() {
    let s = setup(
        &[
            "dot://dns.provider.net?name=n",
            "dot://dns.other.org?ip=9.9.9.9&name=i",
        ],
        &[],
    );
    let no_ip = "dot://dns.provider.net:853#config";
    let with_ip = "dot://dns.other.org:853#config";

    assert_eq!(sel(&s, "www.other.net.").0, strs(&[no_ip, with_ip]));
    // Resolvers without IP are never used to resolve a resolver domain.
    assert_eq!(sel(&s, "dns.provider.net.").0, strs(&[with_ip]));
    assert_eq!(sel(&s, "dns.other.org.").0, strs(&[with_ip]));
}

#[test]
fn query_compliance_depends_on_settings() {
    let s = setup(&[G1], &[]);
    let hidden = Query::new("hidden.onion", RecordType::A);
    assert!(s.service.check_query_compliance(&hidden).is_err());
    s.settings
        .update(|v| v.dont_resolve_special_domains = false);
    assert!(s.service.check_query_compliance(&hidden).is_ok());
    assert!(s
        .service
        .check_query_compliance(&Query::new("x.localhost", RecordType::AAAA))
        .is_err());
}
