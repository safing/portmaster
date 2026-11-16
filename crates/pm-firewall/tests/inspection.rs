//! Tests of the traffic inspector registry, ported from the behavior of Go
//! `service/firewall/inspection/inspection.go`.

use std::net::IpAddr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};

use pm_firewall::inspection::{InspectorAction, InspectorRegistry};
use pm_firewall::packet::{IcmpType, QueuedPacket, VerdictError};
use pm_network::{Connection, PacketInfo, Verdict};

struct TestPacket {
    info: PacketInfo,
}

impl TestPacket {
    fn new() -> TestPacket {
        TestPacket {
            info: PacketInfo::new(
                6,
                "192.168.1.5".parse::<IpAddr>().unwrap(),
                43210,
                "93.184.216.34".parse::<IpAddr>().unwrap(),
                443,
                false,
            ),
        }
    }
}

impl QueuedPacket for TestPacket {
    fn info(&self) -> &PacketInfo {
        &self.info
    }
    fn verdict_set(&self) -> bool {
        false
    }
    fn payload(&self) -> Option<&[u8]> {
        None
    }
    fn raw(&self) -> &[u8] {
        &[]
    }
    fn icmp_type(&self) -> IcmpType {
        IcmpType::NoLayer
    }
    fn set_verdict(&self, _verdict: pm_interception::Verdict) -> Result<(), VerdictError> {
        Ok(())
    }
}

#[test]
fn actions_match_go_constants() {
    assert_eq!(InspectorAction::DO_NOTHING.as_u8(), 0);
    assert_eq!(InspectorAction::BLOCK_PACKET.as_u8(), 1);
    assert_eq!(InspectorAction::DROP_PACKET.as_u8(), 2);
    assert_eq!(InspectorAction::BLOCK_CONN.as_u8(), 3);
    assert_eq!(InspectorAction::DROP_CONN.as_u8(), 4);
    assert_eq!(InspectorAction::STOP_INSPECTING.as_u8(), 5);
    assert_eq!(InspectorAction::from_u8(5), Some(InspectorAction::StopInspecting));
    assert_eq!(InspectorAction::from_u8(6), None);
}

#[test]
fn empty_registry_returns_undecided() {
    let registry = InspectorRegistry::new();
    let mut conn = Connection::default();
    assert_eq!(registry.run_inspectors(&mut conn, &TestPacket::new()), (Verdict::Undecided, false));
    assert_eq!(conn.get_active_inspectors().len(), 0);
}

#[test]
fn inspectors_run_in_registration_order_and_verdicts_combine() {
    let registry = InspectorRegistry::new();
    let calls = Arc::new(Mutex::new(Vec::new()));

    let record = |name: &'static str, calls: Arc<Mutex<Vec<&'static str>>>, action| {
        move |_: &mut Connection, _: &dyn QueuedPacket| {
            calls.lock().unwrap().push(name);
            action
        }
    };

    assert_eq!(
        registry.register_inspector(
            "accept",
            record("accept", Arc::clone(&calls), InspectorAction::DoNothing),
            Verdict::Undecided,
        ),
        0
    );
    assert_eq!(
        registry.register_inspector(
            "block",
            record("block", Arc::clone(&calls), InspectorAction::BlockPacket),
            Verdict::Undecided,
        ),
        1
    );
    assert_eq!(
        registry.register_inspector(
            "drop",
            record("drop", Arc::clone(&calls), InspectorAction::DropPacket),
            Verdict::Undecided,
        ),
        2
    );
    assert_eq!(registry.names(), ["accept", "block", "drop"]);

    let mut conn = Connection::default();
    let (verdict, cont) = registry.run_inspectors(&mut conn, &TestPacket::new());
    assert_eq!(verdict, Verdict::Drop);
    assert!(cont);
    assert_eq!(*calls.lock().unwrap(), ["accept", "block", "drop"]);
    assert_eq!(conn.get_active_inspectors(), [false, false, false]);

    // Per-packet actions never deactivate an inspector.
    let (verdict, cont) = registry.run_inspectors(&mut conn, &TestPacket::new());
    assert_eq!(verdict, Verdict::Drop);
    assert!(cont);
    assert_eq!(calls.lock().unwrap().len(), 6);
}

#[test]
fn do_nothing_and_block_packet_pick_the_strongest_verdict() {
    let registry = InspectorRegistry::new();
    registry.register_inspector(
        "a",
        |_: &mut Connection, _: &dyn QueuedPacket| InspectorAction::DoNothing,
        Verdict::Undecided,
    );
    registry.register_inspector(
        "b",
        |_: &mut Connection, _: &dyn QueuedPacket| InspectorAction::BlockPacket,
        Verdict::Undecided,
    );

    let (verdict, cont) = registry.run_inspectors(&mut Connection::default(), &TestPacket::new());
    assert_eq!(verdict, Verdict::Block);
    assert!(cont);
}

#[test]
fn verdict_past_inspection_criteria_skips_inspector() {
    let registry = InspectorRegistry::new();
    let calls = Arc::new(AtomicUsize::new(0));
    let seen = Arc::clone(&calls);
    registry.register_inspector(
        "late",
        move |_: &mut Connection, _: &dyn QueuedPacket| {
            seen.fetch_add(1, Ordering::SeqCst);
            InspectorAction::DropPacket
        },
        Verdict::Accept,
    );

    let mut conn = Connection::default();
    conn.set_verdict(Verdict::Block, "", "", None);
    let (verdict, cont) = registry.run_inspectors(&mut conn, &TestPacket::new());
    assert_eq!(verdict, Verdict::Undecided);
    assert!(!cont);
    assert_eq!(calls.load(Ordering::SeqCst), 0);
    assert_eq!(conn.get_active_inspectors(), [true]);
}

#[test]
fn block_conn_and_drop_conn_are_terminal_for_the_connection() {
    for (action, expected) in [
        (InspectorAction::BlockConn, Verdict::Block),
        (InspectorAction::DropConn, Verdict::Drop),
    ] {
        let registry = InspectorRegistry::new();
        let calls = Arc::new(AtomicUsize::new(0));
        let seen = Arc::clone(&calls);
        registry.register_inspector(
            "terminal",
            move |_: &mut Connection, _: &dyn QueuedPacket| {
                seen.fetch_add(1, Ordering::SeqCst);
                action
            },
            Verdict::Undecided,
        );

        let mut conn = Connection::default();
        let (verdict, cont) = registry.run_inspectors(&mut conn, &TestPacket::new());
        assert_eq!(verdict, expected);
        assert!(!cont);
        assert_eq!(conn.verdict, expected);
        assert_eq!(conn.get_active_inspectors(), [true]);

        // The inspector is not called again for the same connection.
        registry.run_inspectors(&mut conn, &TestPacket::new());
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }
}

#[test]
fn stop_inspecting_deactivates_without_changing_verdict() {
    let registry = InspectorRegistry::new();
    registry.register_inspector(
        "stop",
        |_: &mut Connection, _: &dyn QueuedPacket| InspectorAction::StopInspecting,
        Verdict::Undecided,
    );

    let mut conn = Connection::default();
    let (verdict, cont) = registry.run_inspectors(&mut conn, &TestPacket::new());
    assert_eq!(verdict, Verdict::Undecided);
    assert!(!cont);
    assert_eq!(conn.get_active_inspectors(), [true]);
}

#[test]
fn inspector_data_is_shared_with_inspectors() {
    let registry = InspectorRegistry::new();
    registry.register_inspector(
        "data",
        |conn: &mut Connection, _: &dyn QueuedPacket| {
            conn.inspector_data.insert(7, Box::new(41_u8 + 1));
            InspectorAction::StopInspecting
        },
        Verdict::Undecided,
    );

    let mut conn = Connection::default();
    registry.run_inspectors(&mut conn, &TestPacket::new());
    let value = conn
        .get_inspector_data()
        .get(&7)
        .and_then(|v| v.downcast_ref::<u8>());
    assert_eq!(value, Some(&42));
}
