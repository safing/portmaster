//! Port of Go `service/firewall/packet_handler.go`: the per-connection handler
//! chain and the verdict logic.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, MutexGuard, RwLock};

use pm_interception::Verdict as PktVerdict;
use pm_netstate::{ip_scope, IpScope, UNDEFINED_PROCESS_ID};
use pm_network::{
    canonical_ip, protocol, Connection, Network, SharedConnection, Verdict,
};
use pm_profile::options::CFG_OPTION_SPLIT_TUN_USE_KEY;

use super::env::{BandwidthSink, ConntrackControl, LayeredProfileLookup, PacketEnv};
use super::queued::{IcmpType, QueuedPacket};
use crate::bypass::looks_like_outgoing_dns_request;
use crate::deciders::NO_REASON_OPTION_KEY;
use crate::master::decide_on_connection;
use super::pid_hints::PidHints;
use crate::preauth::PreAuthPorts;

const SYSTEM_INTEGRATION_CHECK_DST: Ipv4Addr = Ipv4Addr::new(127, 65, 67, 75);
/// Go `packet.AnyHostInternalProtocol61`, used by the system integration check.
const SYSTEM_INTEGRATION_CHECK_PROTOCOL: u8 = 61;
/// Ports of encrypted protocols; Go marks these connections as encrypted.
const ENCRYPTED_PORTS: [u16; 6] = [22, 443, 465, 853, 993, 995];

/// The firewall handler of a connection (Go `network.FirewallHandler`).
/// A connection without handler uses the default handler.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) enum Handler {
    FastTrack,
    GatherData,
    Filter,
    InspectAndVerdict,
    InspectDns,
    IcmpFilter,
}

/// Counters of issued packet verdicts (Go `packetsAccepted` and friends).
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct PacketStats {
    pub accepted: u64,
    pub blocked: u64,
    pub dropped: u64,
    pub failed: u64,
}

#[derive(Default)]
struct Counters {
    accepted: AtomicU64,
    blocked: AtomicU64,
    dropped: AtomicU64,
    failed: AtomicU64,
}

/// Returned when the nameserver IP matcher is set twice.
#[derive(Debug, Clone, Copy, PartialEq, Eq, thiserror::Error)]
#[error("nameserver IP matcher already set")]
pub struct MatcherAlreadySet;

type NameserverMatcher = Box<dyn Fn(IpAddr) -> bool + Send + Sync>;

/// The shared state and the handler chain of the packet handler. Not used
/// directly: [`super::PacketHandler`] dereferences to it for the non-dispatch
/// API (stats, resets, bandwidth).
pub struct HandlerCore {
    pub(super) network: Arc<Network>,
    pub(super) env: Arc<dyn PacketEnv>,
    pub(super) profiles: Arc<dyn LayeredProfileLookup>,
    preauth: Arc<PreAuthPorts>,
    pub(super) conntrack: Arc<dyn ConntrackControl>,
    pub(super) bandwidth: Option<Arc<dyn BandwidthSink>>,
    nameserver_matcher: RwLock<Option<NameserverMatcher>>,
    pub(super) pid_hints: PidHints,
    counters: Counters,
}

pub(super) fn locked<T>(mutex: &Mutex<T>) -> MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|e| e.into_inner())
}

impl HandlerCore {
    pub(super) fn new(
        network: Arc<Network>,
        env: Arc<dyn PacketEnv>,
        profiles: Arc<dyn LayeredProfileLookup>,
        preauth: Arc<PreAuthPorts>,
        conntrack: Arc<dyn ConntrackControl>,
        bandwidth: Option<Arc<dyn BandwidthSink>>,
    ) -> HandlerCore {
        HandlerCore {
            network,
            env,
            profiles,
            preauth,
            conntrack,
            bandwidth,
            nameserver_matcher: RwLock::new(None),
            pid_hints: PidHints::new(),
            counters: Counters::default(),
        }
    }

    /// Go `SetNameserverIPMatcher`. Replaces any previous matcher so that
    /// nameserver changes can be propagated at runtime.
    pub fn set_nameserver_ip_matcher(&self, matcher: NameserverMatcher) {
        *self
            .nameserver_matcher
            .write()
            .unwrap_or_else(|error| error.into_inner()) = Some(matcher);
    }

    pub fn stats(&self) -> PacketStats {
        PacketStats {
            accepted: self.counters.accepted.load(Ordering::Relaxed),
            blocked: self.counters.blocked.load(Ordering::Relaxed),
            dropped: self.counters.dropped.load(Ordering::Relaxed),
            failed: self.counters.failed.load(Ordering::Relaxed),
        }
    }

    /// Returns and resets the counters, like the Go stat logger does.
    pub fn take_stats(&self) -> PacketStats {
        PacketStats {
            accepted: self.counters.accepted.swap(0, Ordering::Relaxed),
            blocked: self.counters.blocked.swap(0, Ordering::Relaxed),
            dropped: self.counters.dropped.swap(0, Ordering::Relaxed),
            failed: self.counters.failed.swap(0, Ordering::Relaxed),
        }
    }

    /// Runs the connection's handler on a packet, with the connection locked
    /// by the caller.
    pub(super) fn run_handler(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        state: &mut Option<Handler>,
    ) {
        match *state {
            None => self.default_handler(conn, pkt),
            Some(Handler::FastTrack) => self.fast_track_handler(shared, conn, pkt, state),
            Some(Handler::GatherData) => self.gather_data_handler(shared, conn, pkt, state),
            Some(Handler::Filter) => self.filter_handler(shared, conn, pkt, state),
            Some(Handler::InspectAndVerdict) => self.inspect_and_verdict_handler(conn, pkt, state),
            Some(Handler::InspectDns) => self.inspect_dns_packet(shared, conn, pkt),
            Some(Handler::IcmpFilter) => self.icmp_filter_handler(conn, pkt),
        }
    }

    fn matches_nameserver(&self, ip: IpAddr) -> Option<bool> {
        self.nameserver_matcher
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .as_ref()
            .map(|matcher| matcher(ip))
    }

    /// Go `fastTrackedPermit`.
    fn fast_tracked_permit(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
    ) -> (Verdict, bool) {
        let meta = pkt.info();
        let undecided = (Verdict::Undecided, false);

        // Check if the packet was already fast-tracked by the OS integration.
        if pkt.fast_tracked_by_integration() {
            tracing::trace!("filter: fast-tracked by OS integration");
            return (Verdict::Accept, true);
        }

        let (Some(src), Some(dst)) = (meta.src.map(canonical_ip), meta.dst.map(canonical_ip))
        else {
            return undecided;
        };

        // The connection was blocked before.
        if dst == IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17)) || dst == IpAddr::V6(blocked_ipv6()) {
            return (Verdict::Block, true);
        }

        // Some programs connect to their own IP and port to test networking.
        if meta.src_port == meta.dst_port && src == dst {
            tracing::trace!("filter: fast-track network self-check");
            return (Verdict::Accept, true);
        }

        match meta.protocol {
            protocol::ICMP | protocol::ICMPV6 => {
                let icmp = pkt.icmp_type();
                if icmp == IcmpType::LoadFailed {
                    tracing::trace!("filter: failed to load ICMP packet data");
                    return (Verdict::Accept, true);
                }

                // A listener needs to see all packets, so no permanent accept.
                if self.env.submit_icmp(pkt) {
                    tracing::trace!("filter: fast-track tracing ICMP/v6");
                    return (Verdict::Accept, false);
                }

                // Echo requests and replies are handled regularly, everything
                // else is system business.
                if matches!(icmp, IcmpType::Type(t) if is_echo(meta.protocol, t)) {
                    return undecided;
                }
                tracing::trace!("filter: fast-track accepting ICMP/v6");
                (Verdict::Accept, true)
            }

            protocol::UDP | protocol::TCP => {
                let api = self.env.api_endpoint();
                match meta.dst_port {
                    67 | 68 | 546 | 547 => {
                        // DHCP and DHCPv6 are UDP in local network scopes only.
                        if meta.protocol != protocol::UDP {
                            return undecided;
                        }
                        match ip_scope(dst) {
                            IpScope::HostLocal
                            | IpScope::LinkLocal
                            | IpScope::SiteLocal
                            | IpScope::LocalMulticast => {}
                            _ => return undecided,
                        }
                        tracing::trace!("filter: fast-track accepting DHCP");
                        (Verdict::Accept, true)
                    }

                    port if api.is_some_and(|(_, api_port)| api_port == port) => {
                        // Direct access to the Portmaster API (TCP, API IP,
                        // from an own IP).
                        let Some((api_ip, _)) = api else {
                            return undecided;
                        };
                        if meta.protocol != protocol::TCP || dst != canonical_ip(api_ip) {
                            return undecided;
                        }
                        match self.env.is_my_ip(src) {
                            Ok(true) => {}
                            Ok(false) => return undecided,
                            Err(error) => {
                                tracing::debug!(
                                    "filter: failed to check if {src} is own IP for fast-track: {error}"
                                );
                                return undecided;
                            }
                        }
                        tracing::trace!("filter: fast-track accepting api connection");
                        (Verdict::Accept, true)
                    }

                    53 => {
                        // Direct access to the Portmaster nameserver, UDP and TCP.
                        if self.matches_nameserver(dst) != Some(true) {
                            return undecided;
                        }
                        match self.env.is_my_ip(src) {
                            Ok(true) => {}
                            Ok(false) => return undecided,
                            Err(error) => {
                                tracing::debug!(
                                    "filter: failed to check if {src} is own IP for fast-track: {error}"
                                );
                                return undecided;
                            }
                        }
                        tracing::trace!("filter: fast-track accepting local dns");

                        // Add to DNS request connections to attribute the request.
                        if pkt.is_outbound() {
                            // Processing stops after fast-track, so take the
                            // PID from the packet directly.
                            conn.pid = meta.pid;
                            self.network
                                .save_dns_request_connection_locked(shared, conn, meta);
                        }

                        // Only permanent if the PID is known.
                        (Verdict::Accept, conn.pid != UNDEFINED_PROCESS_ID)
                    }

                    _ => self.fast_track_api_outbound(pkt, src, api),
                }
            }

            SYSTEM_INTEGRATION_CHECK_PROTOCOL => {
                if dst == IpAddr::V4(SYSTEM_INTEGRATION_CHECK_DST) {
                    self.env.submit_system_integration_check(pkt);
                    return (Verdict::Drop, false);
                }
                undecided
            }

            _ => undecided,
        }
    }

    /// The rare case that the UI connection to the API lost its verdict (e.g.
    /// cleared conntrack entries): packets from the API port to an own IP.
    fn fast_track_api_outbound(
        &self,
        pkt: &dyn QueuedPacket,
        src: IpAddr,
        api: Option<(IpAddr, u16)>,
    ) -> (Verdict, bool) {
        let meta = pkt.info();
        let undecided = (Verdict::Undecided, false);
        let Some((api_ip, api_port)) = api else {
            return undecided;
        };
        if meta.src_port != api_port
            || meta.protocol != protocol::TCP
            || src != canonical_ip(api_ip)
        {
            return undecided;
        }
        let Some(dst) = meta.dst else {
            return undecided;
        };
        if self.env.is_my_ip(canonical_ip(dst)).unwrap_or(false) {
            tracing::trace!("filter: fast-track accepting api-outbound packet");
            return (Verdict::Accept, true);
        }
        undecided
    }

    fn fast_track_handler(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        state: &mut Option<Handler>,
    ) {
        conn.save_when_finished = true;
        let (verdict, permanent) = self.fast_tracked_permit(shared, conn, pkt);
        if verdict != Verdict::Undecided {
            conn.verdict = verdict;

            // Apply the verdict to the (real) packet.
            if !pkt.info_only() {
                self.issue_verdict(conn, pkt, verdict, permanent);
            }

            if permanent {
                conn.set_verdict(verdict, "fast-tracked", "", None);
                // The verdict is not finalized, as data is missing.
                *state = None;
            }
            return;
        }

        // Not fast-tracked: gather more information.
        update(state, Handler::GatherData);
        self.gather_data_handler(shared, conn, pkt, state);
    }

    fn gather_data_handler(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        state: &mut Option<Handler>,
    ) {
        conn.save_when_finished = true;

        // Errors are informational and logged by the network module.
        self.network
            .gather_connection_info(conn, pkt.info(), pkt.info_only());

        // Run this handler again if the data is not complete yet.
        if !conn.data_complete {
            return;
        }

        match conn.ip_protocol {
            protocol::ICMP | protocol::ICMPV6 => {
                update(state, Handler::IcmpFilter);
                self.icmp_filter_handler(conn, pkt);
            }
            _ => {
                update(state, Handler::Filter);
                self.filter_handler(shared, conn, pkt, state);
            }
        }
    }

    fn filter_handler(
        &self,
        shared: &SharedConnection,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        state: &mut Option<Handler>,
    ) {
        conn.save_when_finished = true;

        // Skip if the data is not complete or the packet is info-only.
        if !conn.data_complete || pkt.info_only() {
            return;
        }

        let mut filter_connection = true;
        let mut check_tunnel = true;

        let entity_protocol = conn.entity.as_ref().map_or(0, |e| e.protocol);

        // Check for special (internal) connection cases.
        if !conn.inbound
            && self
                .preauth
                .is_pre_authenticated(entity_protocol, conn.local_port)
        {
            conn.accept("connection by Portmaster", NO_REASON_OPTION_KEY);
            conn.internal = true;
            filter_connection = false;
            tracing::info!("filter: granting own pre-authenticated connection {conn}");
        } else if !conn.inbound && self.is_own_split_tunnel_proxy_connection(conn) {
            conn.accept(
                "split tunnel connection proxied by Portmaster",
                NO_REASON_OPTION_KEY,
            );
            conn.internal = true;
            filter_connection = false;
            check_tunnel = false;
            tracing::info!(
                "filter: granting own pre-authenticated proxied split tunnel connection {conn}"
            );
        } else if self.is_rogue_dns_query(conn, pkt) {
            // Reroute rogue DNS queries back to the Portmaster.
            conn.set_verdict(
                Verdict::RerouteToNameserver,
                "redirecting rogue dns query",
                "",
                None,
            );
            conn.internal = true;
            tracing::info!("filter: redirecting dns query {conn} to Portmaster");

            // Add to DNS request connections to attribute the DNS request.
            self.network
                .save_dns_request_connection_locked(shared, conn, pkt.info());

            // No other processing is necessary.
            *state = None;
            self.issue_verdict(conn, pkt, Verdict::Undecided, true);
            return;
        }

        // Apply the privacy filter and check tunneling.
        self.filter_connection(conn, filter_connection, check_tunnel);

        // Decide how to continue handling the connection.
        if conn.inspecting && looks_like_outgoing_dns_request(conn) {
            self.inspect_dns_packet(shared, conn, pkt);
            update(state, Handler::InspectDns);
        } else if conn.inspecting {
            tracing::trace!("filter: start inspecting");
            update(state, Handler::InspectAndVerdict);
            self.inspect_and_verdict_handler(conn, pkt, state);
        } else {
            *state = None;
            self.verdict_handler(conn, pkt);
        }
    }

    /// Go `isOwnSplitTunnelProxyConnection`.
    fn is_own_split_tunnel_proxy_connection(&self, conn: &Connection) -> bool {
        // Proxies are running only in our own process.
        if conn.process.as_ref().map(|p| p.pid) != Some(self.env.own_pid()) {
            return false;
        }
        // Local connections are not proxied.
        if conn
            .entity
            .as_ref()
            .is_none_or(|entity| entity.ip_scope.is_localhost())
        {
            return false;
        }
        if conn.ip_protocol != protocol::TCP && conn.ip_protocol != protocol::UDP {
            return false;
        }
        self.env.is_proxied_split_tunnel_connection(conn)
    }

    /// The Go switch case for outbound DNS packets that should be redirected.
    fn is_rogue_dns_query(&self, conn: &Connection, pkt: &dyn QueuedPacket) -> bool {
        let info = pkt.info();
        if !(self.env.dns_query_interception()
            && !self.env.resolver_disabled()
            && pkt.is_outbound()
            && info.dst_port == 53)
        {
            return false;
        }
        // Packets to our own nameserver are fine.
        let Some(dst) = info.dst else {
            return false;
        };
        if self.matches_nameserver(dst) != Some(false) {
            return false;
        }
        // Broadcast queries by us are not redirected: unicast queries by the
        // resolver are pre-authenticated, the compat self-check is redirected.
        let own_multicast = conn.process.as_ref().map(|p| p.pid) == Some(self.env.own_pid())
            && conn
                .entity
                .as_ref()
                .is_some_and(|entity| entity.ip_scope == IpScope::LocalMulticast);
        !own_multicast
    }

    /// Go `FilterConnection`: runs the filtering and the tunnel checks.
    pub(super) fn filter_connection(
        &self,
        conn: &mut Connection,
        mut check_filter: bool,
        mut check_tunnel: bool,
    ) {
        if !conn.data_complete {
            return;
        }

        // Location data is always fetched for the UI, filtered or not.
        if let Some(entity) = conn.entity.as_mut() {
            entity.fetch_location();
        }

        // The external verdict handler can override the filter and tunnel flags.
        if let Some(external) = self.env.external_verdict(conn) {
            if matches!(external.verdict, Verdict::Accept | Verdict::Block) {
                conn.set_verdict(external.verdict, &external.reason, "", None);
                check_filter = false;
                check_tunnel = !external.skip_tunnel;
                tracing::info!(
                    "filter: special verdict {} {:?} {conn} for connection",
                    external.verdict,
                    external.reason
                );
            }
        }

        if check_filter {
            if self.env.filter_enabled() {
                tracing::trace!("filter: starting decision process");
                let profile = conn
                    .process
                    .clone()
                    .and_then(|process| self.profiles.layered_profile(&process));
                decide_on_connection(conn, profile.as_deref(), self.env.as_ref());
            } else {
                conn.accept("privacy filter disabled", NO_REASON_OPTION_KEY);
            }
        }

        // Quick fix of Go for the SPN: ports of encrypted protocols.
        if conn
            .entity
            .as_ref()
            .is_some_and(|entity| ENCRYPTED_PORTS.contains(&entity.dst_port()))
        {
            conn.encrypted = true;
        }

        // Check if the connection should be tunneled.
        if check_tunnel {
            self.env.check_tunneling(conn);

            // The SPN takes precedence over split tunneling.
            if conn.verdict != Verdict::RerouteToTunnel {
                self.env.check_split_tunneling(conn);
            }
        }

        // The SPN tunnel context is not ported, so a missing one is assumed.
        if conn.verdict == Verdict::RerouteToTunnel {
            match self.env.request_tunneling(conn) {
                Ok(()) => conn.connection_established = true,
                Err(error) => {
                    conn.failed(&format!("failed to request tunneling: {error}"), "");
                }
            }
        } else if conn.verdict == Verdict::RerouteToSplitTun {
            if let Err(error) = self.env.request_split_tunneling(conn) {
                conn.failed(
                    &format!("failed to request split-tunneling: {error}"),
                    CFG_OPTION_SPLIT_TUN_USE_KEY,
                );
            }
        }
    }

    /// Go `defaultFirewallHandler`.
    fn default_handler(&self, conn: &mut Connection, pkt: &dyn QueuedPacket) {
        match conn.ip_protocol {
            protocol::ICMP | protocol::ICMPV6 => self.icmp_filter_handler(conn, pkt),
            _ => self.verdict_handler(conn, pkt),
        }
    }

    fn verdict_handler(&self, conn: &mut Connection, pkt: &dyn QueuedPacket) {
        // Ignore info-only packets in this handler.
        if pkt.info_only() {
            return;
        }
        self.issue_verdict(conn, pkt, Verdict::Undecided, true);
    }

    fn inspect_and_verdict_handler(
        &self,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        state: &mut Option<Handler>,
    ) {
        if pkt.info_only() {
            return;
        }

        let (verdict, continue_inspection) = self.env.run_inspectors(conn, pkt);
        if continue_inspection {
            self.issue_verdict(conn, pkt, verdict, false);
            return;
        }

        // Inspection is done.
        *state = None;
        self.issue_verdict(conn, pkt, Verdict::Undecided, true);
    }

    fn icmp_filter_handler(&self, conn: &mut Connection, pkt: &dyn QueuedPacket) {
        let icmp = pkt.icmp_type();
        if icmp == IcmpType::LoadFailed {
            tracing::debug!("filter: failed to load ICMP packet data");
            self.issue_verdict(conn, pkt, Verdict::Drop, false);
            return;
        }

        // Submit to the ICMP listener.
        if self.env.submit_icmp(pkt) {
            self.issue_verdict(conn, pkt, Verdict::Drop, false);
            return;
        }

        // Echo requests and replies are handled regularly, other ICMP packets
        // are considered system business.
        // Like Go, a packet without ICMP layer continues as an echo packet.
        if matches!(icmp, IcmpType::Type(t) if !is_echo(pkt.info().protocol, t)) {
            self.issue_verdict(conn, pkt, Verdict::Accept, false);
            return;
        }

        // Check if we already have a verdict.
        if matches!(conn.verdict, Verdict::Undecided | Verdict::Undeterminable) {
            self.filter_connection(conn, true, false);
            conn.save_when_finished = true;
        }

        // Outbound direction has priority: change from inbound to outbound on
        // the first outbound ICMP packet.
        if conn.inbound && conn.ended == 0 && pkt.is_outbound() {
            conn.inbound = false;
            self.filter_connection(conn, true, false);
            conn.save_when_finished = true;
        }

        self.issue_verdict(conn, pkt, Verdict::Undecided, false);
    }

    /// Go `issueVerdict`.
    pub(super) fn issue_verdict(
        &self,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
        verdict: Verdict,
        allow_permanent: bool,
    ) {
        // The OS integration already decided.
        if pkt.fast_tracked_by_integration() {
            return;
        }

        // Enable permanent verdicts.
        if allow_permanent && !conn.verdict_permanent && self.env.permanent_verdicts() {
            conn.verdict_permanent = true;
            conn.save_when_finished = true;
        }

        // Do not allow to circumvent the decision, e.g. accepting packets of a
        // dropped connection.
        let verdict = verdict.max(conn.verdict);

        let permanent = conn.verdict_permanent;
        let pkt_verdict = match verdict {
            Verdict::Accept => {
                self.counters.accepted.fetch_add(1, Ordering::Relaxed);
                if !permanent {
                    PktVerdict::Accept
                } else if !conn.inbound && conn.pid == self.env.own_pid() {
                    // Own outbound connections are finalized so that third
                    // party OUTPUT rules cannot override the decision.
                    PktVerdict::PermanentAcceptFinal
                } else {
                    PktVerdict::PermanentAccept
                }
            }
            Verdict::Block => {
                self.counters.blocked.fetch_add(1, Ordering::Relaxed);
                if permanent {
                    PktVerdict::PermanentBlock
                } else {
                    PktVerdict::Block
                }
            }
            Verdict::Drop => {
                self.counters.dropped.fetch_add(1, Ordering::Relaxed);
                if permanent {
                    PktVerdict::PermanentDrop
                } else {
                    PktVerdict::Drop
                }
            }
            Verdict::RerouteToNameserver => PktVerdict::RerouteToNameserver,
            Verdict::RerouteToTunnel => PktVerdict::RerouteToTunnel,
            Verdict::RerouteToSplitTun => PktVerdict::RerouteToSplitTun,
            Verdict::Failed => {
                self.counters.failed.fetch_add(1, Ordering::Relaxed);
                PktVerdict::Drop
            }
            Verdict::Undecided | Verdict::Undeterminable => {
                tracing::warn!("filter: tried to apply verdict {verdict} to pkt: dropping instead");
                self.counters.dropped.fetch_add(1, Ordering::Relaxed);
                PktVerdict::Drop
            }
        };

        if let Err(error) = pkt.set_verdict(pkt_verdict) {
            tracing::warn!("filter: failed to apply verdict to pkt: {error}");
        }
    }
}

/// Replaces the handler only if the connection has one (Go
/// `UpdateFirewallHandler`).
fn update(state: &mut Option<Handler>, handler: Handler) {
    if state.is_some() {
        *state = Some(handler);
    }
}

fn blocked_ipv6() -> Ipv6Addr {
    Ipv6Addr::new(0, 0, 0, 0, 0, 0, 0, 0x17)
}

/// Whether the ICMP type is an echo request or reply of the protocol.
fn is_echo(protocol: u8, icmp_type: u8) -> bool {
    match protocol {
        protocol::ICMPV6 => matches!(icmp_type, 128 | 129),
        _ => matches!(icmp_type, 8 | 0),
    }
}
