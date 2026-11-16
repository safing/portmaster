//! In-process UDP client/server tests.

mod common;

use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use common::*;
use hickory_proto::op::ResponseCode;
use hickory_proto::rr::RecordType;
use pm_nameserver::{
    start, ConflictingProcess, ListenerEvent, ListeningProcessLookup, StartOptions,
};
use tokio::net::UdpSocket;

const WAIT: Duration = Duration::from_secs(3);

fn payload(name: &str, qtype: RecordType) -> Vec<u8> {
    query_message(name, qtype).to_vec().unwrap()
}

#[tokio::test]
async fn single_listener_answers_over_udp() {
    let rig = rig(FakeResolver::answering());
    let running = start(rig.ns.clone(), "127.0.0.1:0", StartOptions::default())
        .await
        .unwrap();
    assert!(running.failures().is_empty());
    let addrs = running.local_addrs();
    assert_eq!(addrs.len(), 1);
    assert_eq!(addrs[0].ip(), IpAddr::V4(Ipv4Addr::LOCALHOST));

    let reply = exchange(addrs[0], &payload("example.com.", RecordType::A), WAIT)
        .await
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1))]);

    running.shutdown().await;
}

#[tokio::test]
async fn localhost_starts_two_listeners() {
    let rig = rig(FakeResolver::answering());
    let events = Arc::new(Mutex::new(Vec::new()));
    let sink = events.clone();
    let options = StartOptions {
        on_event: Some(Arc::new(move |event| sink.lock().unwrap().push(event))),
        ..StartOptions::default()
    };
    let running = start(rig.ns.clone(), "localhost:0", options).await.unwrap();
    let addrs = running.local_addrs();
    assert_eq!(addrs.len(), 2, "failures: {:?}", running.failures());
    assert_eq!(addrs[0].ip(), IpAddr::V4(Ipv4Addr::new(127, 0, 0, 17)));
    assert_eq!(addrs[1].ip(), IpAddr::V6(std::net::Ipv6Addr::LOCALHOST));

    for addr in &addrs {
        let reply = exchange(*addr, &payload("example.com.", RecordType::A), WAIT)
            .await
            .unwrap();
        assert_eq!(reply.metadata.response_code, ResponseCode::NoError, "{addr}");
    }

    let started: Vec<bool> = events
        .lock()
        .unwrap()
        .iter()
        .filter_map(|e| match e {
            ListenerEvent::Started { primary } => Some(*primary),
            _ => None,
        })
        .collect();
    assert_eq!(started.len(), 2);
    assert!(started.contains(&true) && started.contains(&false));

    let matcher = running.ip_matcher();
    assert!(matcher.matches_ip(IpAddr::V4(Ipv4Addr::new(127, 0, 0, 17))));
    assert!(matcher.matches_ip(IpAddr::V6(std::net::Ipv6Addr::LOCALHOST)));
    assert!(!matcher.matches_ip(IpAddr::V4(Ipv4Addr::LOCALHOST)));

    running.shutdown().await;
}

#[tokio::test]
async fn ipv6_listener_is_skipped_without_ipv6() {
    let env = FakeEnv {
        ipv6: false,
        ..FakeEnv::default()
    };
    let rig = rig_with(FakeResolver::answering(), env, FakeAttributor::new());
    let running = start(rig.ns.clone(), "localhost:0", StartOptions::default())
        .await
        .unwrap();
    assert_eq!(running.local_addrs().len(), 1);
    running.shutdown().await;
}

#[tokio::test]
async fn malformed_packets_do_not_kill_the_listener() {
    let rig = rig(FakeResolver::answering());
    let running = start(rig.ns.clone(), "127.0.0.1:0", StartOptions::default())
        .await
        .unwrap();
    let addr = running.local_addrs()[0];

    assert!(exchange(addr, &[0xde, 0xad], Duration::from_millis(300)).await.is_none());
    assert!(exchange(addr, &[], Duration::from_millis(300)).await.is_none());

    let reply = exchange(addr, &payload("example.com.", RecordType::A), WAIT)
        .await
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    running.shutdown().await;
}

#[tokio::test]
async fn blocked_replies_are_sent_over_udp() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.pre.lock().unwrap() = Some((pm_network::Verdict::Block, "no".into()));
    let running = start(rig.ns.clone(), "127.0.0.1:0", StartOptions::default())
        .await
        .unwrap();
    let addr = running.local_addrs()[0];
    let reply = exchange(addr, &payload("example.com.", RecordType::A), WAIT)
        .await
        .unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);

    // Dropped queries get no reply.
    *rig.hook.pre.lock().unwrap() = Some((pm_network::Verdict::Drop, "no".into()));
    assert!(exchange(addr, &payload("example.com.", RecordType::A), Duration::from_millis(500))
        .await
        .is_none());
    running.shutdown().await;
}

struct FakeLookup(Option<ConflictingProcess>);

impl ListeningProcessLookup for FakeLookup {
    fn listening_process(
        &self,
        _ip: IpAddr,
        _port: u16,
    ) -> Result<Option<ConflictingProcess>, String> {
        Ok(self.0.clone())
    }
}

#[tokio::test]
async fn bind_conflict_reports_the_conflicting_service() {
    let blocker = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let port = blocker.local_addr().unwrap().port();

    let rig = rig(FakeResolver::answering());
    let events = Arc::new(Mutex::new(Vec::new()));
    let sink = events.clone();
    let options = StartOptions {
        lookup: Some(Arc::new(FakeLookup(Some(ConflictingProcess {
            name: "dnsmasq".into(),
            exe: "/usr/sbin/dnsmasq".into(),
        })))),
        on_event: Some(Arc::new(move |event| sink.lock().unwrap().push(event))),
    };
    let running = start(rig.ns.clone(), &format!("127.0.0.1:{port}"), options)
        .await
        .unwrap();
    assert!(running.local_addrs().is_empty());
    let failures = running.failures();
    assert_eq!(failures.len(), 1);
    let failure = &failures[0];
    assert!(failure.primary);
    assert_eq!(failure.event_id(), "nameserver:conflicting-service");
    assert_eq!(failure.title(), "Conflicting DNS Software");
    assert_eq!(
        failure.message(),
        "Restart Portmaster after you have deactivated or properly configured the conflicting software: dnsmasq (/usr/sbin/dnsmasq)"
    );
    assert!(failure.is_conflict());
    assert!(matches!(
        events.lock().unwrap()[0],
        ListenerEvent::Failed(_)
    ));
    running.shutdown().await;
}

#[tokio::test]
async fn bind_failure_without_conflict_is_a_listener_failure() {
    let blocker = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let port = blocker.local_addr().unwrap().port();

    let rig = rig(FakeResolver::answering());
    let options = StartOptions {
        lookup: Some(Arc::new(FakeLookup(None))),
        on_event: None,
    };
    let running = start(rig.ns.clone(), &format!("127.0.0.1:{port}"), options)
        .await
        .unwrap();
    let failure = &running.failures()[0];
    assert_eq!(failure.event_id(), "nameserver:listener-failed");
    assert_eq!(failure.title(), "Secure DNS Error");
    assert!(failure
        .message()
        .starts_with("The internal DNS server failed. Restart Portmaster to try again. Error: "));
    assert!(!failure.is_conflict());
    running.shutdown().await;
}

#[tokio::test]
async fn secondary_listener_failure_uses_the_secondary_event_id() {
    // Occupy the IPv6 side of the same port on a different address family by
    // blocking the exact port of the primary: the secondary uses another port
    // only when port 0 is requested, so block a fixed pair instead.
    let primary = UdpSocket::bind("127.0.0.17:0").await.unwrap();
    let port = primary.local_addr().unwrap().port();
    let rig = rig(FakeResolver::answering());
    let running = start(rig.ns.clone(), &format!("localhost:{port}"), StartOptions::default())
        .await
        .unwrap();
    // The primary failed; the secondary may have started.
    assert_eq!(running.failures()[0].event_id(), "nameserver:listener-failed");
    for failure in running.failures() {
        assert!(failure.primary || failure.event_id().ends_with("-secondary"));
    }
    running.shutdown().await;
}

#[tokio::test]
async fn invalid_listen_address_is_an_error() {
    let rig = rig(FakeResolver::answering());
    assert!(start(rig.ns.clone(), "not-an-address", StartOptions::default())
        .await
        .is_err());
    assert!(start(rig.ns.clone(), "nothost:53", StartOptions::default())
        .await
        .is_err());
}

#[tokio::test]
async fn wildcard_listener_matches_local_addresses_only() {
    let env = FakeEnv {
        local: true,
        ..FakeEnv::default()
    };
    let rig = rig_with(FakeResolver::answering(), env, FakeAttributor::new());
    let running = start(rig.ns.clone(), "0.0.0.0:0", StartOptions::default())
        .await
        .unwrap();
    let matcher = running.ip_matcher();
    assert!(matcher.matches_ip(IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1))));
    let port = running.local_addrs()[0].port();
    assert!(matcher.matches(IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1)), port));
    assert!(!matcher.matches(IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1)), port.wrapping_add(1)));
    running.shutdown().await;

    let env = FakeEnv {
        local: false,
        ..FakeEnv::default()
    };
    let rig = rig_with(FakeResolver::answering(), env, FakeAttributor::new());
    let running = start(rig.ns.clone(), "0.0.0.0:0", StartOptions::default())
        .await
        .unwrap();
    assert!(!running.ip_matcher().matches_ip(IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1))));
    running.shutdown().await;
}

#[allow(dead_code)]
fn _types(_: SocketAddr) {}
