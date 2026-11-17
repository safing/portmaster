//! Platform-independent tests for the kext wire protocol.
//!
//! These run on Linux (and everywhere else): they exercise the ioctl codes,
//! struct layouts, command serialization and info parsers without touching
//! any Windows FFI.

use std::mem::size_of;

use pm_winkext::protocol::*;
use pm_winkext::{Error, KextVerdict, NetworkVerdict};

#[test]
fn ioctl_codes_match_go_ctl_code() {
    assert_eq!(ctl_code(40_000, 0x800, 0, 3), 0x9C40_E000);
    assert_eq!(IOCTL_VERSION, 0x9C40_E000);
    assert_eq!(IOCTL_SHUTDOWN_REQUEST, 0x9C40_E004);
    assert_eq!(IOCTL_RECV_VERDICT_REQ, 0x9C40_E008);
    assert_eq!(IOCTL_SET_VERDICT, 0x9C40_E00C);
    assert_eq!(IOCTL_GET_PAYLOAD, 0x9C40_E010);
    assert_eq!(IOCTL_CLEAR_CACHE, 0x9C40_E014);
    assert_eq!(IOCTL_UPDATE_VERDICT, 0x9C40_E018);
    assert_eq!(IOCTL_GET_CONNECTIONS_STAT, 0x9C40_E01C);
}

#[test]
fn v1_struct_sizes_match_go_unsafe_sizeof() {
    assert_eq!(size_of::<VerdictRequest>(), 72);
    assert_eq!(size_of::<VerdictInfo>(), 8);
    assert_eq!(size_of::<VerdictUpdateInfo>(), 40);
    assert_eq!(size_of::<ConnectionStat>(), 64);
}

#[test]
fn verdict_request_serialization_and_flags() {
    let request = VerdictRequest {
        id: 7,
        pid: 42,
        direction: 1,
        ip_v6: 0,
        protocol: 6,
        flags: VERDICT_REQUEST_FLAG_FAST_TRACK_PERMITTED
            | VERDICT_REQUEST_FLAG_EXPECT_SOCKET_AUTH,
        local_ip: [1, 0, 0, 0],
        remote_ip: [2, 0, 0, 0],
        local_port: 1234,
        remote_port: 80,
        compartment_id: 9,
        interface_index: 10,
        sub_interface_index: 11,
        packet_size: 128,
    };
    assert!(request.fast_tracked_by_integration());
    assert!(request.expect_info());
    assert!(!request.info_only());

    let bytes = request.to_bytes();
    assert_eq!(bytes.len(), 72);
    assert_eq!(&bytes[0..4], &7_u32.to_le_bytes());
    assert_eq!(&bytes[8..16], &42_u64.to_le_bytes());
    assert_eq!(bytes[16], 1);
    assert_eq!(bytes[17], 0);
    assert_eq!(bytes[18], 6);
    assert_eq!(bytes[19], 5);
    assert_eq!(&bytes[20..24], &1_u32.to_le_bytes());
    assert_eq!(&bytes[68..72], &128_u32.to_le_bytes());
}

#[test]
fn verdict_info_serialization() {
    let info = VerdictInfo {
        id: 0x0102_0304,
        verdict: NetworkVerdict::Accept.as_i8(),
    };
    assert_eq!(info.to_bytes(), vec![4, 3, 2, 1, 2]);
}

#[test]
fn ip_array_conversion_is_big_endian_per_word() {
    assert_eq!(ip_array_to_bytes([0x0102_0304, 0, 0, 0], false), vec![1, 2, 3, 4]);
    assert_eq!(
        ip_array_to_bytes([0x0102_0304, 0x0506_0708, 0x090a_0b0c, 0x0d0e_0f10], true),
        vec![
            1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16
        ]
    );
}

#[test]
fn version_display() {
    let version = Version::from_bytes([2, 1, 3, 7]);
    assert_eq!(version.to_string(), "2.1.3.7");
}

#[test]
fn v2_command_bytes() {
    assert_eq!(simple_command(Command::Shutdown), [0]);
    assert_eq!(simple_command(Command::CleanEndedConnections), [8]);

    let verdict = verdict_command(0x0102_0304_0506_0708, KextVerdict::PermanentBlock);
    assert_eq!(
        verdict,
        [1, 8, 7, 6, 5, 4, 3, 2, 1, KextVerdict::PermanentBlock.as_u8()]
    );

    let update4 = update_v4_command(
        6,
        [1, 2, 3, 4],
        0x0102,
        [5, 6, 7, 8],
        0x0304,
        KextVerdict::Drop,
    );
    assert_eq!(update4.len(), 15);
    assert_eq!(update4[0], Command::UpdateV4.as_u8());
    assert_eq!(update4[1], 6);
    assert_eq!(&update4[2..6], &[1, 2, 3, 4]);
    assert_eq!(&update4[6..8], &0x0102_u16.to_le_bytes());
    assert_eq!(&update4[8..12], &[5, 6, 7, 8]);
    assert_eq!(&update4[12..14], &0x0304_u16.to_le_bytes());
    assert_eq!(update4[14], KextVerdict::Drop.as_u8());

    let mut local6 = [0_u8; 16];
    let mut remote6 = [0_u8; 16];
    for (index, byte) in local6.iter_mut().enumerate() {
        *byte = index as u8 + 1;
    }
    for (index, byte) in remote6.iter_mut().enumerate() {
        *byte = index as u8 + 17;
    }
    let update6 = update_v6_command(17, local6, 1, remote6, 2, KextVerdict::Accept);
    assert_eq!(update6.len(), 39);
    assert_eq!(update6[0], Command::UpdateV6.as_u8());
    assert_eq!(&update6[2..18], &local6);
    assert_eq!(&update6[20..36], &remote6);
    assert_eq!(update6[38], KextVerdict::Accept.as_u8());
}

#[test]
fn kext_verdict_mapping_matches_go() {
    assert_eq!(
        kext_verdict_from(NetworkVerdict::Undecided, false),
        KextVerdict::Undecided
    );
    assert_eq!(
        kext_verdict_from(NetworkVerdict::Accept, false),
        KextVerdict::Accept
    );
    assert_eq!(
        kext_verdict_from(NetworkVerdict::Accept, true),
        KextVerdict::PermanentAccept
    );
    assert_eq!(
        kext_verdict_from(NetworkVerdict::Block, true),
        KextVerdict::PermanentBlock
    );
    assert_eq!(
        kext_verdict_from(NetworkVerdict::Drop, true),
        KextVerdict::PermanentDrop
    );
    assert_eq!(
        kext_verdict_from(NetworkVerdict::RerouteToSplitTun, false),
        KextVerdict::RerouteToSplitTun
    );
}

fn frame(info_type: InfoType, payload: &[u8]) -> Vec<u8> {
    let mut frame = vec![info_type as u8];
    frame.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    frame.extend_from_slice(payload);
    frame
}

#[test]
fn parse_connection_v4_info() {
    let mut payload = Vec::new();
    payload.extend_from_slice(&1_u64.to_le_bytes()); // id
    payload.extend_from_slice(&2_u64.to_le_bytes()); // process id
    payload.push(1); // direction: inbound
    payload.push(6); // protocol: TCP
    payload.extend_from_slice(&[10, 0, 0, 1]); // local ip
    payload.extend_from_slice(&[10, 0, 0, 2]); // remote ip
    payload.extend_from_slice(&1111_u16.to_le_bytes());
    payload.extend_from_slice(&2222_u16.to_le_bytes());
    payload.push(3); // payload layer
    payload.extend_from_slice(&3_u32.to_le_bytes());
    payload.extend_from_slice(&[7, 8, 9]);

    let bytes = frame(InfoType::ConnectionV4, &payload);
    let (info, consumed) = parse_info(&bytes).unwrap();
    assert_eq!(consumed, bytes.len());
    match info {
        Info::ConnectionV4(conn) => {
            assert_eq!(conn.id, 1);
            assert_eq!(conn.process_id, 2);
            assert_eq!(conn.direction, 1);
            assert_eq!(conn.protocol, 6);
            assert_eq!(conn.local_ip, [10, 0, 0, 1]);
            assert_eq!(conn.remote_ip, [10, 0, 0, 2]);
            assert_eq!(conn.local_port, 1111);
            assert_eq!(conn.remote_port, 2222);
            assert_eq!(conn.payload_layer, 3);
            assert_eq!(conn.payload, vec![7, 8, 9]);
        }
        other => panic!("unexpected info: {other:?}"),
    }
}

#[test]
fn parse_connection_v6_info_without_payload() {
    let mut payload = Vec::new();
    payload.extend_from_slice(&5_u64.to_le_bytes());
    payload.extend_from_slice(&0_u64.to_le_bytes());
    payload.push(0);
    payload.push(17);
    payload.extend_from_slice(&[1; 16]);
    payload.extend_from_slice(&[2; 16]);
    payload.extend_from_slice(&53_u16.to_le_bytes());
    payload.extend_from_slice(&54_u16.to_le_bytes());
    payload.push(4);
    payload.extend_from_slice(&0_u32.to_le_bytes());

    let bytes = frame(InfoType::ConnectionV6, &payload);
    let (info, _) = parse_info(&bytes).unwrap();
    match info {
        Info::ConnectionV6(conn) => {
            assert_eq!(conn.id, 5);
            assert_eq!(conn.local_ip, [1; 16]);
            assert_eq!(conn.remote_ip, [2; 16]);
            assert!(conn.payload.is_empty());
        }
        other => panic!("unexpected info: {other:?}"),
    }
}

#[test]
fn parse_connection_end_events() {
    let mut v4 = 99_u64.to_le_bytes().to_vec();
    v4.extend_from_slice(&[1, 6]);
    v4.extend_from_slice(&[1, 2, 3, 4]);
    v4.extend_from_slice(&[5, 6, 7, 8]);
    v4.extend_from_slice(&10_u16.to_le_bytes());
    v4.extend_from_slice(&20_u16.to_le_bytes());
    let (info, _) = parse_info(&frame(InfoType::ConnectionEndV4, &v4)).unwrap();
    match info {
        Info::ConnectionEndV4(end) => {
            assert_eq!(end.process_id, 99);
            assert_eq!(end.local_port, 10);
            assert_eq!(end.remote_port, 20);
        }
        other => panic!("unexpected info: {other:?}"),
    }

    let mut v6 = 100_u64.to_le_bytes().to_vec();
    v6.extend_from_slice(&[0, 17]);
    v6.extend_from_slice(&[9; 16]);
    v6.extend_from_slice(&[8; 16]);
    v6.extend_from_slice(&1_u16.to_le_bytes());
    v6.extend_from_slice(&2_u16.to_le_bytes());
    let (info, _) = parse_info(&frame(InfoType::ConnectionEndV6, &v6)).unwrap();
    assert!(matches!(info, Info::ConnectionEndV6(_)));
}

#[test]
fn parse_log_line_info() {
    let mut payload = vec![4]; // warning
    payload.extend_from_slice(b"kext says hi");
    let bytes = frame(InfoType::LogLine, &payload);
    let (info, _) = parse_info(&bytes).unwrap();
    match info {
        Info::LogLine(line) => {
            assert_eq!(line.severity, 4);
            assert_eq!(line.line, "kext says hi");
        }
        other => panic!("unexpected info: {other:?}"),
    }
}

#[test]
fn parse_bandwidth_stats_v4_and_v6() {
    let mut v4 = vec![6]; // protocol
    v4.extend_from_slice(&2_u32.to_le_bytes());
    for port in [100_u16, 200_u16] {
        v4.extend_from_slice(&[1, 2, 3, 4]);
        v4.extend_from_slice(&port.to_le_bytes());
        v4.extend_from_slice(&[5, 6, 7, 8]);
        v4.extend_from_slice(&(port + 1000).to_le_bytes()); // remote port
        v4.extend_from_slice(&(port as u64 + 1).to_le_bytes());
        v4.extend_from_slice(&(port as u64 + 2).to_le_bytes());
    }
    let (info, _) = parse_info(&frame(InfoType::BandwidthStatsV4, &v4)).unwrap();
    match info {
        Info::BandwidthStats(stats) => {
            assert_eq!(stats.protocol, 6);
            assert_eq!(stats.values_v4.len(), 2);
            assert_eq!(stats.values_v4[0].local_port, 100);
            assert_eq!(stats.values_v4[1].remote_port, 1200);
            assert_eq!(stats.values_v4[1].transmitted_bytes, 201);
            assert!(stats.values_v6.is_empty());
        }
        other => panic!("unexpected info: {other:?}"),
    }

    let mut v6 = vec![17];
    v6.extend_from_slice(&1_u32.to_le_bytes());
    v6.extend_from_slice(&[1; 16]);
    v6.extend_from_slice(&1_u16.to_le_bytes());
    v6.extend_from_slice(&[2; 16]);
    v6.extend_from_slice(&2_u16.to_le_bytes());
    v6.extend_from_slice(&3_u64.to_le_bytes());
    v6.extend_from_slice(&4_u64.to_le_bytes());
    let (info, _) = parse_info(&frame(InfoType::BandwidthStatsV6, &v6)).unwrap();
    match info {
        Info::BandwidthStats(stats) => {
            assert_eq!(stats.values_v6.len(), 1);
            assert_eq!(stats.values_v6[0].received_bytes, 4);
        }
        other => panic!("unexpected info: {other:?}"),
    }
}

#[test]
fn parse_multiple_frames_reports_consumed_bytes() {
    let first = frame(InfoType::LogLine, &[3, b'a', b'b']);
    let second = frame(InfoType::LogLine, &[3, b'c', b'd']);
    let mut both = first.clone();
    both.extend_from_slice(&second);

    let (info, consumed) = parse_info(&both).unwrap();
    assert_eq!(consumed, first.len());
    assert!(matches!(info, Info::LogLine(_)));
    let (info, consumed) = parse_info(&both[consumed..]).unwrap();
    assert_eq!(consumed, second.len());
    match info {
        Info::LogLine(line) => assert_eq!(line.line, "cd"),
        other => panic!("unexpected info: {other:?}"),
    }
}

#[test]
fn parse_rejects_bad_frames() {
    assert!(matches!(
        parse_info(&[0, 1]),
        Err(Error::UnexpectedRead(_))
    ));
    assert!(matches!(
        parse_info(&[99, 0, 0, 0, 0]),
        Err(Error::UnknownInfoType(99))
    ));
    // Announced size exceeds the available payload.
    assert!(matches!(
        parse_info(&[0, 10, 0, 0, 0, 1]),
        Err(Error::UnexpectedInfoSize)
    ));
    // A truncated fixed-size payload.
    assert!(matches!(
        parse_info(&frame(InfoType::ConnectionV4, &[0; 4])),
        Err(Error::UnexpectedInfoSize)
    ));
}
