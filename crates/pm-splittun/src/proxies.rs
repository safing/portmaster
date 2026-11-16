//! Split tunnel proxy lifecycle, port of `service/splittun/proxies.go`.
//!
//! The firewall reroutes accepted connections into these proxies; each arriving
//! connection is matched against the pending requests of [`RequestQueue`].
//!
//! Go tracks the egress address of every proxy session and uses it to recognize
//! the proxy's own connections. That session cache is not ported yet:
//! [`Proxies::is_proxied_connection_info`] always returns `false`.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use pm_network::{protocol, Connection};
use pm_proxy::decider::Decision;
use pm_proxy::{
    SharedDecider, TcpProxy, TcpProxyConfig, UdpProxy, UdpProxyConfig,
};

use crate::{InterfaceEnv, RequestQueue, SPLIT_TUN_PORT};

/// The running split tunnel proxies.
#[derive(Default)]
pub struct Proxies {
    tcp4: Mutex<Option<Arc<TcpProxy>>>,
    tcp6: Mutex<Option<Arc<TcpProxy>>>,
    udp4: Mutex<Option<Arc<UdpProxy>>>,
    udp6: Mutex<Option<Arc<UdpProxy>>>,
    tasks: Mutex<Vec<tokio::task::JoinHandle<()>>>,
    ready: AtomicBool,
}

impl Proxies {
    /// Creates an empty, not running set of proxies.
    pub fn new() -> Proxies {
        Proxies::default()
    }

    /// Whether the proxies are running, like Go `splittun.IsReady`.
    pub fn is_ready(&self) -> bool {
        self.ready.load(Ordering::SeqCst)
    }

    /// Starts the proxies on the default split tunnel port.
    pub async fn start(
        &self,
        queue: Arc<RequestQueue>,
        env: &dyn InterfaceEnv,
    ) -> Result<(), String> {
        self.start_on(SPLIT_TUN_PORT, queue, env).await
    }

    /// Starts the proxies on the given port; `0` selects a free port.
    ///
    /// Ports follow Go: TCP listens on the wildcard address, UDP on
    /// `127.0.0.17`/`::1` on Linux so that conntrack's reverse NAT sees the
    /// redirected destination, and on the wildcard address elsewhere.
    pub async fn start_on(
        &self,
        port: u16,
        queue: Arc<RequestQueue>,
        env: &dyn InterfaceEnv,
    ) -> Result<(), String> {
        self.stop();

        let decider: SharedDecider = Arc::new(move |_local: SocketAddr, peer: SocketAddr| {
            let request = queue.consume(&peer.to_string())?;
            Ok(Decision::new(request.dest_ip, request.dest_port).with_binding(request.binding))
        });

        let udp4_ip = if cfg!(target_os = "linux") {
            IpAddr::V4(Ipv4Addr::new(127, 0, 0, 17))
        } else {
            IpAddr::V4(Ipv4Addr::UNSPECIFIED)
        };
        let udp6_ip = if cfg!(target_os = "linux") {
            IpAddr::V6(Ipv6Addr::LOCALHOST)
        } else {
            IpAddr::V6(Ipv6Addr::UNSPECIFIED)
        };

        let result: Result<(), String> = async {
            let tcp4 = TcpProxy::bind(
                SocketAddr::from((Ipv4Addr::UNSPECIFIED, port)),
                Arc::clone(&decider),
                TcpProxyConfig::default(),
            )
            .await
            .map_err(|error| format!("failed to start TCPv4 proxy: {error}"))?;
            let udp4 = UdpProxy::bind(
                SocketAddr::new(udp4_ip, port),
                Arc::clone(&decider),
                UdpProxyConfig::default(),
            )
            .await
            .map_err(|error| format!("failed to start UDPv4 proxy: {error}"))?;

            let (tcp6, udp6) = if env.ipv6_enabled() {
                let tcp6 = TcpProxy::bind(
                    SocketAddr::from((Ipv6Addr::UNSPECIFIED, port)),
                    Arc::clone(&decider),
                    TcpProxyConfig::default(),
                )
                .await
                .map_err(|error| format!("failed to start TCPv6 proxy: {error}"))?;
                let udp6 = UdpProxy::bind(
                    SocketAddr::new(udp6_ip, port),
                    Arc::clone(&decider),
                    UdpProxyConfig::default(),
                )
                .await
                .map_err(|error| format!("failed to start UDPv6 proxy: {error}"))?;
                (Some(tcp6), Some(udp6))
            } else {
                (None, None)
            };

            self.install(tcp4, udp4, tcp6, udp6);
            Ok(())
        }
        .await;

        if result.is_err() {
            self.stop();
        }
        result
    }

    /// Stores the proxies and spawns their accept loops.
    fn install(&self, tcp4: TcpProxy, udp4: UdpProxy, tcp6: Option<TcpProxy>, udp6: Option<UdpProxy>) {
        let mut tasks = Vec::new();

        let tcp4 = Arc::new(tcp4);
        let runner = Arc::clone(&tcp4);
        tasks.push(tokio::spawn(async move {
            if let Err(error) = runner.run().await {
                tracing::error!("splittun: TCPv4 proxy stopped: {error}");
            }
        }));
        *self.tcp4.lock().unwrap_or_else(|error| error.into_inner()) = Some(tcp4);

        let udp4 = Arc::new(udp4);
        let runner = Arc::clone(&udp4);
        tasks.push(tokio::spawn(async move {
            if let Err(error) = runner.run().await {
                tracing::error!("splittun: UDPv4 proxy stopped: {error}");
            }
        }));
        *self.udp4.lock().unwrap_or_else(|error| error.into_inner()) = Some(udp4);

        if let Some(tcp6) = tcp6 {
            let tcp6 = Arc::new(tcp6);
            let runner = Arc::clone(&tcp6);
            tasks.push(tokio::spawn(async move {
                if let Err(error) = runner.run().await {
                    tracing::error!("splittun: TCPv6 proxy stopped: {error}");
                }
            }));
            *self.tcp6.lock().unwrap_or_else(|error| error.into_inner()) = Some(tcp6);
        }

        if let Some(udp6) = udp6 {
            let udp6 = Arc::new(udp6);
            let runner = Arc::clone(&udp6);
            tasks.push(tokio::spawn(async move {
                if let Err(error) = runner.run().await {
                    tracing::error!("splittun: UDPv6 proxy stopped: {error}");
                }
            }));
            *self.udp6.lock().unwrap_or_else(|error| error.into_inner()) = Some(udp6);
        }

        *self.tasks.lock().unwrap_or_else(|error| error.into_inner()) = tasks;
        self.ready.store(true, Ordering::SeqCst);
        tracing::info!("splittun: proxies started");
    }

    /// Stops all proxies, like Go `stopProxies`.
    pub fn stop(&self) {
        self.ready.store(false, Ordering::SeqCst);

        let tcp4 = self
            .tcp4
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .take();
        let tcp6 = self
            .tcp6
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .take();
        let udp4 = self
            .udp4
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .take();
        let udp6 = self
            .udp6
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .take();

        if let Some(proxy) = tcp4 {
            proxy.stop();
        }
        if let Some(proxy) = tcp6 {
            proxy.stop();
        }
        if let Some(proxy) = udp4 {
            proxy.stop();
        }
        if let Some(proxy) = udp6 {
            proxy.stop();
        }

        let tasks = std::mem::take(&mut *self
            .tasks
            .lock()
            .unwrap_or_else(|error| error.into_inner()));
        for task in tasks {
            task.abort();
        }
    }

    /// Whether the connection is the proxy's own egress connection of an
    /// active session, like Go `IsProxiedConnectionInfo`.
    ///
    /// Like Go, TCP checks both proxies because a connection to an upstream
    /// proxy server may use the other IP version.
    pub fn is_proxied_connection_info(&self, conn: &Connection) -> bool {
        let Some(entity) = &conn.entity else {
            return false;
        };
        let Some(ip) = entity.ip else {
            return false;
        };
        let port = entity.port;

        match conn.ip_protocol {
            protocol::TCP => {
                let tcp4 = self.tcp4.lock().unwrap_or_else(|error| error.into_inner());
                if tcp4
                    .as_ref()
                    .is_some_and(|proxy| proxy.has_proxied_egress(ip, port))
                {
                    return true;
                }
                drop(tcp4);
                self.tcp6
                    .lock()
                    .unwrap_or_else(|error| error.into_inner())
                    .as_ref()
                    .is_some_and(|proxy| proxy.has_proxied_egress(ip, port))
            }
            protocol::UDP => {
                let proxy = match conn.ip_version {
                    4 => self.udp4.lock().unwrap_or_else(|error| error.into_inner()).clone(),
                    6 => self.udp6.lock().unwrap_or_else(|error| error.into_inner()).clone(),
                    _ => return false,
                };
                proxy.is_some_and(|proxy| proxy.has_proxied_egress(ip, port))
            }
            _ => false,
        }
    }

    /// Returns the address the IPv4 TCP proxy listens on.
    pub fn tcp4_addr(&self) -> Option<SocketAddr> {
        self.tcp4
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .as_ref()
            .and_then(|proxy| proxy.local_addr().ok())
    }

    /// Returns the address the IPv6 TCP proxy listens on.
    pub fn tcp6_addr(&self) -> Option<SocketAddr> {
        self.tcp6
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .as_ref()
            .and_then(|proxy| proxy.local_addr().ok())
    }

    /// Returns the address the IPv4 UDP proxy listens on.
    pub fn udp4_addr(&self) -> Option<SocketAddr> {
        self.udp4
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .as_ref()
            .and_then(|proxy| proxy.local_addr().ok())
    }

    /// Returns the address the IPv6 UDP proxy listens on.
    pub fn udp6_addr(&self) -> Option<SocketAddr> {
        self.udp6
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .as_ref()
            .and_then(|proxy| proxy.local_addr().ok())
    }
}
