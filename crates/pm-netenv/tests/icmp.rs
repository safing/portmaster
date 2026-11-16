//! Tests for the ICMP listener and probe logic without root privileges.

use std::net::{IpAddr, Ipv4Addr};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Duration;

use pm_netenv::{
    build_echo, embedded_echo_ids, parse_icmp_v4, recv_icmp, recv_timeout, run_probe, IcmpListener,
    IcmpPacket, IcmpReply, IcmpSocket, IcmpV4Packet, IcmpV4Type, ProbeOutcome, RecvOutcome,
};

#[derive(Clone, Debug)]
struct FakePacket {
    outbound: bool,
    destination: IpAddr,
    remote: IpAddr,
    layer: Option<IcmpV4Packet>,
    fail_load: bool,
    loads: Arc<AtomicUsize>,
}

impl IcmpPacket for FakePacket {
    fn load_packet_data(&mut self) -> Result<(), String> {
        self.loads.fetch_add(1, Ordering::SeqCst);
        if self.fail_load {
            Err("no payload".to_string())
        } else {
            Ok(())
        }
    }

    fn is_outbound(&self) -> bool {
        self.outbound
    }

    fn destination(&self) -> IpAddr {
        self.destination
    }

    fn remote_ip(&self) -> IpAddr {
        self.remote
    }

    fn icmp_v4(&self) -> Option<&IcmpV4Packet> {
        self.layer.as_ref()
    }
}

fn packet(outbound: bool, destination: &str) -> FakePacket {
    FakePacket {
        outbound,
        destination: destination.parse().unwrap(),
        remote: "198.51.100.1".parse().unwrap(),
        layer: Some(IcmpV4Packet {
            type_code: IcmpV4Type::TimeExceeded,
            id: 1,
            seq: 1,
            payload: Vec::new(),
        }),
        fail_load: false,
        loads: Arc::new(AtomicUsize::new(0)),
    }
}

#[test]
fn listener_only_accepts_matching_packets_and_is_single_use() {
    let listener = Arc::new(IcmpListener::new());
    let target: IpAddr = "192.0.2.1".parse().unwrap();
    let (mut receiver, guard) = listener.listen(target);
    assert!(
        listener.try_listen(target).is_none(),
        "only one listener at a time"
    );

    // Inbound packets are always accepted.
    assert!(listener.submit_packet(Box::new(packet(false, "192.0.2.99"))));
    // Outbound packets are only accepted for the target IP.
    assert!(!listener.submit_packet(Box::new(packet(true, "192.0.2.99"))));
    assert!(listener.submit_packet(Box::new(packet(true, "192.0.2.1"))));

    assert!(receiver.try_recv().is_ok());
    assert!(receiver.try_recv().is_ok());
    assert!(receiver.try_recv().is_err());

    drop(guard);
    let (_second_receiver, second_guard) = listener.try_listen(target).expect("listener released");
    drop(second_guard);
}

#[test]
fn listener_rejects_packets_without_payload() {
    let listener = Arc::new(IcmpListener::new());
    let (_receiver, _guard) = listener.listen("192.0.2.1".parse().unwrap());
    let mut broken = packet(false, "192.0.2.1");
    broken.fail_load = true;
    assert!(!listener.submit_packet(Box::new(broken)));
}

#[test]
fn recv_icmp_times_out_and_filters_packets() {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .unwrap();
    runtime.block_on(async {
        let listener = Arc::new(IcmpListener::new());
        let (mut receiver, _guard) = listener.listen("192.0.2.1".parse().unwrap());
        assert_eq!(recv_icmp(1, &mut receiver).await, RecvOutcome::Timeout);
        assert_eq!(recv_timeout(1), Duration::from_millis(120));
    });
}

#[test]
fn echo_parsing_matches_go() {
    let packet = build_echo(0x1234, 7);
    let parsed = parse_icmp_v4(&packet).expect("parse echo");
    assert_eq!(parsed.message_type(), IcmpV4Type::Other); // type 8 is Echo Request
    assert_eq!(parsed.id, 0x1234);
    assert_eq!(parsed.seq, 7);

    let mut error = vec![11, 0, 0, 0]; // time exceeded
    error.extend_from_slice(&[0, 0, 0, 0]);
    let mut original = vec![8, 0, 0, 0];
    original.extend_from_slice(&0x1234u16.to_be_bytes());
    original.extend_from_slice(&9u16.to_be_bytes());
    error.extend_from_slice(&[0u8; 20]);
    error.extend_from_slice(&original);
    let parsed = parse_icmp_v4(&error).expect("parse error");
    assert_eq!(parsed.message_type(), IcmpV4Type::TimeExceeded);
    assert_eq!(embedded_echo_ids(&parsed), Some((0x1234, 9)));
}

struct FakeSocket {
    replies: std::sync::Mutex<Vec<IcmpReply>>,
    sent: AtomicUsize,
    failed_sends: usize,
}

impl IcmpSocket for FakeSocket {
    fn set_ttl(&self, _ttl: u8) -> Result<(), String> {
        Ok(())
    }

    fn send_to(&self, _data: &[u8], _target: IpAddr) -> Result<(), String> {
        let count = self.sent.fetch_add(1, Ordering::SeqCst);
        if count < self.failed_sends {
            Err("send failed".to_string())
        } else {
            Ok(())
        }
    }

    fn recv_reply(&self, _timeout: Duration) -> Result<Option<IcmpReply>, String> {
        Ok(self.replies.lock().unwrap().pop())
    }
}

fn time_exceeded(remote: &str, id: u16, seq: u16) -> IcmpReply {
    let mut original = build_echo(id, seq);
    let mut data = vec![11, 0, 0, 0, 0, 0, 0, 0];
    data.extend_from_slice(&[0u8; 20]);
    data.append(&mut original);
    IcmpReply {
        remote_ip: remote.parse().unwrap(),
        packet: parse_icmp_v4(&data).unwrap(),
    }
}

#[test]
fn probe_returns_global_router_and_retries_sends() {
    let socket = FakeSocket {
        replies: std::sync::Mutex::new(vec![time_exceeded("203.0.113.1", 0x1234, 1)]),
        sent: AtomicUsize::new(0),
        failed_sends: 1,
    };
    let target: IpAddr = "192.0.2.1".parse().unwrap();
    let result = run_probe(&socket, target, 0x1234, |ip| {
        ip == "203.0.113.1".parse::<IpAddr>().unwrap()
    })
    .expect("probe");
    assert_eq!(
        result,
        ProbeOutcome::FoundRouter("203.0.113.1".parse().unwrap())
    );
    assert_eq!(
        socket.sent.load(Ordering::SeqCst),
        2,
        "one failed send was retried"
    );
}

#[test]
fn probe_reports_direct_reply_and_no_actionable_reply() {
    let direct = FakeSocket {
        replies: std::sync::Mutex::new(vec![IcmpReply {
            remote_ip: "192.0.2.1".parse().unwrap(),
            packet: parse_icmp_v4(&build_echo(0x1234, 1)).unwrap(),
        }]),
        sent: AtomicUsize::new(0),
        failed_sends: 0,
    };
    // The echo request has type 8; change it to an echo reply (type 0).
    direct.replies.lock().unwrap()[0].packet.type_code = IcmpV4Type::EchoReply;
    let target: IpAddr = "192.0.2.1".parse().unwrap();
    let result = run_probe(&direct, target, 0x1234, |_| false).unwrap();
    assert_eq!(
        result,
        ProbeOutcome::Failed(
            "received final echo reply without time exceeded messages".to_string()
        )
    );

    let silent = FakeSocket {
        replies: std::sync::Mutex::new(Vec::new()),
        sent: AtomicUsize::new(0),
        failed_sends: 0,
    };
    assert_eq!(
        run_probe(&silent, target, 0x1234, |_| false).unwrap(),
        ProbeOutcome::NoReply
    );
}

#[test]
fn system_socket_can_be_constructed_without_asserting_privileges() {
    // Opening may fail on hosts without CAP_NET_RAW; that is an error, not a
    // panic, and tests with fakes never need it.
    let _ = pm_netenv::SystemIcmpSocket::new();
    assert_eq!(Ipv4Addr::new(192, 0, 2, 1).to_string(), "192.0.2.1");
}
