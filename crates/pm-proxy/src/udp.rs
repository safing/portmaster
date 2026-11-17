use std::collections::HashMap;
use std::io;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::net::UdpSocket;
use tokio::sync::{watch, Mutex};
use tokio_util::sync::CancellationToken;
use tracing::{debug, error, info, warn};

use crate::decider::SharedDecider;
use crate::dial::connect_udp;
use crate::egress::{EgressCache, EgressGuard};
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
    /// Cancelled when the session is evicted or the proxy stops. The reply
    /// relay watches this so an evicted session's socket is released and its
    /// stale relay cannot refresh a new session from the same peer.
    cancel: CancellationToken,
    /// Egress registration lives exactly as long as the session is in the map.
    _egress: EgressGuard,
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
                        let mut removed = 0usize;
                        map.retain(|_, session| {
                            let keep = now.duration_since(session.last_activity) < idle_timeout;
                            if !keep {
                                // Signal the reply relay to stop. Dropping the
                                // map entry also drops its EgressGuard (releasing
                                // egress) and one Arc of the upstream socket;
                                // the relay's Arc is dropped when it exits on
                                // cancellation.
                                session.cancel.cancel();
                                removed += 1;
                            }
                            keep
                        });
                        if removed > 0 {
                            debug!("udp_proxy: evicted {} idle sessions", removed);
                            for _ in 0..removed {
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

                            // Register egress before the first datagram so the
                            // firewall can recognize it as a proxy session.
                            // The guard is moved into the session so egress is
                            // released exactly when the session is evicted.
                            let egress = EgressGuard::new(Arc::clone(&self.egress), target_addr);
                            if let Err(e) = upstream.send(data).await {
                                drop(egress);
                                error!("udp_proxy: failed to send initial datagram to {}: {}", target_addr, e);
                                self.metrics.inc_errors();
                                continue;
                            }
                            self.metrics.bytes_tx.fetch_add(len as u64, Ordering::Relaxed);
                            self.metrics.session_started();
                            let session_cancel = CancellationToken::new();
                            sessions_guard.insert(peer_addr, UdpSession {
                                upstream_socket: Arc::clone(&upstream),
                                last_activity: Instant::now(),
                                cancel: session_cancel.clone(),
                                _egress: egress,
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
                                                    // If this session was evicted while we
                                                    // were waiting, do not touch the map: a
                                                    // new session may now occupy the same
                                                    // peer address.
                                                    if session_cancel.is_cancelled() {
                                                        break;
                                                    }
                                                    metrics_ref.bytes_rx.fetch_add(reply_len as u64, Ordering::Relaxed);
                                                    if let Err(e) = client_sock.send_to(&reply_buf[..reply_len], peer_addr).await {
                                                        debug!("udp_proxy: client reply send error: {}", e);
                                                        metrics_ref.inc_errors();
                                                        break;
                                                    }
                                                    metrics_ref.bytes_tx.fetch_add(reply_len as u64, Ordering::Relaxed);
                                                    let mut map = sessions_ref.lock().await;
                                                    if let Some(s) = map.get_mut(&peer_addr) {
                                                        // Re-check after acquiring the lock:
                                                        // eviction holds the same lock.
                                                        if !s.cancel.is_cancelled() {
                                                            s.last_activity = Instant::now();
                                                        }
                                                    }
                                                }
                                                Err(e) => {
                                                    debug!("udp_proxy: upstream recv error: {}", e);
                                                    break;
                                                }
                                            }
                                        }
                                        _ = reply_shutdown.changed() => break,
                                        _ = session_cancel.cancelled() => break,
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::decider::Decision;
    use std::net::Ipv4Addr;

    #[tokio::test]
    async fn idle_session_is_evicted_and_releases_egress() {
        // Echo upstream so the first datagram succeeds and a session is created.
        let echo = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let echo_addr = echo.local_addr().unwrap();
        tokio::spawn(async move {
            let mut buf = [0u8; 256];
            if let Ok((len, peer)) = echo.recv_from(&mut buf).await {
                let _ = echo.send_to(&buf[..len], peer).await;
            }
        });

        let decider: SharedDecider = Arc::new(move |_, _| Ok(Decision::new(echo_addr.ip(), echo_addr.port())));
        let config = UdpProxyConfig {
            max_sessions: 10,
            idle_timeout: Duration::from_millis(150),
            cleanup_interval: Duration::from_millis(20),
        };
        let proxy = Arc::new(
            UdpProxy::bind("127.0.0.1:0".parse().unwrap(), decider, config)
                .await
                .unwrap(),
        );
        let proxy_addr = proxy.local_addr().unwrap();
        let run_proxy = Arc::clone(&proxy);
        let proxy_handle = tokio::spawn(async move {
            let _ = run_proxy.run().await;
        });
        let client = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        client.send_to(b"ping", proxy_addr).await.unwrap();

        let mut buf = [0u8; 256];
        let (n, _) = client.recv_from(&mut buf).await.unwrap();
        assert_eq!(&buf[..n], b"ping");

        // Wait for the session to be created and its egress registered.
        tokio::time::sleep(Duration::from_millis(30)).await;
        assert_eq!(proxy.metrics().snapshot().active_sessions, 1);
        assert!(proxy.has_proxied_egress(
            std::net::IpAddr::V4(Ipv4Addr::LOCALHOST),
            echo_addr.port()
        ));

        // After the idle timeout + a cleanup tick the session must be gone,
        // its cancel token signalled and its egress released.
        tokio::time::sleep(Duration::from_millis(250)).await;
        assert_eq!(proxy.metrics().snapshot().active_sessions, 0);
        assert!(!proxy.has_proxied_egress(
            std::net::IpAddr::V4(Ipv4Addr::LOCALHOST),
            echo_addr.port()
        ));

        proxy.stop();
        proxy_handle.abort();
    }
}
