//! Adapter between the split tunnel module and the firewall.
//!
//! The firewall decides whether a connection is rerouted; this adapter gives it
//! the split tunnel side: readiness, pending requests, proxy egress detection
//! and the per-process profile settings.

use std::sync::Arc;

use pm_endpoints::EpResult;
use pm_firewall::SplitTunnelEnv;
use pm_intel::Entity;
use pm_network::{Connection, SplitTunContext};
use pm_process::Process;

use crate::{InterfaceEnv, Proxies, RequestQueue};

/// Per-process profile settings the split tunnel decision needs.
pub trait SplitTunnelProfiles: Send + Sync {
    /// Returns the layered profile revision, updating the layers first when
    /// the profile is outdated. `None` means the process has no profile.
    fn profile_revision(&self, process: &Process) -> Option<u64>;
    /// Go `LayeredProfile.UseSplitTun`.
    fn use_split_tun(&self, process: &Process) -> bool;
    /// Go `LayeredProfile.SplitTunProxy`.
    fn split_tun_proxy(&self, process: &Process) -> String;
    /// Go `LayeredProfile.SplitTunInterface`.
    fn split_tun_interface(&self, process: &Process) -> String;
    /// Matches the entity against the split tunnel usage policy.
    ///
    /// The entity data is already fetched by the adapter.
    fn match_split_tun_usage_policy(&self, process: &Process, entity: &mut Entity) -> EpResult;
}

/// Split tunnel state exposed to the firewall.
pub struct SplitTunnelEnvAdapter {
    own_pid: i32,
    queue: Arc<RequestQueue>,
    proxies: Arc<Proxies>,
    interfaces: Arc<dyn InterfaceEnv>,
    profiles: Arc<dyn SplitTunnelProfiles>,
}

impl SplitTunnelEnvAdapter {
    /// Creates the adapter.
    pub fn new(
        own_pid: i32,
        queue: Arc<RequestQueue>,
        proxies: Arc<Proxies>,
        interfaces: Arc<dyn InterfaceEnv>,
        profiles: Arc<dyn SplitTunnelProfiles>,
    ) -> SplitTunnelEnvAdapter {
        SplitTunnelEnvAdapter {
            own_pid,
            queue,
            proxies,
            interfaces,
            profiles,
        }
    }

    /// Returns the request queue of the module.
    pub fn queue(&self) -> &Arc<RequestQueue> {
        &self.queue
    }

    /// Returns the proxies of the module.
    pub fn proxies(&self) -> &Arc<Proxies> {
        &self.proxies
    }
}

impl SplitTunnelEnv for SplitTunnelEnvAdapter {
    fn own_pid(&self) -> i32 {
        self.own_pid
    }

    fn is_ready(&self) -> bool {
        self.proxies.is_ready()
    }

    fn is_proxied_connection_info(&self, conn: &Connection) -> bool {
        self.proxies.is_proxied_connection_info(conn)
    }

    fn profile_revision(&self, conn: &Connection) -> Option<u64> {
        let process = conn.process.as_ref()?;
        self.profiles.profile_revision(process)
    }

    fn use_split_tun(&self, conn: &Connection) -> bool {
        conn.process
            .as_ref()
            .is_some_and(|process| self.profiles.use_split_tun(process))
    }

    fn split_tun_proxy(&self, conn: &Connection) -> String {
        conn.process
            .as_ref()
            .map(|process| self.profiles.split_tun_proxy(process))
            .unwrap_or_default()
    }

    fn split_tun_interface(&self, conn: &Connection) -> String {
        conn.process
            .as_ref()
            .map(|process| self.profiles.split_tun_interface(process))
            .unwrap_or_default()
    }

    fn match_split_tun_usage_policy(&self, conn: &mut Connection) -> EpResult {
        // Go fetches the entity data before matching the policy.
        if let Some(entity) = conn.entity.as_mut() {
            entity.fetch_data();
        }
        let (Some(process), Some(entity)) = (conn.process.as_ref(), conn.entity.as_mut()) else {
            return EpResult::NoMatch;
        };
        self.profiles
            .match_split_tun_usage_policy(process, entity)
    }

    fn await_request(
        &self,
        conn: &Connection,
        interface: &str,
        upstream_proxy: &str,
    ) -> Result<SplitTunContext, String> {
        self.queue
            .await_request(conn, interface, upstream_proxy, self.interfaces.as_ref())
    }
}
