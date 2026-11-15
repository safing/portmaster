use std::sync::RwLock;
use std::time::Duration;

use base64::prelude::*;
use hickory_proto::op::Message;
use reqwest::{Client, StatusCode};

use crate::conn::{BasicResolverConn, Query, ResolverConn};
use crate::error::ResolveError;
use crate::model::Resolver;

pub const DEFAULT_REQUEST_TIMEOUT: Duration = Duration::from_secs(5);
pub const DEFAULT_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
pub const MIME_DNS_MESSAGE: &str = "application/dns-message";

/// DoH (DNS over HTTPS) resolver implementing RFC 8484 via reqwest with pure rustls.
pub struct HttpsResolver {
    resolver: Resolver,
    basic: BasicResolverConn,
    client: RwLock<Client>,
    request_timeout: Duration,
    use_post: bool,
}

impl HttpsResolver {
    pub fn new(resolver: Resolver) -> Result<Self, ResolveError> {
        Self::with_options(resolver, DEFAULT_REQUEST_TIMEOUT, false)
    }

    pub fn with_options(
        resolver: Resolver,
        timeout: Duration,
        use_post: bool,
    ) -> Result<Self, ResolveError> {
        let client = build_http_client(timeout)?;
        Ok(Self {
            resolver,
            basic: BasicResolverConn::new(),
            client: RwLock::new(client),
            request_timeout: timeout,
            use_post,
        })
    }

    pub fn set_use_post(&mut self, use_post: bool) {
        self.use_post = use_post;
    }

    async fn execute_query(&self, q: &Query) -> Result<Message, ResolveError> {
        let query_id = crate::conn::random_u16();
        let query_msg = q.to_message(query_id)?;
        let wire_bytes = query_msg.to_vec()?;

        let client = {
            let guard = self.client.read().unwrap();
            guard.clone()
        };

        let path = if self.resolver.path.is_empty() {
            "/dns-query"
        } else {
            &self.resolver.path
        };

        let response = if self.use_post {
            let url = format!("https://{}{}", self.resolver.server_address, path);
            client
                .post(&url)
                .header(reqwest::header::CONTENT_TYPE, MIME_DNS_MESSAGE)
                .header(reqwest::header::ACCEPT, MIME_DNS_MESSAGE)
                .body(wire_bytes)
                .send()
                .await
                .map_err(|e| ResolveError::Http(e.to_string()))?
        } else {
            let b64_dns = BASE64_URL_SAFE_NO_PAD.encode(&wire_bytes);
            let url = format!(
                "https://{}{}{}dns={}",
                self.resolver.server_address,
                path,
                if path.contains('?') { "&" } else { "?" },
                b64_dns
            );
            client
                .get(&url)
                .header(reqwest::header::ACCEPT, MIME_DNS_MESSAGE)
                .send()
                .await
                .map_err(|e| ResolveError::Http(e.to_string()))?
        };

        if response.status() != StatusCode::OK {
            return Err(ResolveError::Http(format!(
                "http request failed with {}",
                response.status()
            )));
        }

        let body_bytes = response
            .bytes()
            .await
            .map_err(|e| ResolveError::Http(format!("failed to read response body: {}", e)))?;

        let reply = Message::from_vec(&body_bytes)?;

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

fn build_http_client(request_timeout: Duration) -> Result<Client, ResolveError> {
    Client::builder()
        .timeout(request_timeout)
        .connect_timeout(DEFAULT_CONNECT_TIMEOUT)
        .build()
        .map_err(|e| ResolveError::Http(format!("failed to create HTTP client: {}", e)))
}

#[async_trait::async_trait]
impl ResolverConn for HttpsResolver {
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
        if let Ok(new_client) = build_http_client(self.request_timeout) {
            let mut guard = self.client.write().unwrap();
            *guard = new_client;
        }
    }
}
