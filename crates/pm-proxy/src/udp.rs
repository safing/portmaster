use std::collections::HashMap;
use std::io;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tokio::sync::{watch, Mutex};
use tracing::{debug, error, info, warn};

use crate::decider::SharedDecider;
use crate::dial::connect_udp;
use crate::egress::EgressCache;
use crate::metrics::ProxyMetrics;

pub struct UdpProxyConfig {
    pub max_sessions: usize,
    pub idle_timeout: Duration,
    pub cleanup_interval: Duration,
}

impl Default for UdpProxyConfig {
    fn default() -> Self {
        Self {
            max_sessions: 10_000,
            idle_timeout: Duration::from_secs(60),
            cleanup_interval: Duration::from_secs(10),
        }
    }
}

struct UdpSession {
    upstream_socket: Arc<UdpSocket>,
    last_activity: Instant,
    /// Destination the session egresses to.
    dest: SocketAddr,
}

pub struct UdpProxy {
    listen_socket: Arc<UdpSocket>,
    decider: SharedDecider,
    config: UdpProxyConfig,
    metrics: Arc<ProxyMetrics>,
    egress: Arc<EgressCache>,
    sessions: Arc<Mutex<HashMap<SocketAddr, UdpSession>>>,
    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,
    running: AtomicBool,
}

impl UdpProxy {
    pub async fn bind(
        addr: SocketAddr,
        decider: SharedDecider,
        config: UdpProxyConfig,
    ) -> io::Result<Self> {
        let socket = Arc::new(UdpSocket::bind(addr).await?);
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Ok(Self {
            listen_socket: socket,
            decider,
            config,
            metrics: Arc::new(ProxyMetrics::new()),
            egress: Arc::new(EgressCache::new()),
            sessions: Arc::new(Mutex::new(HashMap::new())),
            shutdown_tx,
            shutdown_rx,
            running: AtomicBool::new(false),
        })
    }

    pub fn local_addr(&self) -> io::Result<SocketAddr> {
        self.listen_socket.local_addr()
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

        // Spawn cleanup worker
        let sessions_for_cleanup = Arc::clone(&self.sessions);
        let metrics_for_cleanup = Arc::clone(&self.metrics);
        let egress_for_cleanup = Arc::clone(&self.egress);
        let idle_timeout = self.config.idle_timeout;
        let cleanup_interval = self.config.cleanup_interval;
        let mut cleanup_shutdown = self.shutdown_rx.clone();

        tokio::spawn(async move {
            let mut ticker = tokio::time::interval(cleanup_interval);
            loop {
                tokio::select! {
                    _ = ticker.tick() => {
                        let mut map = sessions_for_cleanup.lock().await;
                        let now = Instant::now();
                        let mut removed = Vec::new();
                        map.retain(|_, session| {
                            let keep = now.duration_since(session.last_activity) < idle_timeout;
                            if !keep {
                                removed.push(session.dest);
                            }
                            keep
                        });
                        if !removed.is_empty() {
                            debug!("udp_proxy: evicted {} idle sessions", removed.len());
                            for dest in removed {
                                egress_for_cleanup.remove(dest);
                                metrics_for_cleanup.session_finished();
                            }
                        }
                    }
                    _ = cleanup_shutdown.changed() => break,
                }
            }
        });

        let mut buf = vec![0u8; 65535];
        let local_addr = self.listen_socket.local_addr()?;

        loop {
            tokio::select! {
                res = self.listen_socket.recv_from(&mut buf) => {
                    match res {
                        Ok((len, peer_addr)) => {
                            let data = &buf[..len];
                            self.metrics.bytes_rx.fetch_add(len as u64, Ordering::Relaxed);

                            let mut sessions_guard = self.sessions.lock().await;
                            if let Some(session) = sessions_guard.get_mut(&peer_addr) {
                                session.last_activity = Instant::now();
                                if let Err(e) = session.upstream_socket.send(data).await {
                                    debug!("udp_proxy: upstream send error: {}", e);
                                    self.metrics.inc_errors();
                                } else {
                                    self.metrics.bytes_tx.fetch_add(len as u64, Ordering::Relaxed);
                                }
                                continue;
                            }

                            // New session
                            if self.config.max_sessions > 0 && sessions_guard.len() >= self.config.max_sessions {
                                warn!("udp_proxy: max concurrent sessions reached, dropping datagram from {}", peer_addr);
                                self.metrics.inc_errors();
                                continue;
                            }

                            let decision = match self.decider.decide(local_addr, peer_addr) {
                                Ok(d) => d,
                                Err(e) => {
                                    debug!("udp_proxy: datagram from {} rejected by decider: {}", peer_addr, e);
                                    self.metrics.inc_errors();
                                    continue;
                                }
                            };

                            let target_addr = SocketAddr::new(decision.remote_ip, decision.remote_port);

                            // UDP cannot be relayed through an upstream proxy;
                            // Go drops the datagram instead of leaking it
                            // directly to the destination.
                            if decision
                                .binding
                                .as_ref()
                                .is_some_and(|binding| binding.upstream.is_some())
                            {
                                warn!(
                                    "udp_proxy: UDP is not supported through an upstream proxy, dropping datagram from {}",
                                    peer_addr
                                );
                                self.metrics.inc_errors();
                                continue;
                            }

                            let upstream = match connect_udp(target_addr, decision.binding.as_ref()).await {
                                Ok(socket) => Arc::new(socket),
                                Err(e) => {
                                    error!("udp_proxy: failed to create upstream socket for {}: {}", target_addr, e);
                                    self.metrics.inc_errors();
                                    continue;
                                }
                            };

                            if let Err(e) = upstream.send(data).await {
                                error!("udp_proxy: failed to send initial datagram to {}: {}", target_addr, e);
                                self.metrics.inc_errors();
                                continue;
                            }
                            self.metrics.bytes_tx.fetch_add(len as u64, Ordering::Relaxed);
                            self.metrics.session_started();

                            self.egress.add(target_addr);
                            sessions_guard.insert(peer_addr, UdpSession {
                                upstream_socket: Arc::clone(&upstream),
                                last_activity: Instant::now(),
                                dest: target_addr,
                            });
                            drop(sessions_guard);

                            // Spawn reply receiver for this upstream socket
                            let client_sock = Arc::clone(&self.listen_socket);
                            let sessions_ref = Arc::clone(&self.sessions);
                            let metrics_ref = Arc::clone(&self.metrics);
                            let mut reply_shutdown = self.shutdown_rx.clone();

                            tokio::spawn(async move {
                                let mut reply_buf = vec![0u8; 65535];
                                loop {
                                    tokio::select! {
                                        res = upstream.recv(&mut reply_buf) => {
                                            match res {
                                                Ok(reply_len) => {
                                                    metrics_ref.bytes_rx.fetch_add(reply_len as u64, Ordering::Relaxed);
                                                    if let Err(e) = client_sock.send_to(&reply_buf[..reply_len], peer_addr).await {
                                                        debug!("udp_proxy: client reply send error: {}", e);
                                                        metrics_ref.inc_errors();
                                                        break;
                                                    }
                                                    metrics_ref.bytes_tx.fetch_add(reply_len as u64, Ordering::Relaxed);
                                                    let mut map = sessions_ref.lock().await;
                                                    if let Some(s) = map.get_mut(&peer_addr) {
                                                        s.last_activity = Instant::now();
                                                    }
                                                }
                                                Err(e) => {
                                                    debug!("udp_proxy: upstream recv error: {}", e);
                                                    break;
                                                }
                                            }
                                        }
                                        _ = reply_shutdown.changed() => break,
                                    }
                                }
                            });
                        }
                        Err(e) => {
                            if !self.running.load(Ordering::SeqCst) {
                                break;
                            }
                            error!("udp_proxy: recv error: {}", e);
                        }
                    }
                }
                _ = shutdown_rx.changed() => {
                    info!("udp_proxy: shutdown signal received");
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
