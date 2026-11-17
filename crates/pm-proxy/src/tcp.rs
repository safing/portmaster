use std::io;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::watch;
use tracing::{debug, error, info, warn};

use crate::decider::SharedDecider;
use crate::dial::{connect_tcp, resolve_peers};
use crate::egress::{EgressCache, EgressGuard};
use crate::metrics::ProxyMetrics;

pub struct TcpProxyConfig {
    pub max_sessions: usize,
    pub connect_timeout: Duration,
    pub idle_timeout: Duration,
}

impl Default for TcpProxyConfig {
    fn default() -> Self {
        Self {
            max_sessions: 10_000,
            connect_timeout: Duration::from_secs(10),
            idle_timeout: Duration::from_secs(300),
        }
    }
}

pub struct TcpProxy {
    listener: TcpListener,
    decider: SharedDecider,
    config: TcpProxyConfig,
    metrics: Arc<ProxyMetrics>,
    egress: Arc<EgressCache>,
    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,
    running: AtomicBool,
}

impl TcpProxy {
    pub async fn bind(
        addr: SocketAddr,
        decider: SharedDecider,
        config: TcpProxyConfig,
    ) -> io::Result<Self> {
        let listener = TcpListener::bind(addr).await?;
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Ok(Self {
            listener,
            decider,
            config,
            metrics: Arc::new(ProxyMetrics::new()),
            egress: Arc::new(EgressCache::new()),
            shutdown_tx,
            shutdown_rx,
            running: AtomicBool::new(false),
        })
    }

    pub fn local_addr(&self) -> io::Result<SocketAddr> {
        self.listener.local_addr()
    }

    pub fn metrics(&self) -> Arc<ProxyMetrics> {
        Arc::clone(&self.metrics)
    }

    /// Returns whether a session egresses to the given address, like Go
    /// `HasProxiedEgressConnection`.
    pub fn has_proxied_egress(&self, ip: std::net::IpAddr, port: u16) -> bool {
        self.egress.has(ip, port)
    }

    pub async fn run(&self) -> io::Result<()> {
        self.running.store(true, Ordering::SeqCst);
        let mut shutdown_rx = self.shutdown_rx.clone();

        loop {
            tokio::select! {
                res = self.listener.accept() => {
                    match res {
                        Ok((client_stream, peer_addr)) => {
                            let _ = client_stream.set_nodelay(true);
                            #[cfg(target_os = "linux")]
                            {
                                let _ = socket2::SockRef::from(&client_stream).set_tcp_quickack(true);
                            }
                            if self.config.max_sessions > 0
                                && self.metrics.active_sessions.load(Ordering::Relaxed) >= self.config.max_sessions
                            {
                                warn!("tcp_proxy: max concurrent sessions reached, dropping connection from {}", peer_addr);
                                self.metrics.inc_errors();
                                drop(client_stream);
                                continue;
                            }

                            let local_addr = match client_stream.local_addr() {
                                Ok(a) => a,
                                Err(e) => {
                                    error!("tcp_proxy: failed to get local addr: {}", e);
                                    continue;
                                }
                            };

                            let decision = match self.decider.decide(local_addr, peer_addr) {
                                Ok(d) => d,
                                Err(e) => {
                                    debug!("tcp_proxy: connection from {} rejected by decider: {}", peer_addr, e);
                                    self.metrics.inc_errors();
                                    continue;
                                }
                            };

                            let metrics = Arc::clone(&self.metrics);
                            let connect_timeout = self.config.connect_timeout;
                            let mut session_shutdown = self.shutdown_rx.clone();
                            let egress = Arc::clone(&self.egress);

                            tokio::spawn(async move {
                                metrics.session_started();
                                if let Err(e) = handle_tcp_session(
                                    client_stream,
                                    decision,
                                    connect_timeout,
                                    &metrics,
                                    &mut session_shutdown,
                                    egress,
                                ).await {
                                    debug!("tcp_session closed with error: {}", e);
                                    metrics.inc_errors();
                                }
                                metrics.session_finished();
                            });
                        }
                        Err(e) => {
                            if !self.running.load(Ordering::SeqCst) {
                                break;
                            }
                            error!("tcp_proxy: accept error: {}", e);
                        }
                    }
                }
                _ = shutdown_rx.changed() => {
                    info!("tcp_proxy: shutdown signal received");
                    break;
                }
            }
        }

        self.running.store(false, Ordering::SeqCst);
        Ok(())
    }

    pub fn stop(&self) {
        self.running.store(false, Ordering::SeqCst);
        let _ = self.shutdown_tx.send(true);
    }
}

async fn handle_tcp_session(
    mut client: TcpStream,
    decision: crate::decider::Decision,
    connect_timeout: Duration,
    metrics: &ProxyMetrics,
    shutdown: &mut watch::Receiver<bool>,
    egress: Arc<EgressCache>,
) -> io::Result<()> {
    let target_ip = decision.remote_ip;
    let target_port = decision.remote_port;
    let binding = decision.binding.as_ref();

    // Register the egress address before dialing so the firewall recognizes
    // the outgoing socket as a proxied session from the moment it is created,
    // like Go adding the session to its cache before the dial. For an upstream
    // proxy the egress is the proxy server; otherwise it is the destination.
    let mut egress_guard: Option<EgressGuard> = None;

    let mut upstream = if let Some(proxy) = binding.and_then(|b| b.upstream.as_deref()) {
        let peers = resolve_peers(&proxy.host, proxy.port, connect_timeout).await?;
        let target_host = target_ip.to_string();
        let mut connected = None;
        let mut last_error = None;
        for proxy_addr in peers {
            // Move the egress registration to the address of each dial
            // attempt, like Go `setEgress` before every upstream attempt.
            egress_guard = Some(EgressGuard::new(Arc::clone(&egress), proxy_addr));
            // A loopback proxy is reached without the interface binding;
            // binding to a physical interface would break loopback (Go).
            let dial_binding = if proxy_addr.ip().is_loopback() {
                None
            } else {
                binding
            };
            match proxy
                .dial_and_handshake_to(
                    proxy_addr,
                    &target_host,
                    target_port,
                    dial_binding,
                    connect_timeout,
                )
                .await
            {
                Ok(stream) => {
                    connected = Some(stream);
                    break;
                }
                Err(error) => last_error = Some(error),
            }
        }
        match connected {
            Some(stream) => stream,
            None => {
                return Err(last_error.unwrap_or_else(|| {
                    io::Error::new(io::ErrorKind::NotFound, "proxy host has no addresses")
                }))
            }
        }
    } else {
        let target_addr = SocketAddr::new(target_ip, target_port);
        egress_guard = Some(EgressGuard::new(Arc::clone(&egress), target_addr));
        connect_tcp(target_addr, binding, connect_timeout).await?
    };

    let _ = upstream.set_nodelay(true);
    #[cfg(target_os = "linux")]
    {
        let _ = socket2::SockRef::from(&upstream).set_tcp_quickack(true);
    }

    let result = tokio::select! {
        res = tokio::io::copy_bidirectional_with_sizes(&mut client, &mut upstream, 65536, 65536) => {
            let (tx, rx) = res?;
            metrics.add_bytes(tx, rx);
            Ok(())
        }
        _ = shutdown.changed() => {
            Ok(())
        }
    };
    drop(egress_guard);
    result
}
