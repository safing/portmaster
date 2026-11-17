//! Tests for the online status checks (Go `netenv/online-status.go`).

use std::net::IpAddr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_netenv::{
    check_online_status, dynamic_check_delay, is_connectivity_domain, AddressCache, DnsCheckOutcome,
    FlagPair, InterfaceSource, OnlineCheckEnv, OnlineMonitor, OnlineStatus, PortalProbe,
    PortalProbeResult, RawInterface, RouteCandidate, StaticDnsCheck, StaticPortalProbe,
    CONNECTIVITY_DOMAINS, DNS_TEST_DOMAIN, DNS_TEST_EXPECTED_IP,
};
use tokio_util::sync::CancellationToken;

struct FakeSource {
    interfaces: Mutex<Vec<RawInterface>>,
    calls: AtomicUsize,
}

impl FakeSource {
    fn new(addrs: &[(&str, u8)]) -> Arc<FakeSource> {
        Arc::new(FakeSource {
            interfaces: Mutex::new(vec![RawInterface {
                name: "eth0".to_string(),
                index: 1,
                mac: None,
                up: true,
                loopback: false,
                addrs: addrs
                    .iter()
                    .map(|(ip, prefix)| (ip.parse().unwrap(), *prefix))
                    .collect(),
            }]),
            calls: AtomicUsize::new(0),
        })
    }
}

impl InterfaceSource for FakeSource {
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        Ok(self.interfaces.lock().expect("lock").clone())
    }

    fn is_physical(&self, _name: &str) -> bool {
        true
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        Ok(Vec::new())
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        Vec::new()
    }
}

fn address_cache(addrs: &[(&str, u8)]) -> (Arc<FakeSource>, AddressCache) {
    let source = FakeSource::new(addrs);
    let flags = FlagPair::new();
    let cache = AddressCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    );
    (source, cache)
}

fn probe() -> StaticPortalProbe {
    StaticPortalProbe::new(PortalProbeResult::Error("timeout".to_string()))
}

#[test]
fn spn_and_dns_shortcut_the_check() {
    let (_, addresses) = address_cache(&[]);

    let env = OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: true,
        connected_to_dns: false,
    };
    let result = check_online_status(&env);
    assert_eq!(result.status, OnlineStatus::Online);
    assert_eq!(result.comment, "connected to SPN");

    let env = OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: true,
    };
    assert_eq!(check_online_status(&env).comment, "connected to DNS");
}

#[test]
fn global_address_means_online_without_probes() {
    let (_, addresses) = address_cache(&[("1.1.1.5", 24)]);
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Online);
    assert_eq!(result.comment, "global IPv4 interface detected");
}

#[test]
fn no_lan_mean_offline() {
    let (_, addresses) = address_cache(&[("127.0.0.1", 8), ("169.254.1.1", 16)]);
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Offline);
    assert_eq!(result.comment, "no local or global interfaces detected");
}

#[test]
fn ignored_android_ip_does_not_count() {
    let (_, addresses) = address_cache(&[("100.127.247.245", 8)]);
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Offline);
}

#[test]
fn a_redirect_means_captive_portal() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);
    let portal = StaticPortalProbe::new(PortalProbeResult::Redirect(
        "http://portal.example/login".to_string(),
    ));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &portal,
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Portal);
    assert_eq!(
        result.portal_url.as_deref(),
        Some("http://portal.example/login")
    );
    assert_eq!(
        result.comment,
        "portal test request succeeded with redirect"
    );
}

#[test]
fn a_200_response_means_captive_portal_with_special_domain() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);
    let portal = StaticPortalProbe::new(PortalProbeResult::Ok);
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &portal,
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Portal);
    assert_eq!(
        result.portal_url.as_deref(),
        Some("http://captiveportal.invalid./")
    );
    assert_eq!(result.comment, "portal test request succeeded");
}

#[test]
fn failed_portal_test_falls_through_to_dns() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);

    // No DNS checker configured: online.
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Online);
    assert_eq!(
        result.comment,
        "all checks passed, dns query check disabled"
    );

    let expected = StaticDnsCheck::new(DnsCheckOutcome::Answered(vec![IpAddr::V4(
        DNS_TEST_EXPECTED_IP,
    )]));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: Some(&expected),
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Online);
    assert_eq!(result.comment, "all checks passed");
}

#[test]
fn dns_check_variants_match_go() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);

    let mismatch = StaticDnsCheck::new(DnsCheckOutcome::Answered(vec![
        "1.1.1.1".parse().unwrap(),
    ]));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: Some(&mismatch),
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(
        result.comment,
        "all checks passed, dns query check response mismatched"
    );

    let empty = StaticDnsCheck::new(DnsCheckOutcome::Answered(Vec::new()));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: Some(&empty),
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(
        result.comment,
        "all checks passed, dns query check response was empty"
    );

    let acceptable = StaticDnsCheck::new(DnsCheckOutcome::AnsweredWithError(
        "server failure".to_string(),
    ));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: Some(&acceptable),
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(
        result.comment,
        "all checks passed, acceptable result for dns query check: server failure"
    );

    let failed = StaticDnsCheck::new(DnsCheckOutcome::Failed("timeout".to_string()));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &probe(),
        dns: Some(&failed),
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Offline);
    assert_eq!(result.comment, "dns query check failed");
}

#[test]
fn other_portal_response_continues_the_chain() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);
    let portal = StaticPortalProbe::new(PortalProbeResult::Other(404));
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &portal,
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });
    assert_eq!(result.status, OnlineStatus::Online);
    assert_eq!(
        result.comment,
        "all checks passed, dns query check disabled"
    );
}

#[test]
fn connectivity_domains_match_go() {
    for domain in CONNECTIVITY_DOMAINS {
        assert!(is_connectivity_domain(domain, ""), "{domain}");
    }
    assert!(is_connectivity_domain("captive.apple.com.", ""));
    // The special captive portal domain counts even without a detected portal.
    assert!(is_connectivity_domain(pm_netenv::SPECIAL_CAPTIVE_PORTAL_DOMAIN, ""));
    assert!(is_connectivity_domain(
        "portal.example.",
        "portal.example."
    ));
    assert!(!is_connectivity_domain("", "portal.example."));
    assert!(!is_connectivity_domain("example.com.", "portal.example."));
    assert_eq!(DNS_TEST_DOMAIN, "online-check.safing.io.");
}

#[test]
fn applying_a_check_updates_the_state() {
    let netenv = pm_netenv::NetEnv::new();
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);
    let result = check_online_status(&OnlineCheckEnv {
        addresses: &addresses,
        portal: &StaticPortalProbe::new(PortalProbeResult::Ok),
        dns: None,
        connected_to_spn: false,
        connected_to_dns: false,
    });

    assert!(netenv.apply_online_check(&result));
    assert_eq!(netenv.online_status(), OnlineStatus::Portal);
    let portal = netenv.captive_portal().expect("portal");
    assert_eq!(portal.url, "http://captiveportal.invalid./");
}

#[test]
fn dynamic_delays_match_go() {
    assert_eq!(
        dynamic_check_delay(OnlineStatus::Offline),
        Some(Duration::from_secs(10))
    );
    assert_eq!(
        dynamic_check_delay(OnlineStatus::Limited),
        Some(Duration::from_secs(5))
    );
    assert_eq!(
        dynamic_check_delay(OnlineStatus::Portal),
        Some(Duration::from_secs(5))
    );
    assert_eq!(
        dynamic_check_delay(OnlineStatus::SemiOnline),
        Some(Duration::from_secs(60))
    );
    assert_eq!(dynamic_check_delay(OnlineStatus::Online), None);
    assert_eq!(
        dynamic_check_delay(OnlineStatus::Unknown),
        Some(Duration::from_secs(300))
    );
}

struct ChangingProbe {
    calls: AtomicUsize,
}

impl PortalProbe for ChangingProbe {
    fn probe(&self) -> PortalProbeResult {
        if self.calls.fetch_add(1, Ordering::SeqCst) == 0 {
            PortalProbeResult::Error("timeout".to_string())
        } else {
            PortalProbeResult::Ok
        }
    }
}

#[tokio::test]
async fn monitor_checks_on_start_and_on_trigger() {
    let (_, addresses) = address_cache(&[("192.168.1.10", 24)]);
    let probe = Arc::new(ChangingProbe {
        calls: AtomicUsize::new(0),
    });
    let monitor = Arc::new(OnlineMonitor::new(
        Arc::new(pm_netenv::NetEnv::new()),
        Arc::new(addresses),
        probe,
        None,
    ));
    let mut events = monitor
        .netenv()
        .online_status_events()
        .subscribe("test", 8);

    let token = CancellationToken::new();
    let runner = {
        let monitor = Arc::clone(&monitor);
        let token = token.clone();
        tokio::spawn(async move { monitor.run(token).await })
    };

    // The first check runs immediately: no portal, no DNS check -> online.
    let first = tokio::time::timeout(Duration::from_secs(2), events.recv())
        .await
        .expect("first check")
        .expect("event");
    assert_eq!(first, OnlineStatus::Online);

    // A manual trigger runs the second check, which detects a portal.
    monitor.trigger();
    let second = tokio::time::timeout(Duration::from_secs(2), events.recv())
        .await
        .expect("second check")
        .expect("event");
    assert_eq!(second, OnlineStatus::Portal);
    assert!(monitor.netenv().captive_portal().is_some());

    token.cancel();
    runner.await.expect("monitor task");
}
