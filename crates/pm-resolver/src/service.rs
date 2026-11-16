//! Port of Go `resolvers.go` and `scopes.go`: the list of active resolvers and
//! the selection of resolvers for a query. The orchestration lives in `resolve.rs`.

use std::collections::{HashMap, HashSet};
use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};

use async_trait::async_trait;
use tokio_util::sync::CancellationToken;

use crate::conn::{Query, ResolverConn};
use crate::env::Environment;
use crate::error::ResolveError;
use crate::model::{
    IpScope, Resolver, ResolverInfo, Scope, SERVER_SOURCE_CONFIGURED, SERVER_SOURCE_ENV,
    SERVER_SOURCE_MDNS, SERVER_SOURCE_OPERATING_SYSTEM, SERVER_TYPE_DNS, SERVER_TYPE_DOH,
    SERVER_TYPE_DOT, SERVER_TYPE_ENV, SERVER_TYPE_MDNS, SERVER_TYPE_TCP,
};
use crate::namerecord::NameRecordStore;
use crate::query_error::QueryError;
use crate::resolver_conn_factory;
use crate::scopes::{
    domain_in_scope, internal_special_use_domains, INVALID_DOMAIN, LOCALHOST_DOMAIN,
    MULTICAST_DOMAINS, SPECIAL_SERVICE_DOMAINS, SPECIAL_USE_DOMAINS,
};
use crate::settings::{default_name_servers, Settings};
use crate::url::{configure_search_domains, create_resolver_with_local_net, format_ip_and_port};

/// Creates the connection of a resolver.
pub type ConnFactory =
    Arc<dyn Fn(&Resolver) -> Result<Arc<dyn ResolverConn>, ResolveError> + Send + Sync>;

/// Source name of queries that are answered by special-use rules.
pub const PRIMARY_SOURCE_SPECIAL: &str = "special";

/// Why a resolver cannot be used for a query.
#[derive(Debug, Clone, Copy, PartialEq, Eq, thiserror::Error)]
pub enum ComplianceError {
    #[error("insecure protocols disabled")]
    InsecureProtocol,
    #[error("assigned (dhcp) nameservers disabled")]
    AssignedServer,
    #[error("multicast DNS disabled")]
    MulticastDns,
    #[error("query out of scope for resolver")]
    OutOfScope,
}

/// The state reported when the resolvers could not be loaded as configured.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum MissingResolversState {
    /// Resolvers were loaded as configured.
    #[default]
    None,
    /// Nothing valid was configured; the factory defaults are used (warning).
    UsingDefaults,
    /// No valid server at all (error).
    NoServers,
}

/// Connection of the environment and mDNS resolvers, which are not ported:
/// they never have an answer.
struct UnsupportedConn;

#[async_trait]
impl ResolverConn for UnsupportedConn {
    async fn query(&self, _q: Query) -> Result<hickory_proto::op::Message, ResolveError> {
        Err(ResolveError::NotFound)
    }
    fn report_failure(&self) -> bool {
        false
    }
    fn is_failing(&self) -> bool {
        false
    }
    fn reset_failure(&self) {}
    async fn force_reconnect(&self) {}
}

fn builtin_resolver(config_url: &str, resolver_type: &str, source: &str) -> Arc<Resolver> {
    Arc::new(Resolver {
        config_url: config_url.to_string(),
        info: ResolverInfo {
            name: String::new(),
            r#type: resolver_type.to_string(),
            source: source.to_string(),
            ip: None,
            domain: String::new(),
            ip_scope: IpScope::SiteLocal,
            port: 0,
        },
        server_address: String::new(),
        upstream_block_detection: String::new(),
        search: None,
        search_only: false,
        path: String::new(),
        link_local_unavailable: false,
        conn: Some(Arc::new(UnsupportedConn)),
    })
}

#[derive(Default)]
pub(crate) struct State {
    pub global: Vec<Arc<Resolver>>,
    pub local: Vec<Arc<Resolver>>,
    pub system: Vec<Arc<Resolver>>,
    pub scopes: Vec<Scope>,
    pub active: HashMap<String, Arc<Resolver>>,
    pub current_config: Vec<String>,
    pub init_domains: HashSet<String>,
}

#[derive(Default)]
pub(crate) struct CacheReset {
    pub id: String,
    pub seen_count: i32,
}

pub(crate) struct DedupeStatus {
    pub completed: tokio::sync::watch::Sender<bool>,
    pub wait_until: std::time::Instant,
    pub superseded: AtomicBool,
}

pub(crate) struct Inner {
    pub settings: Arc<dyn Settings>,
    pub env: Arc<dyn Environment>,
    pub store: NameRecordStore,
    pub factory: ConnFactory,
    pub state: RwLock<State>,
    pub env_resolvers: Vec<Arc<Resolver>>,
    pub mdns_resolvers: Vec<Arc<Resolver>>,
    pub dedupe: Mutex<HashMap<String, Arc<DedupeStatus>>>,
    pub cache_reset: Mutex<CacheReset>,
    pub shutdown: CancellationToken,
    pub disabled: AtomicBool,
    pub missing_state: Mutex<MissingResolversState>,
}

/// The resolver: active resolvers, scope selection, cache and failover.
#[derive(Clone)]
pub struct ResolverService {
    pub(crate) inner: Arc<Inner>,
}

impl ResolverService {
    /// Creates the service with the default transport factory.
    pub fn new(
        settings: Arc<dyn Settings>,
        env: Arc<dyn Environment>,
        store: NameRecordStore,
    ) -> ResolverService {
        ResolverService::with_factory(settings, env, store, Arc::new(resolver_conn_factory))
    }

    /// Creates the service with a custom connection factory (for tests).
    pub fn with_factory(
        settings: Arc<dyn Settings>,
        env: Arc<dyn Environment>,
        store: NameRecordStore,
        factory: ConnFactory,
    ) -> ResolverService {
        ResolverService {
            inner: Arc::new(Inner {
                settings,
                env,
                store,
                factory,
                state: RwLock::new(State::default()),
                env_resolvers: vec![builtin_resolver(
                    SERVER_SOURCE_ENV,
                    SERVER_TYPE_ENV,
                    SERVER_SOURCE_ENV,
                )],
                mdns_resolvers: vec![builtin_resolver(
                    SERVER_SOURCE_MDNS,
                    SERVER_TYPE_MDNS,
                    SERVER_SOURCE_MDNS,
                )],
                dedupe: Mutex::new(HashMap::new()),
                cache_reset: Mutex::new(CacheReset::default()),
                shutdown: CancellationToken::new(),
                disabled: AtomicBool::new(false),
                missing_state: Mutex::new(MissingResolversState::None),
            }),
        }
    }

    /// Stops accepting work: running and new resolves fail with `ShuttingDown`.
    pub fn shutdown(&self) {
        self.inner.shutdown.cancel();
    }

    /// Whether the user configured no DNS servers at all, so other modules
    /// know that the Portmaster resolver is not wanted.
    pub fn is_disabled(&self) -> bool {
        self.inner.disabled.load(Ordering::SeqCst)
    }

    /// The state about missing resolvers set by the last load.
    pub fn missing_resolvers_state(&self) -> MissingResolversState {
        *self.inner.missing_state.lock().expect("lock poisoned")
    }

    pub fn store(&self) -> &NameRecordStore {
        &self.inner.store
    }

    pub fn settings(&self) -> &Arc<dyn Settings> {
        &self.inner.settings
    }

    pub fn environment(&self) -> &Arc<dyn Environment> {
        &self.inner.env
    }

    fn create(&self, url: &str, source: &str) -> Result<Option<Resolver>, String> {
        let local_network = |ip: IpAddr| self.inner.env.is_local_network(ip);
        let (resolver, skip) = create_resolver_with_local_net(url, source, Some(&local_network))
            .map_err(|e| e.to_string())?;
        if skip {
            return Ok(None);
        }
        let Some(mut resolver) = resolver else {
            return Ok(None);
        };
        resolver.conn = Some((self.inner.factory)(&resolver).map_err(|e| e.to_string())?);
        Ok(Some(resolver))
    }

    fn configured_resolvers(&self, list: &[String]) -> Vec<Resolver> {
        let mut resolvers = Vec::new();
        for server in list {
            match self.create(server, SERVER_SOURCE_CONFIGURED) {
                Ok(Some(resolver)) => resolvers.push(resolver),
                Ok(None) => {}
                Err(err) => tracing::error!("resolver: cannot use resolver {server}: {err}"),
            }
        }
        resolvers
    }

    fn system_resolvers(&self) -> Vec<Resolver> {
        let mut resolvers = Vec::new();
        for nameserver in self.inner.env.nameservers() {
            let url = format!("dns://{}", format_ip_and_port(nameserver.ip, 53));
            match self.create(&url, SERVER_SOURCE_OPERATING_SYSTEM) {
                Ok(Some(mut resolver)) => {
                    if resolver.info.ip_scope.is_lan() {
                        // Invalid search domains are skipped, not fatal.
                        let _ = configure_search_domains(&mut resolver, &nameserver.search, false);
                    }
                    resolvers.push(resolver);
                }
                Ok(None) => {}
                Err(err) => tracing::error!("resolver: cannot use system resolver {url}: {err}"),
            }
        }
        resolvers
    }

    /// Loads the resolvers from the config and the system, like Go
    /// `loadResolvers`. Call it again when the config or the network changed.
    pub fn load_resolvers(&self) {
        let new_config = self.inner.settings.nameservers();

        self.inner
            .disabled
            .store(new_config.is_empty(), Ordering::SeqCst);

        let mut new_resolvers = self.configured_resolvers(&new_config);
        new_resolvers.extend(self.system_resolvers());

        let mut missing = MissingResolversState::None;
        if new_resolvers.is_empty() {
            // Load the defaults directly, overriding the config system.
            new_resolvers = self.configured_resolvers(&default_name_servers());
            if new_resolvers.is_empty() {
                tracing::error!(
                    "resolver: no (valid) dns server found in config, system or global defaults"
                );
                missing = MissingResolversState::NoServers;
            } else {
                tracing::warn!(
                    "resolver: no (valid) dns server found in config or system, falling back to global defaults"
                );
                missing = MissingResolversState::UsingDefaults;
            }
        }
        *self.inner.missing_state.lock().expect("lock poisoned") = missing;

        let global: Vec<Arc<Resolver>> = new_resolvers.into_iter().map(Arc::new).collect();
        let (local, system, scopes) = self.scoped_resolvers(&global);

        let mut active: HashMap<String, Arc<Resolver>> = HashMap::new();
        for resolver in &global {
            active.insert(resolver.info.id(), Arc::clone(resolver));
        }
        for builtin in self
            .inner
            .mdns_resolvers
            .iter()
            .chain(&self.inner.env_resolvers)
        {
            active.insert(builtin.info.id(), Arc::clone(builtin));
        }

        let init_domains = init_domains(&global);

        if global.is_empty() && local.is_empty() {
            tracing::error!("resolver: no resolvers loaded!");
        }

        let (config_changed, all_failing) = {
            let mut state = self.inner.state.write().expect("lock poisoned");
            // Compare and update under one lock, so concurrent loads cannot both see a change.
            let config_changed =
                !state.current_config.is_empty() && state.current_config != new_config;
            state.global = global;
            state.local = local;
            state.system = system;
            state.scopes = scopes;
            state.active = active;
            state.current_config = new_config;
            state.init_domains = init_domains;
            (config_changed, all_configured_failing(&state))
        };
        // Clear the name cache when the settings changed.
        if config_changed {
            tracing::info!("resolver: clearing dns cache due to changed resolver config");
            if let Err(err) = self.inner.store.clear() {
                tracing::warn!("resolver: failed to clear dns cache: {err}");
            }
        }
        if !all_failing {
            self.inner.env.failing_resolvers_changed(false);
        }
    }

    /// Splits the resolvers into local ones, system ones and search scopes.
    fn scoped_resolvers(
        &self,
        resolvers: &[Arc<Resolver>],
    ) -> (Vec<Arc<Resolver>>, Vec<Arc<Resolver>>, Vec<Scope>) {
        let mut local = Vec::new();
        let mut system = Vec::new();
        let mut scopes: Vec<Scope> = Vec::new();

        for resolver in resolvers {
            let is_local = resolver.info.ip_scope.is_lan()
                || resolver
                    .info
                    .ip
                    .is_some_and(|ip| self.inner.env.is_local_network(ip));
            if is_local {
                local.push(Arc::clone(resolver));
            }

            if resolver.info.source == SERVER_SOURCE_OPERATING_SYSTEM {
                system.push(Arc::clone(resolver));
            }

            if let Some(search) = &resolver.search {
                // Add the resolver to the custom searches.
                for domain in search {
                    if domain == "." {
                        continue;
                    }
                    match scopes.iter_mut().find(|scope| &scope.domain == domain) {
                        Some(scope) => scope.resolvers.push(Arc::clone(resolver)),
                        None => scopes.push(Scope {
                            domain: domain.clone(),
                            resolvers: vec![Arc::clone(resolver)],
                        }),
                    }
                }
            }
        }

        // Longest scopes first.
        scopes.sort_by_key(|scope| std::cmp::Reverse(scope.domain.len()));
        (local, system, scopes)
    }

    /// The global resolvers (configured, then system), for status exports.
    pub fn global_resolvers(&self) -> Vec<Arc<Resolver>> {
        self.inner
            .state
            .read()
            .expect("lock poisoned")
            .global
            .clone()
    }

    /// The local resolvers.
    pub fn local_resolvers(&self) -> Vec<Arc<Resolver>> {
        self.inner
            .state
            .read()
            .expect("lock poisoned")
            .local
            .clone()
    }

    /// The resolvers assigned by the system.
    pub fn assigned_resolvers(&self) -> Vec<Arc<Resolver>> {
        self.inner
            .state
            .read()
            .expect("lock poisoned")
            .system
            .clone()
    }

    /// The search scopes with their resolvers, longest first.
    pub fn scopes(&self) -> Vec<Scope> {
        self.inner
            .state
            .read()
            .expect("lock poisoned")
            .scopes
            .clone()
    }

    /// Returns the active resolver with the given ID.
    pub fn active_resolver_by_id(&self, id: &str) -> Option<Arc<Resolver>> {
        self.inner
            .state
            .read()
            .expect("lock poisoned")
            .active
            .get(id)
            .cloned()
    }

    /// Returns whether the IP and port match a configured resolver.
    pub fn is_resolver_address(&self, ip: IpAddr, port: u16) -> bool {
        let state = self.inner.state.read().expect("lock poisoned");
        state
            .global
            .iter()
            .any(|r| r.info.port == port && r.info.ip.is_some_and(|rip| rip == ip))
    }

    /// Forces all resolvers to reconnect.
    pub async fn force_resolver_reconnect(&self) {
        let resolvers = self.global_resolvers();
        tracing::trace!("resolver: forcing all active resolvers to reconnect");
        for resolver in resolvers {
            if let Some(conn) = &resolver.conn {
                conn.force_reconnect().await;
            }
        }
        tracing::info!("resolver: all active resolvers were forced to reconnect");
    }

    /// Returns whether all configured resolvers are failing. `false` if there
    /// are no configured resolvers.
    pub fn all_configured_resolvers_are_failing(&self) -> bool {
        all_configured_failing(&self.inner.state.read().expect("lock poisoned"))
    }

    /// Returns all resolvers that are in scope to resolve the query, the
    /// primary source and whether all of them should be tried, like Go
    /// `GetResolversInScope`.
    pub fn get_resolvers_in_scope(&self, q: &Query) -> (Vec<Arc<Resolver>>, &'static str, bool) {
        let state = self.inner.state.read().expect("lock poisoned");
        let dot_prefixed = dot_prefixed_fqdn(&q.fqdn);
        let mut selected: Vec<Arc<Resolver>> = Vec::new();

        // Internal use domains.
        if domain_in_scope(&dot_prefixed, &internal_special_use_domains()) {
            return (self.inner.env_resolvers.clone(), SERVER_SOURCE_ENV, false);
        }

        // Special connectivity domains.
        if self.inner.env.is_connectivity_domain(&q.fqdn) && !state.system.is_empty() {
            self.add_resolvers(&state, q, &mut selected, &state.system);
            if selected.is_empty() {
                self.add_resolvers(&state, q, &mut selected, &state.local);
                self.add_resolvers(&state, q, &mut selected, &state.global);
            }
            return (selected, SERVER_SOURCE_OPERATING_SYSTEM, false);
        }

        // Prioritize search scopes.
        for scope in &state.scopes {
            if dot_prefixed.ends_with(&scope.domain) {
                self.add_resolvers(&state, q, &mut selected, &scope.resolvers);
            }
        }

        // Handle multicast domains. Unicast resolvers are tried first: DHCP
        // assigned DNS servers often have authority over .local domains and
        // answer immediately, while mDNS costs a timeout when it has no answer.
        if domain_in_scope(&dot_prefixed, MULTICAST_DOMAINS) {
            self.add_resolvers(&state, q, &mut selected, &state.local);
            self.add_resolvers(&state, q, &mut selected, &state.system);
            self.add_resolvers(&state, q, &mut selected, &self.inner.mdns_resolvers);
            return (selected, SERVER_SOURCE_MDNS, true);
        }

        // Special use domains.
        if domain_in_scope(&dot_prefixed, SPECIAL_USE_DOMAINS)
            || domain_in_scope(&dot_prefixed, SPECIAL_SERVICE_DOMAINS)
        {
            self.add_resolvers(&state, q, &mut selected, &state.local);
            return (selected, PRIMARY_SOURCE_SPECIAL, true);
        }

        // Global domains.
        self.add_resolvers(&state, q, &mut selected, &state.global);
        (selected, SERVER_SOURCE_CONFIGURED, false)
    }

    fn add_resolvers(
        &self,
        state: &State,
        q: &Query,
        selected: &mut Vec<Arc<Resolver>>,
        add: &[Arc<Resolver>],
    ) {
        'next: for resolver in add {
            // Check for compliance.
            if let Err(err) = self.check_resolver_compliance(resolver, q) {
                tracing::trace!(
                    "resolver: skipping non-compliant resolver {}: {err}",
                    resolver.info.descriptive_name()
                );
                continue;
            }

            // Deduplicate.
            let id = resolver.info.id();
            if selected.iter().any(|s| s.info.id() == id) {
                continue 'next;
            }

            // The domains of the configured resolvers should not be resolved
            // with the same resolvers.
            if resolver.info.source == SERVER_SOURCE_CONFIGURED
                && resolver.info.ip.is_none()
                && state.init_domains.contains(&q.fqdn)
            {
                continue 'next;
            }

            selected.push(Arc::clone(resolver));
        }
    }

    /// Checks whether the resolver may be used for the query with the current settings.
    pub fn check_resolver_compliance(
        &self,
        resolver: &Resolver,
        q: &Query,
    ) -> Result<(), ComplianceError> {
        let settings = &self.inner.settings;

        if settings.no_insecure_protocols() {
            match resolver.info.r#type.as_str() {
                // Compliant. The environment is sourced from the local network only.
                SERVER_TYPE_DOT | SERVER_TYPE_DOH | SERVER_TYPE_ENV => {}
                SERVER_TYPE_DNS | SERVER_TYPE_TCP => return Err(ComplianceError::InsecureProtocol),
                _ => return Err(ComplianceError::InsecureProtocol),
            }
        }

        if settings.no_assigned_nameservers()
            && resolver.info.source == SERVER_SOURCE_OPERATING_SYSTEM
        {
            return Err(ComplianceError::AssignedServer);
        }

        if settings.no_multicast_dns() && resolver.info.source == SERVER_SOURCE_MDNS {
            return Err(ComplianceError::MulticastDns);
        }

        // Check if the resolver should only be used for the search scopes.
        if resolver.search_only {
            let in_scope = resolver
                .search
                .as_ref()
                .is_some_and(|search| domain_in_scope(&dot_prefixed_fqdn(&q.fqdn), search));
            if !in_scope {
                return Err(ComplianceError::OutOfScope);
            }
        }

        Ok(())
    }

    /// Checks the query itself against the special domain rules.
    pub fn check_query_compliance(&self, q: &Query) -> Result<(), QueryError> {
        let dot_prefixed = dot_prefixed_fqdn(&q.fqdn);

        // RFC6761: always respond with NXDOMAIN.
        if dot_prefixed.ends_with(INVALID_DOMAIN) {
            return Err(QueryError::NotFound);
        }

        // RFC6761: respond with 127.0.0.1 and ::1 to A and AAAA queries, else NXDOMAIN.
        if dot_prefixed.ends_with(LOCALHOST_DOMAIN) {
            return Err(match q.qtype {
                hickory_proto::rr::RecordType::A | hickory_proto::rr::RecordType::AAAA => {
                    QueryError::Localhost
                }
                _ => QueryError::NotFound,
            });
        }

        // Special TLDs.
        if self.inner.settings.dont_resolve_special_domains()
            && domain_in_scope(&dot_prefixed, SPECIAL_SERVICE_DOMAINS)
        {
            return Err(QueryError::SpecialDomainsDisabled);
        }

        Ok(())
    }
}

/// Returns the FQDN with a leading dot, like Go `dotPrefixedFQDN`.
pub fn dot_prefixed_fqdn(fqdn: &str) -> String {
    if fqdn == "." {
        ".".to_string()
    } else {
        format!(".{fqdn}")
    }
}

fn all_configured_failing(state: &State) -> bool {
    // No configured resolvers: not failing.
    if state.current_config.is_empty() {
        return false;
    }
    // Not failing if any configured resolver is working.
    !state.global.iter().any(|r| {
        r.info.source == SERVER_SOURCE_CONFIGURED
            && r.conn.as_ref().is_some_and(|c| !c.is_failing())
    })
}

/// Domains of resolvers configured with a hostname; they must not be resolved
/// by resolvers that need to resolve a hostname themselves.
fn init_domains(resolvers: &[Arc<Resolver>]) -> HashSet<String> {
    resolvers
        .iter()
        .filter(|r| matches!(r.info.r#type.as_str(), SERVER_TYPE_DOT | SERVER_TYPE_DOH))
        .filter(|r| host_is_domain(&r.config_url))
        .map(|r| fqdn(&r.info.domain))
        .collect()
}

fn host_is_domain(config_url: &str) -> bool {
    match url::Url::parse(config_url).map(|u| {
        u.host()
            .map(|h| matches!(h, url::Host::Domain(d) if d.parse::<IpAddr>().is_err()))
    }) {
        Ok(Some(is_domain)) => is_domain,
        _ => false,
    }
}

fn fqdn(domain: &str) -> String {
    if domain.ends_with('.') {
        domain.to_string()
    } else {
        format!("{domain}.")
    }
}
