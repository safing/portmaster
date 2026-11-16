//! Injected global state of the firewall deciders.

use std::net::IpAddr;

use pm_netenv::OnlineStatus;
use pm_network::Connection;
use pm_process::Process;
use pm_profile::Profile;

/// A local network, as returned by Go `netenv.GetLocalNetwork`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LocalNetwork {
    pub ip: IpAddr,
    pub prefix_len: u8,
}

/// The outgoing multicast or broadcast connection that a reply belongs to,
/// like the remote side of Go `network.GetMulticastRequestConn`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MulticastRequest {
    pub protocol: u8,
    pub ip: IpAddr,
    pub port: u16,
}

/// Lookups in the custom filter list (Go `service/intel/customlists`).
pub trait CustomLists {
    /// Whether a custom list is loaded at all.
    fn is_loaded(&self) -> bool;
    /// Returns whether the domain is listed and the matching list entry.
    fn lookup_domain(&self, fqdn: &str, filter_subdomains: bool) -> (bool, String);
    fn lookup_ip(&self, ip: IpAddr) -> bool;
    fn lookup_asn(&self, asn: u32) -> bool;
    fn lookup_country(&self, country: &str) -> bool;
}

/// A custom list that is never loaded.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoCustomLists;

impl CustomLists for NoCustomLists {
    fn is_loaded(&self) -> bool {
        false
    }
    fn lookup_domain(&self, _fqdn: &str, _filter_subdomains: bool) -> (bool, String) {
        (false, String::new())
    }
    fn lookup_ip(&self, _ip: IpAddr) -> bool {
        false
    }
    fn lookup_asn(&self, _asn: u32) -> bool {
        false
    }
    fn lookup_country(&self, _country: &str) -> bool {
        false
    }
}

/// Everything Go reads from package globals while deciding on a connection.
pub trait FirewallEnv {
    /// PID of the Portmaster core itself.
    fn own_pid(&self) -> i32;
    /// Go `netenv.IsMyIP`.
    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String>;
    /// Go `netenv.GetLocalNetwork`.
    fn local_network(&self, ip: IpAddr) -> Result<Option<LocalNetwork>, String>;
    /// Go `network.GetMulticastRequestConn`.
    fn multicast_request(&self, conn: &Connection, network: &LocalNetwork)
        -> Option<MulticastRequest>;
    /// Go `netenv.GetOnlineStatus`.
    fn online_status(&self) -> OnlineStatus;
    /// Go `netenv.IsConnectivityDomain`.
    fn is_connectivity_domain(&self, domain: &str) -> bool;
    /// Go `resolver.IsDisabled`.
    fn resolver_disabled(&self) -> bool;
    /// Go `compat.ReportSecureDNSBypassIssue`.
    fn report_secure_dns_bypass(&self, _process: Option<&Process>) {}
    /// Go `filterlists.GetUnbreakFilterListIDs`.
    fn unbreak_filter_list_ids(&self) -> Vec<String>;
    fn custom_lists(&self) -> &dyn CustomLists;
    /// Reloads a local profile when the layered profile needs an update.
    fn load_local_profile(&self, id: &str) -> Result<Profile, String>;
    /// Asks the user about the connection; the real prompt is a later task.
    fn prompt(&self, conn: &mut Connection);
}
