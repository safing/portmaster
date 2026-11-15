use std::net::SocketAddr;
use std::time::Duration;

use hickory_proto::op::Message;
use tokio::net::UdpSocket;
use tokio::time::timeout;

use crate::conn::{BasicResolverConn, Query, ResolverConn};
use crate::error::ResolveError;
use crate::model::Resolver;

pub const DEFAULT_REQUEST_TIMEOUT: Duration = Duration::from_secs(3);
pub const DEFAULT_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

/// PlainResolver resolves DNS queries over plain UDP.
pub struct PlainResolver {
    resolver: Resolver,
    basic: BasicResolverConn,
    connect_timeout: Duration,
    request_timeout: Duration,
}

impl PlainResolver {
    pub fn new(resolver: Resolver) -> Self {
        Self {
            resolver,
            basic: BasicResolverConn::new(),
            connect_timeout: DEFAULT_CONNECT_TIMEOUT,
            request_timeout: DEFAULT_REQUEST_TIMEOUT,
        }
    }

    pub fn with_timeouts(
        resolver: Resolver,
        connect_timeout: Duration,
        request_timeout: Duration,
    ) -> Self {
        Self {
            resolver,
            basic: BasicResolverConn::new(),
            connect_timeout,
            request_timeout,
        }
    }

    async fn execute_query(&self, q: &Query) -> Result<Message, ResolveError> {
        let query_id = crate::conn::random_u16();
        let query_msg = q.to_message(query_id)?;
        let wire_bytes = query_msg.to_vec()?;

        let server_addr: SocketAddr = self
            .resolver
            .server_address
            .parse()
            .map_err(|e| ResolveError::Protocol(format!("invalid server address: {}", e)))?;

        let bind_addr = if server_addr.is_ipv4() {
            "0.0.0.0:0"
        } else {
            "[::]:0"
        };

        let socket = UdpSocket::bind(bind_addr).await?;

        timeout(self.connect_timeout, socket.connect(server_addr))
            .await
            .map_err(|_| ResolveError::Timeout)??;

        socket.send(&wire_bytes).await?;

        // Read response, verifying message ID.
        let mut buf = vec![0u8; 4096];
        let deadline = tokio::time::Instant::now() + self.request_timeout;

        loop {
            let now = tokio::time::Instant::now();
            if now >= deadline {
                return Err(ResolveError::Timeout);
            }
            let remaining = deadline - now;

            let n = timeout(remaining, socket.recv(&mut buf))
                .await
                .map_err(|_| ResolveError::Timeout)??;

            let reply = Message::from_vec(&buf[..n])?;

            if reply.id != query_id {
                // Ignore mismatched message ID and wait for matching one.
                continue;
            }

            // Truncated replies are returned as answers, like miekg/dns does.
            if self.resolver.is_blocked_upstream(&reply) {
                return Err(ResolveError::BlockedUpstream(
                    self.resolver.info.descriptive_name(),
                ));
            }

            return Ok(reply);
        }
    }
}

#[async_trait::async_trait]
impl ResolverConn for PlainResolver {
    async fn query(&self, q: Query) -> Result<Message, ResolveError> {
        // Failure reporting belongs to the orchestrator (Go resolve.go).
        self.execute_query(&q).await
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
        // Plain UDP creates a fresh socket for every request, so reconnection is a no-op.
    }
}
