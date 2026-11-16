//! Split tunneling verdicts, port of Go `service/firewall/split-tunnel.go`.
//!
//! The split tunnel module itself (`service/splittun`, the proxy in
//! `pm-proxy`) is injected through [`SplitTunnelEnv`], because the firewall only
//! decides whether a connection is rerouted, blocked or left alone.

use pm_endpoints::EpResult;
use pm_network::{protocol, Connection, SplitTunContext, Verdict};

/// Gates that the firewall reads from the split tunnel module and the layered
/// profile of the connection.
pub trait SplitTunnelEnv: Send + Sync {
    /// PID of the Portmaster core.
    fn own_pid(&self) -> i32;

    /// Whether the split tunnel proxy is ready to accept connections.
    fn is_ready(&self) -> bool;

    /// Go `splittun.IsProxiedConnectionInfo`.
    fn is_proxied_connection_info(&self, conn: &Connection) -> bool;

    /// Returns the layered profile revision of the connection, updating it when
    /// the profile is outdated, like the first block of Go
    /// `checkSplitTunneling`.
    ///
    /// Returns `None` when the process has no profile; the connection is then
    /// marked as failed.
    fn profile_revision(&self, conn: &Connection) -> Option<u64>;

    /// Go `LayeredProfile.UseSplitTun`.
    fn use_split_tun(&self, conn: &Connection) -> bool;

    /// Go `LayeredProfile.SplitTunProxy`.
    fn split_tun_proxy(&self, conn: &Connection) -> String;

    /// Go `LayeredProfile.SplitTunInterface`.
    fn split_tun_interface(&self, conn: &Connection) -> String;

    /// Matches the connection's entity against the split tunnel usage policy.
    ///
    /// The implementation must fetch the entity data first, like Go
    /// `conn.Entity.FetchData`.
    fn match_split_tun_usage_policy(&self, conn: &mut Connection) -> EpResult;

    /// Queues the connection in the split tunnel module, like Go
    /// `splittun.AwaitRequest`.
    fn await_request(
        &self,
        conn: &Connection,
        interface: &str,
        upstream_proxy: &str,
    ) -> Result<SplitTunContext, String>;
}

/// Decides whether the connection is rerouted through the split tunnel.
///
/// Mirrors `checkSplitTunneling`: the verdict is left untouched unless the
/// connection is accepted, not inbound, not localhost, TCP/UDP and not created
/// by the core itself.
pub fn check_split_tunneling(conn: &mut Connection, env: &dyn SplitTunnelEnv) {
    let is_localhost = conn
        .entity
        .as_ref()
        .is_some_and(|entity| entity.ip_scope.is_localhost());

    if is_localhost
        || conn.inbound
        || conn.verdict != Verdict::Accept
        || (conn.ip_protocol != protocol::TCP && conn.ip_protocol != protocol::UDP)
        || conn.pid == env.own_pid()
        || !env.is_ready()
    {
        return;
    }

    // The process must have a layered profile.
    let Some(revision) = env.profile_revision(conn) else {
        conn.failed("no profile set", "");
        return;
    };
    if conn.profile_revision_counter != revision {
        conn.profile_revision_counter = revision;
        conn.save_when_finished = true;
    }

    // Split tunneling must be enabled for this app.
    if !env.use_split_tun(conn) {
        return;
    }

    match env.match_split_tun_usage_policy(conn) {
        EpResult::MatchError => {
            conn.failed(
                "failed to check Split Tunnel rules",
                pm_profile::options::CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY,
            );
            return;
        }
        EpResult::Denied => return,
        EpResult::Permitted | EpResult::NoMatch => {}
    }

    // UDP cannot be relayed through the upstream proxy; block it instead of
    // letting it bypass the proxy.
    if conn.ip_protocol == protocol::UDP && !env.split_tun_proxy(conn).is_empty() {
        conn.block(
            "UDP cannot be relayed through the Split Tunnel proxy",
            pm_profile::options::CFG_OPTION_SPLIT_TUN_PROXY_KEY,
        );
        return;
    }

    conn.save_when_finished = true;
    conn.verdict = Verdict::RerouteToSplitTun;
}

/// Queues the connection in the split tunnel module.
///
/// Mirrors Go `requestSplitTunneling`; the returned context is stored on the
/// connection so the UI can display the interface.
pub fn request_split_tunneling(
    conn: &mut Connection,
    env: &dyn SplitTunnelEnv,
) -> Result<(), String> {
    if conn.process.is_none() {
        return Err("no profile set".to_string());
    }

    let interface = env.split_tun_interface(conn);
    let upstream_proxy = env.split_tun_proxy(conn);
    let context = env.await_request(conn, &interface, &upstream_proxy)?;
    conn.split_tun_context = Some(context);
    Ok(())
}

/// Returns whether the connection is a proxied connection of the split tunnel
/// proxy that runs in the core process.
pub fn is_own_split_tunnel_proxy_connection(conn: &Connection, env: &dyn SplitTunnelEnv) -> bool {
    if conn.pid != env.own_pid()
        || conn.ip_protocol != protocol::TCP && conn.ip_protocol != protocol::UDP
        || !env.is_ready()
    {
        return false;
    }
    if conn
        .entity
        .as_ref()
        .is_some_and(|entity| entity.ip_scope.is_localhost())
    {
        return false;
    }

    env.is_proxied_connection_info(conn)
}
