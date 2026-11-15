use std::collections::HashMap;
use std::io;
use std::pin::Pin;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::task::{Context, Poll};
use std::time::Duration;

use hickory_proto::op::Message;
use tokio::io::{
    split, AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt, ReadBuf, ReadHalf, WriteHalf,
};
use tokio::net::TcpStream;
use tokio::sync::{mpsc, oneshot, Mutex};
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

struct PendingQuery {
    msg: Message,
    reply_tx: oneshot::Sender<Result<Message, ResolveError>>,
}

type InFlightMap = Arc<std::sync::Mutex<HashMap<u16, oneshot::Sender<Result<Message, ResolveError>>>>>;

#[derive(Clone)]
struct ActiveTcpConn {
    query_tx: mpsc::Sender<PendingQuery>,
    heartbeat_tx: mpsc::Sender<oneshot::Sender<()>>,
    abandoned: Arc<AtomicBool>,
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

        timeout(HEARTBEAT_TIMEOUT, rx).await.is_ok()
    }
}

/// TCP / DoT resolver with persistent connection and pipelining by message ID.
pub struct TcpResolver {
    resolver: Resolver,
    basic: BasicResolverConn,
    is_tls: bool,
    connect_timeout: Duration,
    request_timeout: Duration,
    active_conn: Mutex<Option<ActiveTcpConn>>,
}

impl TcpResolver {
    pub fn new_tcp(resolver: Resolver) -> Self {
        Self {
            resolver,
            basic: BasicResolverConn::new(),
            is_tls: false,
            connect_timeout: TCP_CONNECT_TIMEOUT,
            request_timeout: DEFAULT_REQUEST_TIMEOUT,
            active_conn: Mutex::new(None),
        }
    }

    pub fn new_dot(resolver: Resolver) -> Self {
        Self {
            resolver,
            basic: BasicResolverConn::new(),
            is_tls: true,
            connect_timeout: TCP_CONNECT_TIMEOUT,
            request_timeout: DEFAULT_REQUEST_TIMEOUT,
            active_conn: Mutex::new(None),
        }
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
            active_conn: Mutex::new(None),
        }
    }

    async fn get_or_create_conn(&self) -> Result<ActiveTcpConn, ResolveError> {
        let mut lock = self.active_conn.lock().await;

        if let Some(conn) = lock.as_ref() {
            if conn.is_alive().await {
                return Ok(conn.clone());
            }
        }

        let new_conn = self.dial().await?;
        *lock = Some(new_conn.clone());
        Ok(new_conn)
    }

    async fn dial(&self) -> Result<ActiveTcpConn, ResolveError> {
        let server_addr_str = &self.resolver.server_address;
        let tcp_stream = timeout(
            self.connect_timeout,
            TcpStream::connect(server_addr_str),
        )
        .await
        .map_err(|_| ResolveError::Timeout)?
        .map_err(ResolveError::Io)?;

        let (reader, writer) = if self.is_tls {
            let mut root_store = RootCertStore::empty();
            root_store.extend(webpki_roots::TLS_SERVER_ROOTS.iter().cloned());
            let config = ClientConfig::builder_with_provider(Arc::new(
                rustls::crypto::ring::default_provider(),
            ))
            .with_safe_default_protocol_versions()
            .map_err(|e| ResolveError::Tls(e.to_string()))?
            .with_root_certificates(root_store)
            .with_no_client_auth();

            let connector = tokio_rustls::TlsConnector::from(Arc::new(config));
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
        let (query_tx, query_rx) = mpsc::channel::<PendingQuery>(32);
        let (heartbeat_tx, heartbeat_rx) = mpsc::channel::<oneshot::Sender<()>>(8);
        let abandoned = Arc::new(AtomicBool::new(false));

        // Start writer task.
        let in_flight_writer = in_flight.clone();
        let abandoned_writer = abandoned.clone();
        tokio::spawn(async move {
            writer_loop(
                writer,
                query_rx,
                heartbeat_rx,
                in_flight_writer,
                abandoned_writer,
            )
            .await;
        });

        // Start reader task.
        let in_flight_reader = in_flight;
        let abandoned_reader = abandoned.clone();
        tokio::spawn(async move {
            reader_loop(reader, in_flight_reader, abandoned_reader).await;
        });

        Ok(ActiveTcpConn {
            query_tx,
            heartbeat_tx,
            abandoned,
        })
    }

    async fn execute_query(&self, q: &Query) -> Result<Message, ResolveError> {
        let conn = self.get_or_create_conn().await?;
        let dummy_msg = q.to_message(0)?;
        let (reply_tx, reply_rx) = oneshot::channel();

        let pending = PendingQuery {
            msg: dummy_msg,
            reply_tx,
        };

        conn.query_tx
            .send(pending)
            .await
            .map_err(|_| ResolveError::Failure("connection query queue closed".to_string()))?;

        let reply_res = timeout(self.request_timeout, reply_rx)
            .await
            .map_err(|_| ResolveError::Timeout)?
            .map_err(|_| ResolveError::Failure("query response channel closed".to_string()))?;

        let reply = reply_res?;

        if reply.truncation {
            return Err(ResolveError::Truncated);
        }

        if self.resolver.is_blocked_upstream(&reply) {
            return Err(ResolveError::BlockedUpstream(
                self.resolver.info.descriptive_name(),
            ));
        }

        Ok(reply)
    }
}

async fn writer_loop(
    mut writer: AnyWriteHalf,
    mut query_rx: mpsc::Receiver<PendingQuery>,
    mut heartbeat_rx: mpsc::Receiver<oneshot::Sender<()>>,
    in_flight: InFlightMap,
    abandoned: Arc<AtomicBool>,
) {
    let mut next_id: u16 = crate::conn::random_u16();
    let ttl_sleep = tokio::time::sleep(DEFAULT_CLIENT_TTL);
    tokio::pin!(ttl_sleep);

    loop {
        tokio::select! {
            _ = &mut ttl_sleep => {
                // Connection TTL reached; recycle connection once idle.
                abandoned.store(true, Ordering::Relaxed);
                let is_idle = in_flight.lock().unwrap().is_empty();
                if is_idle {
                    let _ = writer.shutdown().await;
                    return;
                }
            }

            Some(hb_tx) = heartbeat_rx.recv() => {
                let _ = hb_tx.send(());
            }

            Some(mut pending) = query_rx.recv() => {
                // Assign a unique 16-bit message ID not currently in flight.
                let id = {
                    let mut lock = in_flight.lock().unwrap();
                    let mut chosen_id = next_id;
                    for _ in 0..65535 {
                        if !lock.contains_key(&chosen_id) {
                            break;
                        }
                        chosen_id = chosen_id.wrapping_add(1);
                    }
                    next_id = chosen_id.wrapping_add(1);
                    lock.insert(chosen_id, pending.reply_tx);
                    chosen_id
                };

                pending.msg.metadata.id = id;

                let wire_bytes = match pending.msg.to_vec() {
                    Ok(b) => b,
                    Err(e) => {
                        if let Some(tx) = in_flight.lock().unwrap().remove(&id) {
                            let _ = tx.send(Err(ResolveError::Proto(e)));
                        }
                        continue;
                    }
                };

                let len = wire_bytes.len() as u16;
                let mut packet = Vec::with_capacity(2 + wire_bytes.len());
                packet.extend_from_slice(&len.to_be_bytes());
                packet.extend_from_slice(&wire_bytes);

                let write_res = timeout(TCP_WRITE_TIMEOUT, writer.write_all(&packet)).await;
                if let Err(_) | Ok(Err(_)) = write_res {
                    abandoned.store(true, Ordering::Relaxed);
                    if let Some(tx) = in_flight.lock().unwrap().remove(&id) {
                        let _ = tx.send(Err(ResolveError::Failure("write failed".to_string())));
                    }
                    let _ = writer.shutdown().await;
                    return;
                }
            }

            else => {
                // Channels closed.
                abandoned.store(true, Ordering::Relaxed);
                let _ = writer.shutdown().await;
                return;
            }
        }
    }
}

async fn reader_loop(
    mut reader: AnyReadHalf,
    in_flight: InFlightMap,
    abandoned: Arc<AtomicBool>,
) {
    loop {
        let mut len_buf = [0u8; 2];
        if reader.read_exact(&mut len_buf).await.is_err() {
            // Connection closed or EOF.
            abandoned.store(true, Ordering::Relaxed);
            let mut lock = in_flight.lock().unwrap();
            for (_, tx) in lock.drain() {
                let _ = tx.send(Err(ResolveError::Failure(
                    "connection closed by peer".to_string(),
                )));
            }
            return;
        }

        let msg_len = u16::from_be_bytes(len_buf) as usize;
        let mut msg_buf = vec![0u8; msg_len];
        if reader.read_exact(&mut msg_buf).await.is_err() {
            abandoned.store(true, Ordering::Relaxed);
            let mut lock = in_flight.lock().unwrap();
            for (_, tx) in lock.drain() {
                let _ = tx.send(Err(ResolveError::Failure("read payload failed".to_string())));
            }
            return;
        }

        let message = match Message::from_vec(&msg_buf) {
            Ok(m) => m,
            Err(e) => {
                tracing::warn!("failed to decode incoming DNS TCP message: {}", e);
                continue;
            }
        };

        let id = message.id;
        let target_tx = {
            let mut lock = in_flight.lock().unwrap();
            lock.remove(&id)
        };

        if let Some(tx) = target_tx {
            let _ = tx.send(Ok(message));
        } else {
            tracing::debug!("received unsolicited TCP DNS message with id {}", id);
        }
    }
}

#[async_trait::async_trait]
impl ResolverConn for TcpResolver {
    async fn query(&self, q: Query) -> Result<Message, ResolveError> {
        match self.execute_query(&q).await {
            Ok(msg) => Ok(msg),
            Err(err) => {
                self.report_failure();
                Err(err)
            }
        }
    }

    fn report_failure(&self) {
        self.basic.report_failure();
    }

    fn is_failing(&self) -> bool {
        self.basic.is_failing()
    }

    fn reset_failure(&self) {
        self.basic.reset_failure();
    }

    async fn force_reconnect(&self) {
        let mut lock = self.active_conn.lock().await;
        if let Some(conn) = lock.take() {
            conn.abandoned.store(true, Ordering::Relaxed);
        }
    }
}
