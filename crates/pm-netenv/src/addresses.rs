//! Local address bookkeeping, port of Go `service/netenv/adresses.go`.
//!
//! The host's interface networks are cached and refreshed when the network
//! change flag fires. `is_my_ip` follows the Go two-pass algorithm: an exact
//! address match is answered first, then network containment, and only then is
//! the cache refreshed once more before giving up.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};

use pm_netstate::{ip_scope, IpScope};

use crate::interfaces::{network_contains, normalize_ip, InterfaceSource};
use crate::NetworkChangeFlag;

/// A local network with its prefix length.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LocalNetwork {
    /// Network address.
    pub network: IpAddr,
    /// Prefix length.
    pub prefix_len: u8,
}

impl LocalNetwork {
    /// Returns whether the IP is inside the network.
    pub fn contains(&self, ip: IpAddr) -> bool {
        network_contains(self.network, self.prefix_len, ip)
    }
}

/// Cache of the addresses assigned to the host.
pub struct AddressCache {
    source: Arc<dyn InterfaceSource>,
    flag: NetworkChangeFlag,
    refresh_interval: Duration,
    state: RwLock<AddressState>,
}

#[derive(Default)]
struct AddressState {
    networks: Vec<(IpAddr, u8)>,
    last_refresh: Option<Instant>,
    last_error: Option<String>,
}

impl AddressCache {
    /// Creates a cache with the default one-second refresh throttle.
    pub fn new(source: Arc<dyn InterfaceSource>, flag: NetworkChangeFlag) -> AddressCache {
        AddressCache::with_refresh_interval(source, flag, Duration::from_secs(1))
    }

    /// Creates a cache with an explicit refresh throttle.
    pub fn with_refresh_interval(
        source: Arc<dyn InterfaceSource>,
        flag: NetworkChangeFlag,
        refresh_interval: Duration,
    ) -> AddressCache {
        AddressCache {
            source,
            flag,
            refresh_interval,
            state: RwLock::new(AddressState::default()),
        }
    }

    /// Returns all assigned IPv4 and IPv6 addresses, like Go
    /// `GetAssignedAddresses`.
    pub fn get_assigned_addresses(&self) -> Result<(Vec<Ipv4Addr>, Vec<Ipv6Addr>), String> {
        let interfaces = self.source.interfaces()?;
        let mut ipv4 = Vec::new();
        let mut ipv6 = Vec::new();
        for interface in interfaces {
            for (ip, _prefix) in interface.addrs {
                match normalize_ip(ip) {
                    IpAddr::V4(ip) => ipv4.push(ip),
                    IpAddr::V6(ip) => ipv6.push(ip),
                }
            }
        }
        Ok((ipv4, ipv6))
    }

    /// Returns the globally routable assigned addresses, like Go
    /// `GetAssignedGlobalAddresses`.
    pub fn get_assigned_global_addresses(
        &self,
    ) -> Result<(Vec<Ipv4Addr>, Vec<Ipv6Addr>), String> {
        let (all_v4, all_v6) = self.get_assigned_addresses()?;
        let ipv4 = all_v4
            .into_iter()
            .filter(|ip| ip_scope(IpAddr::V4(*ip)).is_global())
            .collect();
        let ipv6 = all_v6
            .into_iter()
            .filter(|ip| ip_scope(IpAddr::V6(*ip)).is_global())
            .collect();
        Ok((ipv4, ipv6))
    }

    /// Returns whether the given unicast IP is configured on this host.
    pub fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String> {
        match ip_scope(ip) {
            IpScope::HostLocal => return Ok(true),
            IpScope::LocalMulticast | IpScope::GlobalMulticast => return Ok(false),
            _ => {}
        }

        if self.needs_refresh() {
            self.refresh()?;
        }
        let mut networks = self.snapshot();

        for (network, _) in &networks {
            if *network == ip {
                return Ok(true);
            }
        }
        for (network, prefix) in &networks {
            if network_contains(*network, *prefix, ip) {
                return Ok(false);
            }
        }

        // Could not find the IP anywhere: refresh once more to be sure.
        self.refresh()?;
        networks = self.snapshot();
        Ok(networks.iter().any(|(network, _)| *network == ip))
    }

    /// Returns the local network the IP belongs to, if any.
    pub fn get_local_network(&self, ip: IpAddr) -> Result<Option<LocalNetwork>, String> {
        if self.needs_refresh() {
            self.refresh()?;
        }
        let networks = self.snapshot();
        Ok(networks
            .iter()
            .find(|(network, prefix)| network_contains(*network, *prefix, ip))
            .map(|(network, prefix)| LocalNetwork {
                network: *network,
                prefix_len: *prefix,
            }))
    }

    fn needs_refresh(&self) -> bool {
        if self.flag.is_set() {
            return true;
        }
        let state = self.state.read().unwrap_or_else(|error| error.into_inner());
        state.networks.is_empty() && state.last_refresh.is_none() || state.last_error.is_some()
    }

    fn refresh(&self) -> Result<(), String> {
        let mut state = self.state.write().unwrap_or_else(|error| error.into_inner());

        if state
            .last_refresh
            .is_some_and(|last| last.elapsed() < self.refresh_interval)
        {
            if let Some(error) = &state.last_error {
                return Err(format!(
                    "failed to previously refresh interface addresses: {error}"
                ));
            }
            return Ok(());
        }

        self.flag.refresh();
        let interfaces = match self.source.interfaces() {
            Ok(interfaces) => interfaces,
            Err(error) => {
                state.last_error = Some(error.clone());
                state.last_refresh = Some(Instant::now());
                return Err(format!("failed to refresh interface addresses: {error}"));
            }
        };

        state.networks = interfaces
            .into_iter()
            .flat_map(|interface| interface.addrs)
            .map(|(ip, prefix)| (normalize_ip(ip), prefix))
            .collect();
        state.last_error = None;
        state.last_refresh = Some(Instant::now());
        Ok(())
    }

    fn snapshot(&self) -> Vec<(IpAddr, u8)> {
        self.state
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .networks
            .clone()
    }
}
