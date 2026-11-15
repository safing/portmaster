pub mod block;
pub mod conn;
pub mod error;
pub mod model;
pub mod transports;
pub mod url;

use std::sync::Arc;

pub use block::is_blocked_upstream;
pub use conn::{BasicResolverConn, Query, ResolverConn};
pub use error::{ParseResolverError, ResolveError};
pub use model::{
    IpScope, Resolver, ResolverInfo, Scope, BLOCK_DETECTION_DISABLED, BLOCK_DETECTION_EMPTY_ANSWER,
    BLOCK_DETECTION_REFUSED, BLOCK_DETECTION_ZERO_IP, HTTPS_PROTOCOL, SERVER_SOURCE_CONFIGURED,
    SERVER_SOURCE_ENV, SERVER_SOURCE_ETW, SERVER_SOURCE_FIREWALL, SERVER_SOURCE_MDNS,
    SERVER_SOURCE_OPERATING_SYSTEM, SERVER_SOURCE_SYSTEMD, SERVER_TYPE_DNS, SERVER_TYPE_DOH,
    SERVER_TYPE_DOT, SERVER_TYPE_ENV, SERVER_TYPE_FIREWALL, SERVER_TYPE_MDNS, SERVER_TYPE_MONITOR,
    SERVER_TYPE_TCP, TLS_PROTOCOL,
};
pub use transports::{HttpsResolver, PlainResolver, TcpResolver};
pub use url::{
    check_search_scope, configure_search_domains, create_resolver, create_resolver_with_local_net,
    format_host_and_port,
    format_ip_and_port, MAX_SEARCH_DOMAINS, SPECIAL_SERVICE_DOMAINS,
};

/// Creates the appropriate `ResolverConn` for the given resolver, mirroring Go `resolverConnFactory`.
pub fn resolver_conn_factory(resolver: &Resolver) -> Result<Arc<dyn ResolverConn>, ResolveError> {
    match resolver.info.r#type.as_str() {
        SERVER_TYPE_DNS => Ok(Arc::new(PlainResolver::new(resolver.clone()))),
        SERVER_TYPE_TCP => Ok(Arc::new(TcpResolver::new_tcp(resolver.clone()))),
        SERVER_TYPE_DOT => Ok(Arc::new(TcpResolver::new_dot(resolver.clone()))),
        SERVER_TYPE_DOH => Ok(Arc::new(HttpsResolver::new(resolver.clone())?)),
        other => Err(ResolveError::Failure(format!(
            "unsupported resolver connection type: {}",
            other
        ))),
    }
}
