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

use crate::{
    BestPhysicalDefaultInterfaces, InterfaceEnv, InterfaceInfo, Proxies, RequestQueue,
};

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

/// [`InterfaceEnv`] backed by the `pm-netenv` interface and address caches.
pub struct SystemInterfaceEnv {
    interfaces: Arc<pm_netenv::InterfaceCache>,
    addresses: Arc<pm_netenv::AddressCache>,
}

impl SystemInterfaceEnv {
    /// Creates the environment from the network caches.
    pub fn new(
        interfaces: Arc<pm_netenv::InterfaceCache>,
        addresses: Arc<pm_netenv::AddressCache>,
    ) -> SystemInterfaceEnv {
        SystemInterfaceEnv {
            interfaces,
            addresses,
        }
    }
}

impl InterfaceEnv for SystemInterfaceEnv {
    fn ipv6_enabled(&self) -> bool {
        self.addresses
            .get_assigned_addresses()
            .map(|(_ipv4, ipv6)| !ipv6.is_empty())
            .unwrap_or(false)
    }

    fn get_interface(&self, identifier: &str) -> Result<InterfaceInfo, String> {
        self.interfaces
            .get_interface(identifier)
            .map(|info| InterfaceInfo {
                name: info.interface.name,
                ipv4: info.ipv4,
                ipv6: info.ipv6,
            })
    }

    fn best_physical_default_interfaces(&self) -> Result<BestPhysicalDefaultInterfaces, String> {
        let best = self.interfaces.best_physical_default_interfaces()?;
        Ok(BestPhysicalDefaultInterfaces {
            for_ipv4: best.for_ipv4.map(|info| InterfaceInfo {
                name: info.interface.name,
                ipv4: info.ipv4,
                ipv6: info.ipv6,
            }),
            for_ipv6: best.for_ipv6.map(|info| InterfaceInfo {
                name: info.interface.name,
                ipv4: info.ipv4,
                ipv6: info.ipv6,
            }),
        })
    }
}
