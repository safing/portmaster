use std::io;
use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::watch;
use tracing::{debug, error, info, warn};

use crate::decider::SharedDecider;
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

    pub async fn run(&self) -> io::Result<()> {
        self.running.store(true, Ordering::SeqCst);
        let mut shutdown_rx = self.shutdown_rx.clone();

        loop {
            tokio::select! {
                res = self.listener.accept() => {
                    match res {
                        Ok((client_stream, peer_addr)) => {
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

                            tokio::spawn(async move {
                                metrics.session_started();
                                if let Err(e) = handle_tcp_session(
                                    client_stream,
                                    decision,
                                    connect_timeout,
                                    &metrics,
                                    &mut session_shutdown,
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
) -> io::Result<()> {
    let target_ip = decision.remote_ip.to_string();
    let target_port = decision.remote_port;

    let mut upstream = if let Some(binding) = decision.binding.as_ref() {
        if let Some(proxy) = binding.upstream.as_ref() {
            proxy.dial_and_handshake(&target_ip, target_port, connect_timeout).await?
        } else {
            let target_addr = SocketAddr::new(decision.remote_ip, target_port);
            tokio::time::timeout(connect_timeout, TcpStream::connect(target_addr))
                .await
                .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "direct dial timed out"))??
        }
    } else {
        let target_addr = SocketAddr::new(decision.remote_ip, target_port);
        tokio::time::timeout(connect_timeout, TcpStream::connect(target_addr))
            .await
            .map_err(|_| io::Error::new(io::ErrorKind::TimedOut, "direct dial timed out"))??
    };

    tokio::select! {
        res = tokio::io::copy_bidirectional(&mut client, &mut upstream) => {
            let (tx, rx) = res?;
            metrics.add_bytes(tx, rx);
            Ok(())
        }
        _ = shutdown.changed() => {
            Ok(())
        }
    }
}
