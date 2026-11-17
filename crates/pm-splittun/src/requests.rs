//! Pending split tunnel requests, port of `service/splittun/requests.go`.
//!
//! The firewall reroutes a connection, then registers its local address here.
//! When the redirected connection arrives at the proxy, the proxy decider
//! consumes the pending request and reads the destination and binding from it.

use std::collections::HashMap;
use std::net::{IpAddr, SocketAddr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use pm_network::version;
use pm_network::{Connection, JsonIp, SplitTunContext};
use pm_proxy::{LocalBinding, UpstreamProxy};
use tokio_util::sync::CancellationToken;

/// Port the split tunnel proxies listen on.
pub const SPLIT_TUN_PORT: u16 = 719;

/// How long a pending request waits for the proxy to accept the connection.
pub const PENDING_REQUEST_TTL: Duration = Duration::from_secs(30);

/// IPv4 and IPv6 addresses of a network interface.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct InterfaceInfo {
    /// Name of the interface, e.g. `eth0`.
    pub name: String,
    /// IPv4 address of the interface.
    pub ipv4: Option<IpAddr>,
    /// IPv6 address of the interface.
    pub ipv6: Option<IpAddr>,
}

/// The best physical default interfaces per IP version.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BestPhysicalDefaultInterfaces {
    pub for_ipv4: Option<InterfaceInfo>,
    pub for_ipv6: Option<InterfaceInfo>,
}

/// Network environment lookups needed by the split tunnel module.
pub trait InterfaceEnv: Send + Sync {
    /// Whether IPv6 is enabled on this system, like Go `netenv.IPv6Enabled`.
    fn ipv6_enabled(&self) -> bool;
    /// Resolves an interface by name, IP or MAC address.
    fn get_interface(&self, identifier: &str) -> Result<InterfaceInfo, String>;
    /// Returns the best physical default interfaces, like Go
    /// `netenv.GetBestPhysicalDefaultInterfaces`.
    fn best_physical_default_interfaces(&self) -> Result<BestPhysicalDefaultInterfaces, String>;
}

/// A request waiting for its redirected connection to reach the proxy.
#[derive(Debug, Clone)]
pub struct PendingRequest {
    /// Destination IP address of the connection.
    pub dest_ip: IpAddr,
    /// Destination port of the connection.
    pub dest_port: u16,
    /// Local binding parameters for the outgoing proxy connection.
    pub binding: LocalBinding,
    /// Entity domain, used by upstream proxies that send host names.
    pub domain: String,
    /// When the request expires.
    pub expires_at: Instant,
}

/// Known-pending requests, keyed by `local IP:port`.
#[derive(Debug, Default)]
pub struct RequestQueue {
    /// Shared with the deferred cleanup task so it can sweep without keeping
    /// the whole queue alive.
    pending: Arc<Mutex<HashMap<String, PendingRequest>>>,
    /// Guards against spawning more than one cleanup task, like Go's
    /// `cleanupScheduled` atomic.
    cleanup_scheduled: Arc<AtomicBool>,
    /// Cancels the cleanup task when the module shuts down.
    cleanup_token: CancellationToken,
}

impl RequestQueue {
    /// Creates an empty queue.
    pub fn new() -> RequestQueue {
        RequestQueue::default()
    }

    /// Registers a connection for handling when it arrives at the proxy.
    ///
    /// `bind_interface` may be an interface name, a local IP or a MAC address;
    /// an empty value detects the best physical default interface, or uses the
    /// default routing when an upstream proxy is set.
    pub fn await_request(
        &self,
        conn: &Connection,
        bind_interface: &str,
        upstream_proxy: &str,
        env: &dyn InterfaceEnv,
    ) -> Result<SplitTunContext, String> {
        let mut binding = LocalBinding::default();
        let mut ip_version = conn.ip_version;

        if !upstream_proxy.is_empty() {
            // Use the Go-compatible validation first: it reports the messages
            // the UI knows, including credential checks.
            pm_profile::validate_upstream_proxy_url(upstream_proxy)?;
            let mut upstream = UpstreamProxy::parse(upstream_proxy)?;
            if let Some(entity) = &conn.entity {
                if !entity.domain.is_empty() {
                    upstream.target_host_override = Some(entity.domain.clone());
                }
            }
            if let Ok(ip) = upstream.host.parse::<IpAddr>() {
                ip_version = if ip.is_ipv4() { version::V4 } else { version::V6 };
            }
            binding = binding.with_upstream(upstream);
        }

        if bind_interface.is_empty() && binding.upstream.is_some() {
            // Reach the proxy server via the default routing.
        } else if bind_interface.is_empty() {
            let interfaces = env.best_physical_default_interfaces()?;
            let selected = match ip_version {
                version::V6 => interfaces.for_ipv6,
                version::V4 => interfaces.for_ipv4,
                _ => interfaces.for_ipv4.or(interfaces.for_ipv6),
            };
            let Some(selected) = selected else {
                return Err(format!(
                    "no suitable default physical interface found for {}",
                    ip_version_name(ip_version)
                ));
            };
            binding.ip = if ip_version == version::V6 {
                selected.ipv6
            } else {
                selected.ipv4
            };
            binding.interface = Some(selected.name);
        } else {
            let info = env.get_interface(bind_interface)?;
            let (ip, alt_ip) = if ip_version == version::V6 {
                (info.ipv6, info.ipv4)
            } else {
                (info.ipv4, info.ipv6)
            };
            binding.ip = ip;
            if binding.upstream.is_some() {
                // A proxy hostname may resolve to addresses of either version,
                // so provide the address of the other version too.
                binding.alt_ip = alt_ip;
                if binding.ip.is_none() {
                    binding.ip = binding.alt_ip.take();
                }
            }
            if binding.ip.is_none() {
                return Err(format!(
                    "interface {bind_interface:?} has no usable address for {}",
                    ip_version_name(ip_version)
                ));
            }
            binding.interface = Some(info.name);
        }

        let Some(local_ip) = conn.local_ip else {
            return Err("connection has no local IP".to_string());
        };
        let key = SocketAddr::new(local_ip, conn.local_port).to_string();

        let Some(entity) = &conn.entity else {
            return Err("connection has no entity".to_string());
        };
        let Some(entity_ip) = entity.ip else {
            return Err("connection entity has no IP".to_string());
        };

        let mut pending = self.pending.lock().unwrap_or_else(|error| error.into_inner());
        if let Some(existing) = pending.get(&key) {
            if existing.expires_at > Instant::now() {
                return Err(format!("a pending request for {key} already exists"));
            }
            pending.remove(&key);
        }

        pending.insert(
            key,
            PendingRequest {
                dest_ip: entity_ip,
                dest_port: entity.port,
                binding: binding.clone(),
                domain: entity.domain.clone(),
                expires_at: Instant::now() + PENDING_REQUEST_TTL,
            },
        );
        drop(pending);

        // Sweep expired entries in the background, like Go `scheduleCleanup`.
        self.spawn_cleanup(self.cleanup_token.clone());

        let context = SplitTunContext {
            interface: binding.interface.clone().unwrap_or_default(),
            ip: JsonIp(binding.ip),
            proxy: binding
                .upstream
                .as_ref()
                .map(|upstream| redacted_upstream(upstream))
                .unwrap_or_default(),
        };
        Ok(context)
    }

    /// Retrieves and removes a pending request for the given address.
    pub fn consume(&self, address: &str) -> Result<PendingRequest, String> {
        self.consume_at(address, Instant::now())
    }

    /// Retrieves and removes a pending request, using the given time.
    ///
    /// The explicit time makes expiry testable without sleeping.
    pub fn consume_at(&self, address: &str, now: Instant) -> Result<PendingRequest, String> {
        let mut pending = self.pending.lock().unwrap_or_else(|error| error.into_inner());
        let Some(request) = pending.remove(address) else {
            return Err(format!("no pending request for {address}"));
        };
        if now > request.expires_at {
            return Err(format!("pending request for {address} has expired"));
        }
        Ok(request)
    }

    /// Removes all expired requests.
    pub fn sweep(&self) {
        self.sweep_at(Instant::now());
    }

    /// Removes all requests that expired before `now`.
    pub fn sweep_at(&self, now: Instant) {
        let mut pending = self.pending.lock().unwrap_or_else(|error| error.into_inner());
        pending.retain(|_, request| request.expires_at > now);
    }

    /// Removes all pending requests, like Go `clearPendingRequests`.
    pub fn clear(&self) {
        self.pending
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clear();
    }

    /// Returns the number of pending requests.
    pub fn len(&self) -> usize {
        self.pending
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .len()
    }

    /// Returns whether no requests are pending.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Schedules the deferred cleanup task, like Go `scheduleCleanup`.
    ///
    /// Only one cleanup task runs at a time. It wakes after
    /// `PENDING_REQUEST_TTL + 1s`, sweeps expired entries and keeps running
    /// while requests remain. Cancelling `token` stops it early. Scheduling is
    /// skipped when no Tokio runtime is active (e.g. in synchronous tests).
    pub fn spawn_cleanup(&self, token: CancellationToken) {
        if self
            .cleanup_scheduled
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .is_err()
        {
            return; // already scheduled; that task will sweep our entry too
        }

        let Ok(handle) = tokio::runtime::Handle::try_current() else {
            self.cleanup_scheduled.store(false, Ordering::SeqCst);
            return;
        };

        let pending = Arc::clone(&self.pending);
        let scheduled = Arc::clone(&self.cleanup_scheduled);
        let _task = handle.spawn(async move {
            loop {
                let sleep = PENDING_REQUEST_TTL + Duration::from_secs(1);
                let cancelled = tokio::select! {
                    () = token.cancelled() => true,
                    () = tokio::time::sleep(sleep) => false,
                };
                if cancelled {
                    scheduled.store(false, Ordering::SeqCst);
                    return;
                }

                let now = Instant::now();
                {
                    let mut pending = pending
                        .lock()
                        .unwrap_or_else(|error| error.into_inner());
                    pending.retain(|_, request| request.expires_at > now);

                    if pending.is_empty() {
                        // Clear the guard while still holding the lock: a
                        // concurrent registration then either is seen here or
                        // gets to schedule a fresh task after the reset.
                        scheduled.store(false, Ordering::SeqCst);
                        return;
                    }
                }
            }
        });
    }
}

/// Returns the Go-style name of an IP version.
fn ip_version_name(version: u8) -> &'static str {
    match version {
        version::V4 => "IPv4",
        version::V6 => "IPv6",
        _ => "unknown IP version",
    }
}

/// Returns `scheme://host:port`, like Go `UpstreamProxy.Redacted`.
fn redacted_upstream(upstream: &UpstreamProxy) -> String {
    let scheme = match upstream.scheme {
        pm_proxy::UpstreamScheme::Http => "http",
        pm_proxy::UpstreamScheme::Socks4 => "socks4",
        pm_proxy::UpstreamScheme::Socks4a => "socks4a",
        pm_proxy::UpstreamScheme::Socks5 => "socks5",
        pm_proxy::UpstreamScheme::Socks5h => "socks5h",
    };
    format!("{scheme}://{}:{}", upstream.host, upstream.port)
}
