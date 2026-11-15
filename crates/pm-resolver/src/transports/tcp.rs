use std::collections::HashMap;
use std::io;
use std::pin::Pin;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::task::{Context, Poll};
use std::time::Duration;

use hickory_proto::op::Message;
use tokio::io::{
    split, AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt, ReadBuf, ReadHalf, WriteHalf,
};
use tokio::net::TcpStream;
use tokio::sync::{mpsc, oneshot, Notify};
use tokio::time::timeout;
use tokio_rustls::client::TlsStream;
use tokio_rustls::rustls::{self, pki_types::ServerName, ClientConfig, RootCertStore};

use crate::conn::{BasicResolverConn, Query, ResolverConn};
use crate::error::ResolveError;
use crate::model::Resolver;

pub const TCP_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);
pub const TCP_WRITE_TIMEOUT: Duration = Duration::from_secs(2);
pub const HEARTBEAT_TIMEOUT: Duration = Duration::from_secs(5);
pub const DEFAULT_REQUEST_TIMEOUT: Duration = Duration::from_secs(3);
pub const DEFAULT_CLIENT_TTL: Duration = Duration::from_secs(300);

enum AnyReadHalf {
    Tcp(ReadHalf<TcpStream>),
    Tls(ReadHalf<TlsStream<TcpStream>>),
}

impl AsyncRead for AnyReadHalf {
    fn poll_read(
        self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &mut ReadBuf<'_>,
    ) -> Poll<io::Result<()>> {
        match self.get_mut() {
            Self::Tcp(r) => Pin::new(r).poll_read(cx, buf),
            Self::Tls(r) => Pin::new(r).poll_read(cx, buf),
        }
    }
}

enum AnyWriteHalf {
    Tcp(WriteHalf<TcpStream>),
    Tls(WriteHalf<TlsStream<TcpStream>>),
}

impl AsyncWrite for AnyWriteHalf {
    fn poll_write(
        self: Pin<&mut Self>,
        cx: &mut Context<'_>,
        buf: &[u8],
    ) -> Poll<io::Result<usize>> {
        match self.get_mut() {
            Self::Tcp(w) => Pin::new(w).poll_write(cx, buf),
            Self::Tls(w) => Pin::new(w).poll_write(cx, buf),
        }
    }

    fn poll_flush(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<io::Result<()>> {
        match self.get_mut() {
            Self::Tcp(w) => Pin::new(w).poll_flush(cx),
            Self::Tls(w) => Pin::new(w).poll_flush(cx),
        }
    }

    fn poll_shutdown(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<io::Result<()>> {
        match self.get_mut() {
            Self::Tcp(w) => Pin::new(w).poll_shutdown(cx),
            Self::Tls(w) => Pin::new(w).poll_shutdown(cx),
        }
    }
}

type Reply = Result<Message, ResolveError>;
type InFlightMap = Arc<std::sync::Mutex<HashMap<u16, oneshot::Sender<Reply>>>>;

#[derive(Clone)]
struct ActiveTcpConn {
    instance_id: u64,
    query_tx: mpsc::Sender<Vec<u8>>,
    heartbeat_tx: mpsc::Sender<oneshot::Sender<()>>,
    abandoned: Arc<AtomicBool>,
    in_flight: InFlightMap,
    /// Wakes the writer to re-check whether the connection can be recycled.
    wake: Arc<Notify>,
}

impl ActiveTcpConn {
    async fn is_alive(&self) -> bool {
        if self.abandoned.load(Ordering::Relaxed) {
            return false;
        }

        let (tx, rx) = oneshot::channel();
        if self.heartbeat_tx.send(tx).await.is_err() {
            return false;
        }

        matches!(timeout(HEARTBEAT_TIMEOUT, rx).await, Ok(Ok(())))
    }

    fn abandon(&self) {
        self.abandoned.store(true, Ordering::Relaxed);
        self.wake.notify_one();
    }
}

/// Removes a query from the in-flight registry when it finishes, times out or is cancelled.
struct InFlightGuard {
    conn: ActiveTcpConn,
    id: u16,
}

impl Drop for InFlightGuard {
    fn drop(&mut self) {
        self.conn.in_flight.lock().unwrap().remove(&self.id);
        // A connection waiting to be recycled may have become idle.
        self.conn.wake.notify_one();
    }
}

/// TCP / DoT resolver with persistent connection and pipelining by message ID.
pub struct TcpResolver {
    resolver: Resolver,
    basic: BasicResolverConn,
    is_tls: bool,
    connect_timeout: Duration,
    request_timeout: Duration,
    client_ttl: Duration,
    active_conn: std::sync::Mutex<Option<ActiveTcpConn>>,
    next_instance_id: AtomicU64,
    dial_lock: tokio::sync::Mutex<()>,
    tls_config: std::sync::Mutex<Option<Arc<ClientConfig>>>,
}

impl TcpResolver {
    pub fn new_tcp(resolver: Resolver) -> Self {
        Self::with_timeouts(resolver, false, TCP_CONNECT_TIMEOUT, DEFAULT_REQUEST_TIMEOUT)
    }

    pub fn new_dot(resolver: Resolver) -> Self {
        Self::with_timeouts(resolver, true, TCP_CONNECT_TIMEOUT, DEFAULT_REQUEST_TIMEOUT)
    }

    pub fn with_timeouts(
        resolver: Resolver,
        is_tls: bool,
        connect_timeout: Duration,
        request_timeout: Duration,
    ) -> Self {
        Self {
            resolver,
            basic: BasicResolverConn::new(),
            is_tls,
            connect_timeout,
            request_timeout,
            client_ttl: DEFAULT_CLIENT_TTL,
            active_conn: std::sync::Mutex::new(None),
            next_instance_id: AtomicU64::new(1),
            dial_lock: tokio::sync::Mutex::new(()),
            tls_config: std::sync::Mutex::new(None),
        }
    }

    /// Sets how long a connection is used before it is recycled.
    pub fn with_client_ttl(mut self, ttl: Duration) -> Self {
        self.client_ttl = ttl;
        self
    }

    /// Replaces the default TLS configuration (webpki roots), e.g. to trust a test CA.
    pub fn with_tls_config(self, config: Arc<ClientConfig>) -> Self {
        *self.tls_config.lock().unwrap() = Some(config);
        self
    }

    /// Number of queries currently awaiting a reply on the active connection.
    pub fn in_flight_count(&self) -> usize {
        self.active_conn
            .lock()
            .unwrap()
            .as_ref()
            .map_or(0, |c| c.in_flight.lock().unwrap().len())
    }

    fn tls_config(&self) -> Result<Arc<ClientConfig>, ResolveError> {
        let mut guard = self.tls_config.lock().unwrap();
        if let Some(cfg) = guard.as_ref() {
            return Ok(cfg.clone());
        }
        let mut root_store = RootCertStore::empty();
        root_store.extend(webpki_roots::TLS_SERVER_ROOTS.iter().cloned());
        let config = ClientConfig::builder_with_provider(Arc::new(
            rustls::crypto::ring::default_provider(),
        ))
        .with_safe_default_protocol_versions()
        .map_err(|e| ResolveError::Tls(e.to_string()))?
        .with_root_certificates(root_store)
        .with_no_client_auth();
        let config = Arc::new(config);
        *guard = Some(config.clone());
        Ok(config)
    }

    /// Returns the live connection or dials a new one. The lock is never held
    /// across the heartbeat or the dial, like Go.
    async fn get_or_create_conn(&self) -> Result<ActiveTcpConn, ResolveError> {
        let (existing, seen_instance_id) = {
            let guard = self.active_conn.lock().unwrap();
            let seen = guard.as_ref().map_or(0, |c| c.instance_id);
            let existing = guard
                .as_ref()
                .filter(|c| !c.abandoned.load(Ordering::Relaxed))
                .cloned();
            (existing, seen)
        };

        if let Some(conn) = existing {
            if conn.is_alive().await {
                return Ok(conn);
            }
        }

        // Concurrent queries wait for one dial instead of each opening a connection.
        let _dial_guard = self.dial_lock.lock().await;
        {
            let guard = self.active_conn.lock().unwrap();
            if let Some(current) = guard.as_ref() {
                if current.instance_id != seen_instance_id
                    && !current.abandoned.load(Ordering::Relaxed)
                {
                    return Ok(current.clone());
                }
            }
        }

        let new_conn = self.dial().await?;
        *self.active_conn.lock().unwrap() = Some(new_conn.clone());
        Ok(new_conn)
    }

    async fn dial(&self) -> Result<ActiveTcpConn, ResolveError> {
        let server_addr_str = &self.resolver.server_address;
        let tcp_stream = timeout(self.connect_timeout, TcpStream::connect(server_addr_str))
            .await
            .map_err(|_| ResolveError::Timeout)?
            .map_err(ResolveError::Io)?;

        let (reader, writer) = if self.is_tls {
            let connector = tokio_rustls::TlsConnector::from(self.tls_config()?);
            let domain = self.resolver.info.domain.clone();
            let server_name = ServerName::try_from(domain)
                .map_err(|e| ResolveError::Tls(format!("invalid SNI domain: {}", e)))?
                .to_owned();

            let tls_stream = timeout(
                self.connect_timeout,
                connector.connect(server_name, tcp_stream),
            )
            .await
            .map_err(|_| ResolveError::Timeout)?
            .map_err(|e| ResolveError::Tls(e.to_string()))?;

            let (r, w) = split(tls_stream);
            (AnyReadHalf::Tls(r), AnyWriteHalf::Tls(w))
        } else {
            let (r, w) = split(tcp_stream);
            (AnyReadHalf::Tcp(r), AnyWriteHalf::Tcp(w))
        };

        let in_flight: InFlightMap = Arc::new(std::sync::Mutex::new(HashMap::new()));
        let (query_tx, query_rx) = mpsc::channel::<Vec<u8>>(32);
        let (heartbeat_tx, heartbeat_rx) = mpsc::channel::<oneshot::Sender<()>>(8);
        let (kill_tx, kill_rx) = oneshot::channel::<()>();
        let abandoned = Arc::new(AtomicBool::new(false));
        let wake = Arc::new(Notify::new());

        tokio::spawn(writer_loop(WriterCtx {
            writer,
            query_rx,
            heartbeat_rx,
            in_flight: in_flight.clone(),
            abandoned: abandoned.clone(),
            wake: wake.clone(),
            ttl: self.client_ttl,
            _kill_tx: kill_tx,
        }));
        tokio::spawn(reader_loop(
            reader,
            in_flight.clone(),
            abandoned.clone(),
            wake.clone(),
            kill_rx,
        ));

        Ok(ActiveTcpConn {
            instance_id: self.next_instance_id.fetch_add(1, Ordering::Relaxed),
            query_tx,
            heartbeat_tx,
            abandoned,
            in_flight,
            wake,
        })
    }

    /// Picks a message ID that is not in flight and registers the reply channel.
    fn register(conn: &ActiveTcpConn, tx: oneshot::Sender<Reply>) -> Result<u16, ResolveError> {
        let mut lock = conn.in_flight.lock().unwrap();
        let start = crate::conn::random_u16();
        for offset in 0..=u16::MAX {
            let id = start.wrapping_add(offset);
            if let std::collections::hash_map::Entry::Vacant(e) = lock.entry(id) {
                e.insert(tx);
                return Ok(id);
            }
        }
        Err(ResolveError::Failure("no free message ID".to_string()))
    }

    async fn execute_query(&self, q: &Query) -> Result<Message, ResolveError> {
        let conn = self.get_or_create_conn().await?;
        let (reply_tx, reply_rx) = oneshot::channel();
        let id = Self::register(&conn, reply_tx)?;
        let query_tx = conn.query_tx.clone();
        let _guard = InFlightGuard { conn, id };

        let wire = q.to_message(id)?.to_vec()?;
        let len = u16::try_from(wire.len())
            .map_err(|_| ResolveError::Protocol("query too large".to_string()))?;
        let mut packet = Vec::with_capacity(2 + wire.len());
        packet.extend_from_slice(&len.to_be_bytes());
        packet.extend_from_slice(&wire);

        timeout(self.request_timeout, query_tx.send(packet))
            .await
            .map_err(|_| ResolveError::Timeout)?
            .map_err(|_| ResolveError::Failure("connection query queue closed".to_string()))?;

        let reply = timeout(self.request_timeout, reply_rx)
            .await
            .map_err(|_| ResolveError::Timeout)?
            .map_err(|_| ResolveError::Failure("query response channel closed".to_string()))??;

        // Truncated replies are returned as answers, like miekg/dns does.
        if self.resolver.is_blocked_upstream(&reply) {
            return Err(ResolveError::BlockedUpstream(
                self.resolver.info.descriptive_name(),
            ));
        }

        Ok(reply)
    }
}

fn fail_all(in_flight: &InFlightMap, reason: &str) {
    let mut lock = in_flight.lock().unwrap();
    for (_, tx) in lock.drain() {
        let _ = tx.send(Err(ResolveError::Failure(reason.to_string())));
    }
}

struct WriterCtx {
    writer: AnyWriteHalf,
    query_rx: mpsc::Receiver<Vec<u8>>,
    heartbeat_rx: mpsc::Receiver<oneshot::Sender<()>>,
    in_flight: InFlightMap,
    abandoned: Arc<AtomicBool>,
    wake: Arc<Notify>,
    ttl: Duration,
    /// Dropped when the writer exits, which stops the reader.
    _kill_tx: oneshot::Sender<()>,
}

async fn writer_loop(mut ctx: WriterCtx) {
    let ttl_sleep = tokio::time::sleep(ctx.ttl);
    tokio::pin!(ttl_sleep);
    let mut ttl_fired = false;

    loop {
        tokio::select! {
            // A completed Sleep is always ready, so it must be disarmed after firing.
            _ = &mut ttl_sleep, if !ttl_fired => {
                // Recycle: no new queries use this connection; it closes once idle.
                ttl_fired = true;
                ctx.abandoned.store(true, Ordering::Relaxed);
            }

            _ = ctx.wake.notified() => {}

            hb = ctx.heartbeat_rx.recv() => match hb {
                Some(tx) => { let _ = tx.send(()); }
                None => break,
            },

            q = ctx.query_rx.recv() => match q {
                Some(packet) => {
                    let res = timeout(TCP_WRITE_TIMEOUT, ctx.writer.write_all(&packet)).await;
                    if !matches!(res, Ok(Ok(()))) {
                        ctx.abandoned.store(true, Ordering::Relaxed);
                        fail_all(&ctx.in_flight, "write failed");
                        break;
                    }
                }
                None => break,
            },
        }

        if ctx.abandoned.load(Ordering::Relaxed) && ctx.in_flight.lock().unwrap().is_empty() {
            break;
        }
    }

    ctx.abandoned.store(true, Ordering::Relaxed);
    // Fail queries that were queued but never answered, then close.
    ctx.query_rx.close();
    fail_all(&ctx.in_flight, "connection closed");
    let _ = ctx.writer.shutdown().await;
}

async fn reader_loop(
    mut reader: AnyReadHalf,
    in_flight: InFlightMap,
    abandoned: Arc<AtomicBool>,
    wake: Arc<Notify>,
    mut kill_rx: oneshot::Receiver<()>,
) {
    let reason = loop {
        let mut len_buf = [0u8; 2];
        let read_len = tokio::select! {
            r = reader.read_exact(&mut len_buf) => r,
            _ = &mut kill_rx => break "connection closed",
        };
        if read_len.is_err() {
            break "connection closed by peer";
        }

        let mut msg_buf = vec![0u8; u16::from_be_bytes(len_buf) as usize];
        let read_body = tokio::select! {
            r = reader.read_exact(&mut msg_buf) => r,
            _ = &mut kill_rx => break "connection closed",
        };
        if read_body.is_err() {
            break "read payload failed";
        }

        let message = match Message::from_vec(&msg_buf) {
            Ok(m) => m,
            Err(e) => {
                tracing::warn!("failed to decode incoming DNS TCP message: {}", e);
                continue;
            }
        };

        let target_tx = in_flight.lock().unwrap().remove(&message.id);
        match target_tx {
            Some(tx) => {
                let _ = tx.send(Ok(message));
                // Let the writer recycle an expired connection that is now idle.
                wake.notify_one();
            }
            None => tracing::debug!("received unsolicited TCP DNS message with id {}", message.id),
        }
    };

    abandoned.store(true, Ordering::Relaxed);
    fail_all(&in_flight, reason);
    wake.notify_one();
}

#[async_trait::async_trait]
impl ResolverConn for TcpResolver {
    async fn query(&self, q: Query) -> Result<Message, ResolveError> {
        // Failure reporting belongs to the orchestrator (Go resolve.go).
        self.execute_query(&q).await
    }

    fn report_failure(&self) -> bool {
        self.basic.report_failure()
    }

    fn is_failing(&self) -> bool {
        self.basic.is_failing()
    }

    fn reset_failure(&self) {
        self.basic.reset_failure();
    }

    async fn force_reconnect(&self) {
        // The old connection finishes its in-flight queries, then closes.
        if let Some(conn) = self.active_conn.lock().unwrap().as_ref() {
            conn.abandon();
        }
    }
}
