use std::net::SocketAddr;
use std::sync::RwLock;
use std::time::Duration;

use base64::prelude::*;
use hickory_proto::op::Message;
use reqwest::{redirect, Client, StatusCode};

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
    extra_root: Option<reqwest::Certificate>,
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
        let client = build_http_client(&resolver, timeout, None)?;
        Ok(Self {
            resolver,
            basic: BasicResolverConn::new(),
            client: RwLock::new(client),
            request_timeout: timeout,
            use_post,
            extra_root: None,
        })
    }

    /// Trusts the given extra root certificates (DER) in addition to the
    /// built-in web roots. Intended for tests against a local server.
    pub fn with_extra_root_der(
        resolver: Resolver,
        timeout: Duration,
        use_post: bool,
        root_der: &[u8],
    ) -> Result<Self, ResolveError> {
        let cert = reqwest::Certificate::from_der(root_der)
            .map_err(|e| ResolveError::Tls(e.to_string()))?;
        let client = build_http_client(&resolver, timeout, Some(cert.clone()))?;
        Ok(Self {
            resolver,
            basic: BasicResolverConn::new(),
            client: RwLock::new(client),
            request_timeout: timeout,
            use_post,
            extra_root: Some(cert),
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

        // The URL carries the verified domain (SNI and certificate name); the
        // connection itself goes to ServerAddress via the client's resolve override.
        let base = format!(
            "https://{}:{}",
            self.resolver.info.domain, self.resolver.info.port
        );
        // Path exactly as configured (Go uses Resolver.Path unchanged).
        let path = self.resolver.path.as_str();

        let response = if self.use_post {
            let url = format!("{}{}", base, path);
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
                "{}{}{}dns={}",
                base,
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

        // No block detection here: Go's HTTPS resolver does not run it.
        Ok(Message::from_vec(&body_bytes)?)
    }
}

fn build_http_client(
    resolver: &Resolver,
    request_timeout: Duration,
    extra_root: Option<reqwest::Certificate>,
) -> Result<Client, ResolveError> {
    let mut builder = Client::builder()
        .timeout(request_timeout)
        .connect_timeout(DEFAULT_CONNECT_TIMEOUT)
        // Go's transport has no proxy and the resolver must not leak through one.
        .no_proxy()
        .redirect(redirect::Policy::none());
    if let Some(cert) = extra_root {
        builder = builder.add_root_certificate(cert);
    }
    // Connect to the configured address while verifying the certificate for the domain.
    // reqwest ignores the port of the override; the request URL carries it.
    if let Ok(addr) = resolver.server_address.parse::<SocketAddr>() {
        builder = builder.resolve(&resolver.info.domain, addr);
    }
    builder
        .build()
        .map_err(|e| ResolveError::Http(format!("failed to create HTTP client: {}", e)))
}

#[async_trait::async_trait]
impl ResolverConn for HttpsResolver {
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
        if let Ok(new_client) = build_http_client(&self.resolver, self.request_timeout, self.extra_root.clone()) {
            let mut guard = self.client.write().unwrap();
            *guard = new_client;
        }
    }
}
