//! Tests for the D-Bus property parsing and NetworkManager logic
//! (Go `netenv/dbus_linux.go`).

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::Mutex;

use pm_netenv::dbus::{
    connectivity_state_from_dbus, nameservers_from_dbus, parse_busctl_value,
    parse_connectivity_u32, ConnectivityState, DbusValue, NM_OBJECT,
};
use pm_netenv::DbusClient;

#[derive(Default)]
struct FakeDbus {
    properties: Mutex<HashMap<String, DbusValue>>,
}

impl FakeDbus {
    fn with(mut self, object: &str, interface: &str, property: &str, value: DbusValue) -> Self {
        self.properties
            .get_mut()
            .unwrap()
            .insert(format!("{object}|{interface}|{property}"), value);
        self
    }
}

impl DbusClient for FakeDbus {
    fn get_property(
        &self,
        _service: &str,
        object: &str,
        interface: &str,
        property: &str,
    ) -> Result<DbusValue, String> {
        self.properties
            .lock()
            .unwrap()
            .get(&format!("{object}|{interface}|{property}"))
            .cloned()
            .ok_or_else(|| format!("property {interface}.{property} not found"))
    }
}

#[test]
fn busctl_scalars_and_arrays_are_parsed() {
    assert_eq!(parse_busctl_value("uint32 4"), DbusValue::U32(4));
    assert_eq!(
        parse_busctl_value("o \"/org/freedesktop/NetworkManager/ActiveConnection/1\""),
        DbusValue::ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1".to_string())
    );
    assert_eq!(
        parse_busctl_value("as 2 \"example.com\" \"search.example\""),
        DbusValue::StringArray(vec![
            "example.com".to_string(),
            "search.example".to_string()
        ])
    );
    assert_eq!(
        parse_busctl_value("au 2 3232235777 16843009"),
        DbusValue::U32Array(vec![3232235777, 16843009])
    );
    assert_eq!(
        parse_busctl_value("aay 2 4 192 0 2 1 4 32 1 13 184"),
        DbusValue::ByteArrayArray(vec![vec![192, 0, 2, 1], vec![32, 1, 13, 184]])
    );
    assert_eq!(parse_connectivity_u32("<uint32 3>").unwrap(), 3);
    assert!(parse_connectivity_u32("garbage").is_err());
}

#[test]
fn connectivity_state_is_read_and_mapped() {
    let client = FakeDbus::default().with(
        NM_OBJECT,
        "org.freedesktop.NetworkManager",
        "Connectivity",
        DbusValue::U32(2),
    );
    assert_eq!(
        connectivity_state_from_dbus(&client).unwrap(),
        ConnectivityState::Portal
    );

    let tolerant = FakeDbus::default().with(
        NM_OBJECT,
        "org.freedesktop.NetworkManager",
        "Connectivity",
        DbusValue::Other("uint32 4".to_string()),
    );
    assert_eq!(
        connectivity_state_from_dbus(&tolerant)
            .unwrap()
            .online_status(),
        pm_netenv::OnlineStatus::Online
    );
}

#[test]
fn nameservers_are_collected_primary_first_and_deduplicated_by_order() {
    let primary = "/org/freedesktop/NetworkManager/ActiveConnection/1";
    let secondary = "/org/freedesktop/NetworkManager/ActiveConnection/2";
    let nm = "org.freedesktop.NetworkManager";
    let active = "org.freedesktop.NetworkManager.Connection.Active";
    let ip4 = "org.freedesktop.NetworkManager.IP4Config";
    let ip6 = "org.freedesktop.NetworkManager.IP6Config";

    let client = FakeDbus::default()
        .with(
            NM_OBJECT,
            nm,
            "PrimaryConnection",
            DbusValue::ObjectPath(primary.to_string()),
        )
        .with(
            NM_OBJECT,
            nm,
            "ActiveConnections",
            DbusValue::Other(format!("ao 2 {primary} {secondary}")),
        )
        .with(
            primary,
            active,
            "Ip4Config",
            DbusValue::ObjectPath("/ip4/1".to_string()),
        )
        .with(
            primary,
            active,
            "Ip6Config",
            DbusValue::ObjectPath("/".to_string()),
        )
        .with(
            secondary,
            active,
            "Ip4Config",
            DbusValue::ObjectPath("/".to_string()),
        )
        .with(
            secondary,
            active,
            "Ip6Config",
            DbusValue::ObjectPath("/ip6/2".to_string()),
        )
        // 192.0.2.1 as a NetworkManager uint32 (little-endian byte order).
        .with(
            "/ip4/1",
            ip4,
            "Nameservers",
            DbusValue::U32Array(vec![0x0102_00C0]),
        )
        .with(
            "/ip4/1",
            ip4,
            "Domains",
            DbusValue::StringArray(vec!["example".to_string()]),
        )
        .with(
            "/ip4/1",
            ip4,
            "Searches",
            DbusValue::StringArray(vec!["search.example".to_string()]),
        )
        .with(
            "/ip6/2",
            ip6,
            "Nameservers",
            DbusValue::ByteArrayArray(vec![vec![
                0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
            ]]),
        )
        .with("/ip6/2", ip6, "Domains", DbusValue::StringArray(vec![]))
        .with("/ip6/2", ip6, "Searches", DbusValue::StringArray(vec![]));

    let nameservers = nameservers_from_dbus(&client).expect("nameservers");
    assert_eq!(nameservers.len(), 2);
    assert_eq!(nameservers[0].ip, "192.0.2.1".parse::<IpAddr>().unwrap());
    assert_eq!(nameservers[0].search, vec!["example", "search.example"]);
    assert_eq!(nameservers[1].ip, "2001:db8::1".parse::<IpAddr>().unwrap());
    assert!(nameservers[1].search.is_empty());
}
