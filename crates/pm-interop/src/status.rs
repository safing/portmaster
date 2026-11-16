//! Connection status shared by the IVPN event handlers and the firewall
//! verdict hook, plus the pure verdict decision extracted from the Go
//! `VerdictHandler` so it can be tested without a live firewall.

use std::net::IpAddr;

use pm_netstate::IpScope;
use pm_network::{Connection, Verdict};

use crate::protocol::{PROTOCOL_TCP, PROTOCOL_UDP};

/// Information about the active VPN connection of the IVPN client, used for
/// identifying VPN traffic in firewall verdicts. Mirrors Go `vpnConnectionInfo`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct VpnConnectionInfo {
    pub dst_port: u16,
    pub dst_address: Option<IpAddr>,
    pub protocol: u8,
}

impl VpnConnectionInfo {
    /// Returns whether this endpoint is usable for verdict matching. Go
    /// invalidates the port when the address cannot be parsed.
    pub fn is_valid(&self) -> bool {
        self.dst_port != 0 && self.dst_address.is_some()
    }
}

/// Information about the connected IVPN client. Mirrors Go `clientStatus`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ClientStatus {
    pub service_binary: String,
    pub service_port: u16,
    pub vpn_connection: VpnConnectionInfo,
    /// Whether a `ConnectedResp` event was seen; Go stores `*ConnectedResp`.
    pub connected_info: Option<crate::protocol::ConnectedResp>,
}

/// Answer of a single interoperability module's verdict handler.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VerdictDecision {
    pub verdict: Verdict,
    pub reason: String,
    pub skip_tunnel: bool,
}

impl VerdictDecision {
    /// Go returns this for every module that has no opinion.
    pub fn undecided() -> VerdictDecision {
        VerdictDecision {
            verdict: Verdict::Undecided,
            reason: String::new(),
            skip_tunnel: false,
        }
    }

    fn accept(reason: &str) -> VerdictDecision {
        VerdictDecision {
            verdict: Verdict::Accept,
            reason: reason.to_string(),
            skip_tunnel: true,
        }
    }

    pub fn is_undecided(&self) -> bool {
        self.verdict == Verdict::Undecided
    }
}

/// The remote endpoint of a connection, decoupled from `pm_intel::Entity` so
/// the verdict logic can be tested with mocks.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct EntityView {
    pub ip: Option<IpAddr>,
    pub protocol: u8,
    pub port: u16,
}

/// The connection fields the verdict handler reads, decoupled from the live
/// `pm_network::Connection`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ConnView {
    pub inbound: bool,
    pub ip_protocol: u8,
    pub local_ip_scope: IpScope,
    pub local_port: u16,
    pub entity: Option<EntityView>,
    pub process_path: Option<String>,
}

impl From<&Connection> for ConnView {
    fn from(conn: &Connection) -> ConnView {
        ConnView {
            inbound: conn.inbound,
            ip_protocol: conn.ip_protocol,
            local_ip_scope: conn.local_ip_scope,
            local_port: conn.local_port,
            entity: conn.entity.as_ref().map(|entity| EntityView {
                ip: entity.ip,
                protocol: entity.protocol,
                port: entity.port,
            }),
            process_path: conn.process.as_ref().map(|process| process.path.clone()),
        }
    }
}

/// Pure port of `InteropIvpn.VerdictHandler`.
///
/// `status` is `None` when the Go code would have a nil status pointer (no
/// connection attempt succeeded), which yields an undecided verdict.
pub fn decide(status: Option<&ClientStatus>, conn: &ConnView) -> VerdictDecision {
    let Some(status) = status else {
        return VerdictDecision::undecided();
    };

    // Connection to the remote VPN server.
    if status.vpn_connection.is_valid() {
        if let Some(entity) = &conn.entity {
            if entity.port == status.vpn_connection.dst_port
                && entity.protocol == status.vpn_connection.protocol
                && entity.ip == status.vpn_connection.dst_address
            {
                // By default, outbound connections are accepted. UDP traffic is
                // bidirectional, so inbound responses are accepted as well.
                if !conn.inbound || status.vpn_connection.protocol == PROTOCOL_UDP {
                    return VerdictDecision::accept("IVPN VPN connection");
                }
            }
        }
    }

    // Connections to the IVPN service port.
    if conn.local_ip_scope == IpScope::HostLocal && conn.ip_protocol == PROTOCOL_TCP {
        if conn.inbound && conn.local_port == status.service_port {
            return VerdictDecision::accept("IVPN Local Service connection");
        }
        if !conn.inbound
            && conn
                .entity
                .as_ref()
                .is_some_and(|entity| entity.port == status.service_port)
        {
            return VerdictDecision::accept("IVPN Local Service connection");
        }
    }

    // Connections from/to the IVPN service binary, once its path is known.
    if !status.service_binary.is_empty()
        && conn.process_path.as_deref() == Some(status.service_binary.as_str())
    {
        return VerdictDecision::accept("IVPN Service connection");
    }

    VerdictDecision::undecided()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    fn vpn_status() -> ClientStatus {
        ClientStatus {
            vpn_connection: VpnConnectionInfo {
                dst_port: 1194,
                dst_address: Some(IpAddr::V4(Ipv4Addr::new(203, 0, 113, 7))),
                protocol: PROTOCOL_UDP,
            },
            ..ClientStatus::default()
        }
    }

    fn vpn_conn(inbound: bool, protocol: u8) -> ConnView {
        ConnView {
            inbound,
            entity: Some(EntityView {
                ip: Some(IpAddr::V4(Ipv4Addr::new(203, 0, 113, 7))),
                protocol,
                port: 1194,
            }),
            ..ConnView::default()
        }
    }

    #[test]
    fn nil_status_is_undecided() {
        assert_eq!(decide(None, &vpn_conn(false, PROTOCOL_UDP)), VerdictDecision::undecided());
    }

    #[test]
    fn accepts_outbound_and_inbound_udp_vpn_traffic() {
        let status = vpn_status();
        for inbound in [false, true] {
            let decision = decide(Some(&status), &vpn_conn(inbound, PROTOCOL_UDP));
            assert_eq!(decision.verdict, Verdict::Accept);
            assert_eq!(decision.reason, "IVPN VPN connection");
            assert!(decision.skip_tunnel);
        }
    }

    #[test]
    fn does_not_accept_inbound_tcp_vpn_traffic() {
        let status = vpn_status();
        assert_eq!(
            decide(Some(&status), &vpn_conn(true, PROTOCOL_TCP)),
            VerdictDecision::undecided()
        );
    }

    #[test]
    fn rejects_vpn_traffic_with_wrong_endpoint() {
        let status = vpn_status();
        let mut conn = vpn_conn(false, PROTOCOL_UDP);
        conn.entity.as_mut().unwrap().port = 443;
        assert_eq!(decide(Some(&status), &conn), VerdictDecision::undecided());

        let mut conn = vpn_conn(false, PROTOCOL_UDP);
        conn.entity.as_mut().unwrap().ip = Some(IpAddr::V4(Ipv4Addr::new(1, 2, 3, 4)));
        assert_eq!(decide(Some(&status), &conn), VerdictDecision::undecided());
    }

    #[test]
    fn invalid_vpn_endpoint_is_ignored() {
        let mut status = vpn_status();
        status.vpn_connection.dst_port = 0;
        assert_eq!(
            decide(Some(&status), &vpn_conn(false, PROTOCOL_UDP)),
            VerdictDecision::undecided()
        );
    }

    #[test]
    fn accepts_local_service_port() {
        let status = ClientStatus {
            service_port: 12345,
            ..ClientStatus::default()
        };
        let inbound = ConnView {
            inbound: true,
            ip_protocol: PROTOCOL_TCP,
            local_ip_scope: IpScope::HostLocal,
            local_port: 12345,
            ..ConnView::default()
        };
        assert_eq!(
            decide(Some(&status), &inbound).reason,
            "IVPN Local Service connection"
        );

        let outbound = ConnView {
            ip_protocol: PROTOCOL_TCP,
            local_ip_scope: IpScope::HostLocal,
            entity: Some(EntityView {
                port: 12345,
                ..EntityView::default()
            }),
            ..ConnView::default()
        };
        assert_eq!(
            decide(Some(&status), &outbound).reason,
            "IVPN Local Service connection"
        );
    }

    #[test]
    fn local_service_checks_scope_and_protocol() {
        let status = ClientStatus {
            service_port: 12345,
            ..ClientStatus::default()
        };
        let mut conn = ConnView {
            inbound: true,
            ip_protocol: PROTOCOL_TCP,
            local_port: 12345,
            ..ConnView::default()
        };
        // Not host-local -> no match.
        assert_eq!(decide(Some(&status), &conn), VerdictDecision::undecided());
        conn.local_ip_scope = IpScope::HostLocal;
        // Not TCP -> no match.
        conn.ip_protocol = PROTOCOL_UDP;
        assert_eq!(decide(Some(&status), &conn), VerdictDecision::undecided());
    }

    #[test]
    fn accepts_service_binary() {
        let status = ClientStatus {
            service_binary: "/opt/ivpn/service".to_string(),
            ..ClientStatus::default()
        };
        let conn = ConnView {
            process_path: Some("/opt/ivpn/service".to_string()),
            ..ConnView::default()
        };
        assert_eq!(decide(Some(&status), &conn).reason, "IVPN Service connection");

        let other = ConnView {
            process_path: Some("/usr/bin/curl".to_string()),
            ..ConnView::default()
        };
        assert_eq!(decide(Some(&status), &other), VerdictDecision::undecided());
    }
}
