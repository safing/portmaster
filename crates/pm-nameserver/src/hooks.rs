//! The injection points of the nameserver: the resolver, the request
//! attribution and the firewall step.

use std::net::SocketAddr;
use std::sync::Arc;

use async_trait::async_trait;
use hickory_proto::rr::{Record, RecordType};
use pm_netstate::IpScope;
use pm_network::{Connection, Network, OpenDnsQuery, SharedConnection};
use pm_resolver::{Query, RRCache, ResolveFailure, ResolverService};
use tokio_util::sync::CancellationToken;

use crate::responder::Responder;

/// The resolver the nameserver asks (Go `resolver.Resolve` and friends).
#[async_trait]
pub trait DnsResolve: Send + Sync {
    /// Resolves the query.
    async fn resolve(
        &self,
        q: Query,
        cancel: &CancellationToken,
    ) -> Result<RRCache, ResolveFailure>;

    /// The IP scope of the first resolver for the query (Go
    /// `GetResolversInScope`), used when nothing was resolved.
    fn resolver_ip_scope(&self, _q: &Query) -> Option<IpScope> {
        None
    }

    /// Asks the Portmaster environment resolver (Go `QueryPortmasterEnv`).
    async fn query_portmaster_env(&self, _q: &Query) -> Option<RRCache> {
        None
    }

    /// Reads a cache entry (Go `GetRRCache`).
    fn get_rr_cache(&self, _domain: &str, _qtype: RecordType) -> Option<RRCache> {
        None
    }
}

#[async_trait]
impl DnsResolve for ResolverService {
    async fn resolve(
        &self,
        q: Query,
        cancel: &CancellationToken,
    ) -> Result<RRCache, ResolveFailure> {
        self.resolve_with_cancel(q, cancel).await
    }

    fn resolver_ip_scope(&self, q: &Query) -> Option<IpScope> {
        let (resolvers, _, _) = self.get_resolvers_in_scope(q);
        resolvers.first().map(|r| r.info.ip_scope)
    }

    fn get_rr_cache(&self, domain: &str, qtype: RecordType) -> Option<RRCache> {
        self.store().get_rrcache(domain, qtype).ok()
    }
}

/// What the nameserver knows about a request before attributing it.
#[derive(Debug, Clone)]
pub struct DnsRequestInfo {
    pub fqdn: String,
    pub conn_id: String,
    pub remote: SocketAddr,
    /// The request comes from this device.
    pub local: bool,
}

/// Identifies the process behind a request: Go
/// `NewConnectionFromDNSRequest` / `NewConnectionFromExternalDNSRequest`.
#[async_trait]
pub trait RequestAttributor: Send + Sync {
    async fn attribute(&self, request: &DnsRequestInfo) -> Result<SharedConnection, String>;

    /// Go `Process.IsSystemResolver`.
    fn is_system_resolver(&self, _conn: &Connection) -> bool {
        false
    }
}

/// [`RequestAttributor`] on top of the connection store.
pub struct NetworkAttributor {
    pub network: Arc<Network>,
}

#[async_trait]
impl RequestAttributor for NetworkAttributor {
    async fn attribute(&self, request: &DnsRequestInfo) -> Result<SharedConnection, String> {
        let network = Arc::clone(&self.network);
        let request = request.clone();
        // The process lookup reads the proc file system.
        tokio::task::spawn_blocking(move || {
            let ip = request.remote.ip().to_canonical();
            if request.local {
                network.new_connection_from_dns_request(
                    &request.fqdn,
                    Vec::new(),
                    &request.conn_id,
                    ip,
                    request.remote.port(),
                )
            } else {
                network.new_connection_from_external_dns_request(
                    &request.fqdn,
                    Vec::new(),
                    &request.conn_id,
                    ip,
                )
            }
        })
        .await
        .map_err(|e| e.to_string())
    }
}

/// The outcome of a firewall step besides the verdict on the connection.
#[derive(Default, Clone)]
pub struct FilterOutcome {
    /// The firewall wants to craft the reply itself (Go: `Reason.Context`
    /// implements `Responder`).
    pub responder: Option<Arc<dyn Responder>>,
    /// Records for the extra section that justify the verdict (Go: `Reason.Context`
    /// implements `RRProvider`).
    pub extra_rrs: Vec<Record>,
}

/// The firewall steps of a DNS request. Verdicts are set on the connection.
#[async_trait]
pub trait FirewallHook: Send + Sync {
    /// Go `FilterConnection(ctx, conn, nil, true, false)`, before resolving.
    async fn filter_connection(&self, conn: &SharedConnection) -> FilterOutcome;

    /// Go `FilterResolvedDNS`; returns the (possibly filtered) answer, or
    /// `None` if there is none.
    async fn filter_resolved_dns(
        &self,
        conn: &SharedConnection,
        q: &Query,
        rr_cache: RRCache,
    ) -> (Option<RRCache>, FilterOutcome);

    /// Go `UpdateIPsAndCNAMEs`.
    fn update_ips_and_cnames(&self, _q: &Query, _rr: &RRCache, _conn: &SharedConnection) {}

    /// Go `network.SaveOpenDNSRequest`.
    fn save_open_dns_request(&self, _q: &Query, _rr: &RRCache, _conn: &SharedConnection) {}

    /// Go `Connection.Save`, for blocked, dropped and failed requests.
    fn save_connection(&self, _conn: &SharedConnection) {}
}

/// Allows everything. Requests are saved through the connection store when
/// one is given.
#[derive(Default)]
pub struct PermissiveHook {
    pub network: Option<Arc<Network>>,
}

#[async_trait]
impl FirewallHook for PermissiveHook {
    async fn filter_connection(&self, _conn: &SharedConnection) -> FilterOutcome {
        FilterOutcome::default()
    }

    async fn filter_resolved_dns(
        &self,
        _conn: &SharedConnection,
        _q: &Query,
        rr_cache: RRCache,
    ) -> (Option<RRCache>, FilterOutcome) {
        (Some(rr_cache), FilterOutcome::default())
    }

    fn save_open_dns_request(&self, q: &Query, rr: &RRCache, conn: &SharedConnection) {
        if let Some(network) = &self.network {
            let query = OpenDnsQuery {
                qtype: u16::from(q.qtype),
                served_from_cache: rr.served_from_cache,
                requesting_new: rr.requesting_new,
            };
            network.save_open_dns_request(Some(&query), conn);
        }
    }

    fn save_connection(&self, conn: &SharedConnection) {
        if let Some(network) = &self.network {
            network.save(conn);
        }
    }
}
