//! Injected global state and side effects of the packet handler.

use std::io;
use std::net::IpAddr;
use std::sync::Arc;

use pm_network::{Connection, ProfileLookup, SharedConnection, Verdict};
use pm_process::Process;
use pm_profile::LayeredProfile;

use super::queued::QueuedPacket;
use crate::env::FirewallEnv;

pub use pm_interception::conntrack::ConnTuple as ConnectionTuple;

/// Gives the deciders the layered profile of a process.
///
/// It must be backed by the same profile source as the `ProfileLookup` in the
/// environment of the `Network`, so that `Reason.Profile` and the profile that
/// decides agree. The handler installs this lookup on every connection it
/// creates, which keeps the reason source in sync.
pub trait LayeredProfileLookup: ProfileLookup {
    fn layered_profile(&self, process: &Process) -> Option<Arc<LayeredProfile>>;
}

/// An answer of the external verdict handler (Go `ExtVerdictHandlerFunc`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ExternalVerdict {
    pub verdict: Verdict,
    pub reason: String,
    pub skip_tunnel: bool,
}

/// Everything the packet handler reads from package globals or calls outside
/// of the firewall decision. Methods with a default are optional integrations
/// that are not part of the port (SPN, split tunnel, compat, ICMP listeners).
pub trait PacketEnv: FirewallEnv + Send + Sync {
    /// Go `filterEnabled`.
    fn filter_enabled(&self) -> bool;
    /// Go `permanentVerdicts`.
    fn permanent_verdicts(&self) -> bool;
    /// Go `dnsQueryInterception`.
    fn dns_query_interception(&self) -> bool;

    /// Go `apiIP` and `apiPort`; `None` while the API port is not set.
    fn api_endpoint(&self) -> Option<(IpAddr, u16)> {
        None
    }

    /// Go `netenv.IPv6Enabled`.
    fn ipv6_enabled(&self) -> bool {
        true
    }

    /// Go `netenv.SubmitPacketToICMPListener`; returns whether it was taken.
    fn submit_icmp(&self, _pkt: &dyn QueuedPacket) -> bool {
        false
    }

    /// Go `compat.SubmitSystemIntegrationCheckPacket`.
    fn submit_system_integration_check(&self, _pkt: &dyn QueuedPacket) {}

    /// Go `compat.SubmitDNSCheckDomain`.
    fn submit_dns_check_domain(&self, _subdomain: &str) {}

    /// Go `UpdateIPsAndCNAMEs`. The connection is locked by the caller.
    fn update_ips_and_cnames(
        &self,
        _query: &pm_resolver::Query,
        _rr: &pm_resolver::RRCache,
        _conn: &Connection,
    ) {
    }

    /// Go `externalVerdictHandler`; only accept and block are honored.
    fn external_verdict(&self, _conn: &Connection) -> Option<ExternalVerdict> {
        None
    }

    /// Go `splittun.IsProxiedConnectionInfo` (with `splittun.IsReady`).
    fn is_proxied_split_tunnel_connection(&self, _conn: &Connection) -> bool {
        false
    }

    /// Go `checkTunneling` (SPN).
    fn check_tunneling(&self, _conn: &mut Connection) {}

    /// Go `checkSplitTunneling`.
    fn check_split_tunneling(&self, _conn: &mut Connection) {}

    /// Go `requestTunneling`; only called when the verdict is
    /// `RerouteToTunnel`.
    fn request_tunneling(&self, _conn: &mut Connection) -> Result<(), String> {
        Ok(())
    }

    /// Go `requestSplitTunneling`; only called when the verdict is
    /// `RerouteToSplitTun`.
    fn request_split_tunneling(&self, _conn: &mut Connection) -> Result<(), String> {
        Ok(())
    }

    /// Go `inspection.RunInspectors`. The default runs the process-wide
    /// registry; environments that own a registry override this method.
    fn run_inspectors(&self, conn: &mut Connection, pkt: &dyn QueuedPacket) -> (Verdict, bool) {
        crate::inspection::run_inspectors(conn, pkt)
    }

    /// Go `Connection.RemovePrompt`; called before the connection is locked.
    fn remove_prompt(&self, _conn: &SharedConnection) {}
}

/// Receives the byte deltas of connections; the netquery manager implements
/// it. Mirrors the Go `NetQuery().Store.UpdateBandwidth` call, including the
/// `source/profile` string.
pub trait BandwidthSink: Send + Sync {
    fn update_bandwidth(
        &self,
        history_enabled: bool,
        profile: &str,
        process_key: &str,
        conn_id: &str,
        bytes_received: u64,
        bytes_sent: u64,
    ) -> Result<(), String>;
}

/// Conntrack operations of the verdict reset (Go `nfq.DeleteMarkedConnection`
/// and `nfq.DeleteAllMarkedConnection`).
pub trait ConntrackControl: Send + Sync {
    /// Deletes the conntrack entry of one connection.
    fn delete_connection(&self, tuple: &ConnectionTuple) -> io::Result<()>;
    /// Deletes all entries with permanent verdict marks and returns how many.
    fn delete_all_marked(&self, ipv6: bool) -> io::Result<usize>;
}

/// [`ConntrackControl`] backed by ctnetlink; needs CAP_NET_ADMIN.
#[derive(Default)]
pub struct SystemConntrack {
    socket: std::sync::Mutex<Option<pm_interception::conntrack::Conntrack>>,
}

impl SystemConntrack {
    pub fn new() -> SystemConntrack {
        SystemConntrack::default()
    }

    fn with<T>(
        &self,
        f: impl FnOnce(&mut pm_interception::conntrack::Conntrack) -> io::Result<T>,
    ) -> io::Result<T> {
        let mut guard = self.socket.lock().unwrap_or_else(|e| e.into_inner());
        if guard.is_none() {
            *guard = Some(pm_interception::conntrack::Conntrack::open()?);
        }
        let socket = guard.as_mut().expect("socket was just opened");
        f(socket)
    }
}

impl ConntrackControl for SystemConntrack {
    fn delete_connection(&self, tuple: &ConnectionTuple) -> io::Result<()> {
        self.with(|ct| ct.delete_tuple(tuple))
    }

    fn delete_all_marked(&self, ipv6: bool) -> io::Result<usize> {
        use pm_interception::conntrack::Family;
        self.with(|ct| {
            let (mut deleted, err) = ct.delete_marked(Family::V4);
            if ipv6 {
                let (more, err6) = ct.delete_marked(Family::V6);
                deleted += more;
                if let Some(err) = err6 {
                    tracing::debug!("nfq: error deleting ipv6 conntrack entries: {err}");
                }
            }
            if let Some(err) = err {
                tracing::debug!("nfq: error deleting ipv4 conntrack entries: {err}");
            }
            Ok(deleted)
        })
    }
}
