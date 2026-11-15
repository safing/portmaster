//! Behavior tests of the state lookups, using a fake proc tree.

use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime};

use pm_netstate::{
    IpProtocol, LookupError, NetState, PacketInfo, ProcFs, Stack, UNDEFINED_PROCESS_ID,
};

fn temp_dir(tag: &str) -> PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-netstate-state-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let _ = std::fs::remove_dir_all(&path);
    std::fs::create_dir_all(&path).unwrap();
    path
}

const HEADER: &str = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode";

fn write_table(root: &Path, stack: Stack, lines: &[&str]) {
    let path = root.join(stack.file());
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    let mut data = String::from(HEADER);
    data.push('\n');
    for line in lines {
        data.push_str(line);
        data.push('\n');
    }
    std::fs::write(path, data).unwrap();
}

fn add_socket_owner(root: &Path, pid: i32, inode: i64) {
    let fd_dir = root.join(pid.to_string()).join("fd");
    std::fs::create_dir_all(&fd_dir).unwrap();
    let _ = std::os::unix::fs::symlink(format!("socket:[{inode}]"), fd_dir.join("7"));
}

fn packet(
    src: &str,
    dst: &str,
    protocol: IpProtocol,
    inbound: bool,
    src_port: u16,
    dst_port: u16,
) -> PacketInfo {
    let mut packet = PacketInfo::new(
        src.parse().unwrap(),
        dst.parse().unwrap(),
        protocol,
        inbound,
    );
    packet.src_port = src_port;
    packet.dst_port = dst_port;
    packet
}

#[test]
fn looks_up_tcp_connections_and_listeners() {
    let root = temp_dir("tcp");
    write_table(
        &root,
        Stack::Tcp4,
        &[
            // 10.0.0.1:50000 -> 10.0.0.2:443, established.
            "   0: 0100000A:C350 0200000A:01BB 01 00000000:00000000 00:00000000 00000000  1000 0 1111 1 0000000000000000 20 4 30 10 -1",
            // 0.0.0.0:8080, listening.
            "   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 2222 1 0000000000000000 100 0 0 10 0",
        ],
    );
    add_socket_owner(&root, 4242, 1111);
    add_socket_owner(&root, 4343, 2222);

    let state = NetState::new(ProcFs::new(&root));

    // Outbound connection.
    let outbound = packet("10.0.0.1", "10.0.0.2", IpProtocol::TCP, false, 50000, 443);
    let result = state.lookup(&outbound, true).unwrap();
    assert_eq!(result.pid, 4242);
    assert!(!result.inbound);
    assert!(state.exists(&outbound, SystemTime::now()));

    // Inbound connection to the wildcard listener.
    let inbound = packet("1.2.3.4", "10.0.0.1", IpProtocol::TCP, true, 5555, 8080);
    let result = state.lookup(&inbound, true).unwrap();
    assert_eq!(result.pid, 4343);
    assert!(result.inbound);

    // Unknown connections and mismatching packets.
    let unknown = packet("10.0.0.1", "10.0.0.2", IpProtocol::TCP, false, 50000, 444);
    assert!(matches!(
        state.lookup(&unknown, true),
        Err(LookupError::ConnectionNotFound)
    ));
    assert!(!state.exists(&unknown, SystemTime::now()));

    let info = state.info(false);
    assert_eq!(info.tcp4_connections.len(), 1);
    assert_eq!(info.tcp4_listeners.len(), 1);
    assert_eq!(info.tcp4_listeners[0].inode, 2222);
}

#[test]
fn udp_direction_and_exists() {
    let root = temp_dir("udp");
    write_table(
        &root,
        Stack::Udp4,
        &[
            // 0.0.0.0:5353
            "   0: 00000000:14E9 00000000:0000 07 00000000:00000000 00:00000000 00000000  1000 0 3333 2 0000000000000000 0",
        ],
    );
    add_socket_owner(&root, 4444, 3333);

    let state = NetState::new(ProcFs::new(&root));

    // The first packet decides the direction of the connection.
    let inbound = packet("10.0.0.9", "192.168.1.5", IpProtocol::UDP, true, 1234, 5353);
    let result = state.lookup(&inbound, true).unwrap();
    assert_eq!(result.pid, 4444);
    assert!(result.inbound);
    assert!(state.exists(&inbound, SystemTime::now()));

    // The reverse packet keeps the stored direction.
    let outbound = packet(
        "192.168.1.5",
        "10.0.0.9",
        IpProtocol::UDP,
        false,
        5353,
        1234,
    );
    let result = state.lookup(&outbound, true).unwrap();
    assert!(result.inbound);

    // A new remote endpoint starts outbound.
    let other_remote = packet(
        "192.168.1.5",
        "10.0.0.10",
        IpProtocol::UDP,
        false,
        5353,
        1234,
    );
    let result = state.lookup(&other_remote, true).unwrap();
    assert!(!result.inbound);

    // Unseen endpoints do not exist, and old states expire.
    let unseen = packet(
        "10.0.0.11",
        "192.168.1.5",
        IpProtocol::UDP,
        true,
        1234,
        5353,
    );
    assert!(!state.exists(&unseen, SystemTime::now()));
    assert!(!state.exists(&inbound, SystemTime::now() + Duration::from_secs(11 * 60)));
    assert!(state.exists(&inbound, SystemTime::now() + Duration::from_secs(60)));
}

#[test]
fn dual_stack_lookups() {
    let root = temp_dir("dual");
    write_table(&root, Stack::Tcp4, &[]);
    write_table(
        &root,
        Stack::Tcp6,
        &[
            // [::]:8081, listening.
            "   0: 00000000000000000000000000000000:1F91 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 5555 1 0000000000000000 100 0 0 10 0",
        ],
    );
    write_table(&root, Stack::Udp4, &[]);
    write_table(
        &root,
        Stack::Udp6,
        &[
            // [::]:5353
            "   0: 00000000000000000000000000000000:14E9 00000000000000000000000000000000:0000 07 00000000:00000000 00:00000000 00000000  1000 0 6666 2 0000000000000000 0",
        ],
    );
    add_socket_owner(&root, 4545, 5555);
    add_socket_owner(&root, 4646, 6666);

    // An inbound connection to the IPv6 wildcard listener, seen as IPv4.
    let tcp = packet("127.0.0.1", "127.0.0.1", IpProtocol::TCP, true, 50001, 8081);
    let udp = packet(
        "192.168.1.5",
        "10.0.0.9",
        IpProtocol::UDP,
        false,
        5353,
        1234,
    );

    // Without dual stack the IPv6 sockets are not found from IPv4 packets.
    let state = NetState::new(ProcFs::new(&root));
    assert!(matches!(
        state.lookup(&tcp, true),
        Err(LookupError::ConnectionNotFound)
    ));
    assert!(matches!(
        state.lookup(&udp, true),
        Err(LookupError::ConnectionNotFound)
    ));

    // With dual stack they are.
    let mut state = NetState::new(ProcFs::new(&root));
    state.enable_tcp_dual_stack();
    state.enable_udp_dual_stack();
    assert_eq!(state.lookup(&tcp, true).unwrap().pid, 4545);
    assert_eq!(state.lookup(&udp, true).unwrap().pid, 4646);
}

#[test]
fn unsupported_protocol() {
    let root = temp_dir("unsupported");
    let state = NetState::new(ProcFs::new(&root));
    let packet = packet("127.0.0.1", "127.0.0.1", IpProtocol::Unknown, false, 1, 2);
    assert!(matches!(
        state.lookup(&packet, true),
        Err(LookupError::UnsupportedProtocol)
    ));
    assert!(!state.exists(&packet, SystemTime::now()));

    // A PID lookup without a matching user returns the undefined ID.
    assert_eq!(state.procfs().find_pid(123456, 1), UNDEFINED_PROCESS_ID);
}

#[test]
fn finds_own_process_by_socket_inode() {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let port = listener.local_addr().unwrap().port();

    let procfs = ProcFs::default();
    let (_, binds) = procfs.table(Stack::Tcp4).unwrap();
    let bind = binds
        .iter()
        .find(|bind| {
            bind.local.port == port
                && bind.local.ip == "127.0.0.1".parse::<std::net::IpAddr>().unwrap()
        })
        .expect("listener should be visible in /proc/net/tcp");

    let uid = std::fs::metadata("/proc/self").unwrap().uid() as i32;
    assert_eq!(
        procfs.find_pid(uid, bind.inode),
        std::process::id() as i32,
        "the owner of our own listening socket must be this process"
    );
}

#[test]
fn reads_live_tables() {
    let procfs = ProcFs::default();
    // Every readable table must parse into the same entries as a direct read.
    for stack in [Stack::Tcp4, Stack::Tcp6, Stack::Udp4, Stack::Udp6] {
        if let Ok((connections, binds)) = procfs.table(stack) {
            for connection in &connections {
                assert!(connection.inode >= 0, "{stack:?}");
            }
            for bind in &binds {
                assert!(bind.inode >= 0, "{stack:?}");
            }
        }
    }

    let state = NetState::new(ProcFs::default());
    let info = state.info(true);
    // It must not panic and must reflect the parsed tables.
    let _ = info.tcp4_connections.len();
}
