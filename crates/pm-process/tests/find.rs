//! Tests of connection attribution.

mod common;

use pm_netstate::{IpProtocol, NetState, PacketInfo, ProcFs};
use pm_process::{
    get_pid_of_connection, get_process_of_connection, UNIDENTIFIED_PROCESS_ID,
    UNSOLICITED_PROCESS_ID,
};

#[test]
fn attributes_connections_to_processes() {
    let root = common::temp_dir("find");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 4242, 1, 4242, 500, "server", "/usr/bin/server");
    let passwd = common::write_passwd(&root);
    let uid = common::current_uid();

    // 10.0.0.1:50000 -> 10.0.0.2:443, owned by PID 4242.
    common::write_tcp_table(
        &root,
        &[format!(
            "   0: 0100000A:C350 0200000A:01BB 01 00000000:00000000 00:00000000 00000000  {uid} 0 1111 1 0000000000000000 20 4 30 10 -1",
        )],
    );
    common::add_socket_owner(&root, 4242, 1111);

    let cache = common::cache(&root, &passwd);
    let state = NetState::new(ProcFs::new(&root));

    let mut outbound = PacketInfo::new(
        "10.0.0.1".parse().unwrap(),
        "10.0.0.2".parse().unwrap(),
        IpProtocol::TCP,
        false,
    );
    outbound.src_port = 50000;
    outbound.dst_port = 443;

    let (pid, inbound, error) = get_pid_of_connection(&cache, &state, &mut outbound, false);
    assert_eq!(pid, 4242);
    assert!(!inbound);
    assert!(error.is_none());

    let process = get_process_of_connection(&cache, &state, &mut outbound, false).unwrap();
    assert_eq!(process.pid, 4242);
    assert_eq!(process.name, "server");

    // Packets that already carry a PID are not looked up again.
    let mut tagged = outbound.clone();
    tagged.pid = 777;
    let (pid, _, _) = get_pid_of_connection(&cache, &state, &mut tagged, false);
    assert_eq!(pid, 777);

    // Unknown outbound connections are unidentified.
    let mut unknown = PacketInfo::new(
        "10.0.0.1".parse().unwrap(),
        "10.0.0.3".parse().unwrap(),
        IpProtocol::TCP,
        false,
    );
    unknown.src_port = 50001;
    unknown.dst_port = 443;
    let (pid, _, error) = get_pid_of_connection(&cache, &state, &mut unknown, false);
    assert_eq!(pid, UNIDENTIFIED_PROCESS_ID);
    assert!(error.is_some());

    // Inbound localhost connections are unidentified.
    let mut localhost = PacketInfo::new(
        "127.0.0.1".parse().unwrap(),
        "127.0.0.1".parse().unwrap(),
        IpProtocol::TCP,
        true,
    );
    localhost.src_port = 5555;
    localhost.dst_port = 9999;
    let (pid, inbound, _) = get_pid_of_connection(&cache, &state, &mut localhost, true);
    assert_eq!(pid, UNIDENTIFIED_PROCESS_ID);
    assert!(inbound);

    // Inbound connections from the network are unsolicited.
    let mut remote = PacketInfo::new(
        "10.0.0.9".parse().unwrap(),
        "10.0.0.1".parse().unwrap(),
        IpProtocol::TCP,
        true,
    );
    remote.src_port = 5555;
    remote.dst_port = 9999;
    let (pid, _, _) = get_pid_of_connection(&cache, &state, &mut remote, true);
    assert_eq!(pid, UNSOLICITED_PROCESS_ID);

    // ICMP is always unidentified.
    let mut icmp = PacketInfo::new(
        "10.0.0.9".parse().unwrap(),
        "10.0.0.1".parse().unwrap(),
        IpProtocol::Icmp,
        true,
    );
    let (pid, _, _) = get_pid_of_connection(&cache, &state, &mut icmp, true);
    assert_eq!(pid, UNIDENTIFIED_PROCESS_ID);

    // With detection disabled everything is unidentified.
    cache.set_enabled(false);
    let (pid, _, error) = get_pid_of_connection(&cache, &state, &mut outbound, false);
    assert_eq!(pid, UNIDENTIFIED_PROCESS_ID);
    assert!(error.is_none());
}
