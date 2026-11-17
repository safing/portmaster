//! Port of Go `resolve.go` and `failing.go`: the resolve flow with cache,
//! request deduplication, failover and the recheck of failing resolvers.

use std::net::IpAddr;
use std::sync::atomic::Ordering;
use std::sync::Arc;
use std::time::{Duration, Instant};

use hickory_proto::rr::RecordType;
use pm_netenv::OnlineStatus;
use tokio_util::sync::CancellationToken;

use crate::conn::Query;
use crate::env::DNS_TEST_DOMAIN;
use crate::error::ResolveError;
use crate::model::{Resolver, SERVER_SOURCE_CONFIGURED, SERVER_SOURCE_ENV};
use crate::namerecord::NameRecordError;
use crate::query_error::{QueryError, ResolveFailure};
use crate::rrcache::{
    qtype_string, CleanContext, RRCache, MIN_TTL, RCODE_NAME_ERROR, RCODE_REFUSED, RCODE_SUCCESS,
};
use crate::service::{DedupeStatus, ResolverService};

/// Maximum time to wait for a request that is already running for the same query.
pub const MAX_REQUEST_TIMEOUT: Duration = Duration::from_secs(5);

/// Number of times the same query must be seen in a row to reset its cache entry.
const CACHE_RESET_THRESHOLD: i32 = 3;

/// Returns the ID of a query: domain and query type.
pub fn query_id(q: &Query) -> String {
    format!("{}{}", q.fqdn, qtype_string(q.qtype))
}

/// Runs the basic checks and normalizes the FQDN, like Go `Query.check`.
fn check_query(q: &mut Query) -> bool {
    if q.fqdn.is_empty() {
        return false;
    }
    if !q.fqdn.ends_with('.') {
        q.fqdn.push('.');
    }
    true
}

/// Marks a deduplicated request as finished when dropped.
pub(crate) struct DedupeGuard {
    service: ResolverService,
    key: String,
    status: Arc<DedupeStatus>,
}

impl Drop for DedupeGuard {
    fn drop(&mut self) {
        let mut map = self.service.inner.dedupe.lock().expect("lock poisoned");
        // Mark the request as done.
        self.status.completed.send_replace(true);
        // Delete it from the registry, if it is still ours.
        if !self.status.superseded.load(Ordering::SeqCst) {
            if let Some(current) = map.get(&self.key) {
                if Arc::ptr_eq(current, &self.status) {
                    map.remove(&self.key);
                }
            }
        }
    }
}

/// The result of asking all resolvers in scope.
struct QueryOutcome {
    result: Result<RRCache, ResolveFailure>,
    /// A resolver answered (not a backup cache).
    resolver_succeeded: bool,
    failure_reported: bool,
}

impl ResolverService {
    /// Resolves the query, like Go `Resolve`.
    pub async fn resolve(&self, q: Query) -> Result<RRCache, ResolveFailure> {
        self.resolve_with_cancel(q, &CancellationToken::new()).await
    }

    /// Resolves the query; cancelling the token aborts it.
    pub async fn resolve_with_cancel(
        &self,
        mut q: Query,
        cancel: &CancellationToken,
    ) -> Result<RRCache, ResolveFailure> {
        if !check_query(&mut q) {
            return Err(QueryError::Invalid.into());
        }
        tracing::trace!("resolver: resolving {}{}", q.fqdn, qtype_string(q.qtype));

        // Check query compliance.
        self.check_query_compliance(&q)?;

        let mut rr_cache = None;
        let mut _guard = None;
        if !q.no_caching {
            rr_cache = self.check_cache(&q);
            if let Some(cache) = rr_cache.as_mut() {
                if !cache.expired() {
                    // Return non-expired cached entry immediately.
                    return Ok(cache.clone());
                }
                if cache.rcode == RCODE_SUCCESS && self.inner.settings.use_stale_cache() {
                    // Return the expired cache, but start an async query instead.
                    self.start_async_query(&q, cache);
                    return Ok(cache.clone());
                }
            }

            // Deduplicate!
            match self.deduplicate_request(&q, cancel).await {
                None => {
                    // We waited for another request, recheck the cache!
                    rr_cache = self.check_cache(&q);
                    if let Some(cache) = &rr_cache {
                        if !cache.expired() || self.inner.settings.use_stale_cache() {
                            // There just was a request, so do not trigger an async query.
                            return Ok(cache.clone());
                        }
                    }
                    tracing::debug!(
                        "resolver: waited for another {} query, but cache missed!",
                        query_id(&q)
                    );
                    // If the cache is still empty or non-compliant, just query.
                }
                Some(guard) => {
                    // We are the first!
                    _guard = Some(guard);
                }
            }
        }

        self.resolve_and_cache(&q, rr_cache, cancel).await
    }

    /// Gets a usable cache entry for the query, if any.
    fn check_cache(&self, q: &Query) -> Option<RRCache> {
        // Never ask the cache for connectivity domains.
        if self.inner.env.is_connectivity_domain(&q.fqdn) {
            return None;
        }

        // Get data from the cache.
        let mut rr_cache = match self.inner.store.get_rrcache(&q.fqdn, q.qtype) {
            Ok(cache) => cache,
            Err(NameRecordError::NotFound) => return None,
            Err(err) => {
                tracing::warn!(
                    "resolver: getting RRCache {} from database failed: {err}",
                    query_id(q)
                );
                return None;
            }
        };

        // Get the resolver that the entry was resolved with.
        let resolver_id = rr_cache.resolver.as_ref()?.id();
        let Some(resolver) = self.active_resolver_by_id(&resolver_id) else {
            tracing::debug!(
                "resolver: ignoring RRCache {} because source server {resolver_id:?} has been removed",
                query_id(q)
            );
            return None;
        };

        // Check compliance of the resolver.
        if let Err(err) = self.check_resolver_compliance(&resolver, q) {
            tracing::debug!(
                "resolver: cached entry for {} does not comply to query parameters: {err}",
                query_id(q)
            );
            return None;
        }

        if self.should_reset_cache(q) {
            // Reset the cache entry that keeps being asked for.
            match self.inner.store.reset(&q.fqdn, &qtype_string(q.qtype)) {
                Ok(()) => tracing::info!("resolver: cache for {} was reset", query_id(q)),
                Err(NameRecordError::NotFound) => {
                    tracing::trace!(
                        "resolver: cache for {} was already reset (is empty)",
                        query_id(q)
                    )
                }
                Err(err) => {
                    tracing::warn!("resolver: failed to reset cache for {}: {err}", query_id(q))
                }
            }
            return None;
        }

        if rr_cache.expired() {
            // The expired entry is used if the new query fails, unless it is a
            // failure itself.
            return (rr_cache.rcode == RCODE_SUCCESS).then_some(rr_cache);
        }

        if rr_cache.expires_soon() {
            // Refresh in the background.
            self.start_async_query(q, &mut rr_cache);
            return Some(rr_cache);
        }

        Some(rr_cache)
    }

    /// Starts a background refresh of the entry, unless one is running.
    pub(crate) fn start_async_query(&self, q: &Query, current: &mut RRCache) {
        if current.requesting_new {
            return;
        }
        current.requesting_new = true;
        tracing::trace!("resolver: refreshing {} async", query_id(q));

        let service = self.clone();
        let q = q.clone();
        let Ok(handle) = tokio::runtime::Handle::try_current() else {
            return;
        };
        handle.spawn(async move {
            let cancel = service.inner.shutdown.child_token();
            match service.resolve_and_cache(&q, None, &cancel).await {
                Ok(_) => tracing::info!("resolver: async query for {} succeeded", query_id(&q)),
                Err(err) => {
                    tracing::warn!("resolver: async query for {} failed: {err}", query_id(&q))
                }
            }
        });
    }

    /// Waits for an identical request that is already running. Returns `None`
    /// if it waited, or a guard if this is the request that should run.
    async fn deduplicate_request(
        &self,
        q: &Query,
        cancel: &CancellationToken,
    ) -> Option<DedupeGuard> {
        let key = query_id(q);

        loop {
            let mut receiver = {
                let mut map = self.inner.dedupe.lock().expect("lock poisoned");
                let mut wait = None;
                if let Some(status) = map.get(&key) {
                    if Instant::now() < status.wait_until {
                        // Someone else is already on it!
                        wait = Some((status.completed.subscribe(), status.wait_until));
                    } else {
                        // But that someone is taking too long.
                        status.superseded.store(true, Ordering::SeqCst);
                    }
                }

                match wait {
                    Some(wait) => wait,
                    None => {
                        // We are currently the only one doing a request for this.
                        let (sender, _) = tokio::sync::watch::channel(false);
                        let status = Arc::new(DedupeStatus {
                            completed: sender,
                            wait_until: Instant::now() + MAX_REQUEST_TIMEOUT,
                            superseded: false.into(),
                        });
                        map.insert(key.clone(), Arc::clone(&status));
                        return Some(DedupeGuard {
                            service: self.clone(),
                            key,
                            status,
                        });
                    }
                }
            };

            tracing::trace!("resolver: waiting for duplicate query for {key} to complete");
            let deadline = tokio::time::Instant::from_std(receiver.1);
            tokio::select! {
                result = receiver.0.wait_for(|done| *done) => {
                    // Done, or the request was dropped without finishing.
                    let _ = result;
                    return None;
                }
                _ = tokio::time::sleep_until(deadline) => {
                    // Something went wrong with the query, retry.
                    continue;
                }
                _ = cancel.cancelled() => return None,
            }
        }
    }

    fn should_reset_cache(&self, q: &Query) -> bool {
        let mut reset = self.inner.cache_reset.lock().expect("lock poisoned");

        // Reset to a new domain.
        let id = query_id(q);
        if id != reset.id {
            reset.id = id;
            reset.seen_count = 1;
            return false;
        }

        // Increase and check if the threshold is reached.
        reset.seen_count += 1;
        if reset.seen_count >= CACHE_RESET_THRESHOLD {
            reset.seen_count = -7; // 10 for follow-up resets
            return true;
        }
        false
    }

    fn clean_context(&self, domain: &str) -> CleanContext {
        CleanContext {
            connectivity_domain: self.inner.env.is_connectivity_domain(domain),
            online: self.inner.env.online(),
        }
    }

    /// Reports a failure of the resolver, unless the device is offline.
    fn report_failure(&self, resolver: &Resolver) {
        // Do not mark resolvers as failed if we are offline.
        if !self.inner.env.online() {
            return;
        }
        let Some(conn) = &resolver.conn else { return };
        // Ignore the report when the resolver is already failing.
        if conn.is_failing() {
            return;
        }
        // Report that a configured server failed, only if the failure was counted.
        if conn.report_failure() && resolver.info.source == SERVER_SOURCE_CONFIGURED {
            self.inner.env.set_connected_to_dns(false);
        }
    }

    /// Reports a successful connection to the resolver.
    fn reset_failure(&self, resolver: &Resolver) {
        if let Some(conn) = &resolver.conn {
            conn.reset_failure();
        }
        if resolver.info.source == SERVER_SOURCE_CONFIGURED {
            self.inner.env.set_connected_to_dns(true);
        }
    }

    fn is_offline_for(&self, q: &Query) -> bool {
        self.inner.env.online_status() == OnlineStatus::Offline
            && q.fqdn != DNS_TEST_DOMAIN
            && !self.inner.env.is_connectivity_domain(&q.fqdn)
    }

    pub(crate) async fn resolve_and_cache(
        &self,
        q: &Query,
        old_cache: Option<RRCache>,
        cancel: &CancellationToken,
    ) -> Result<RRCache, ResolveFailure> {
        // Get resolvers.
        let (resolvers, primary_source, try_all) = self.get_resolvers_in_scope(q);
        if resolvers.is_empty() {
            return Err(QueryError::NoCompliance.into());
        }

        // Check if we are online.
        if self.inner.env.online_status() == OnlineStatus::Offline
            && primary_source != SERVER_SOURCE_ENV
        {
            if self.is_offline_for(q) {
                // We are offline and this is not an online check query.
                return Err(ResolveFailure::with_stale(QueryError::Offline, old_cache));
            }
            tracing::debug!(
                "resolver: allowing online status test domain {} to resolve even though offline",
                q.fqdn
            );
        }

        let outcome = self
            .query_resolvers(q, old_cache, resolvers, try_all, cancel)
            .await;

        // Report when all configured resolvers are failing.
        if outcome.failure_reported
            && self.inner.env.online()
            && primary_source == SERVER_SOURCE_CONFIGURED
            && self.all_configured_resolvers_are_failing()
        {
            self.inner.env.failing_resolvers_changed(true);
        } else if outcome.resolver_succeeded
            && primary_source == SERVER_SOURCE_CONFIGURED
            && !self.all_configured_resolvers_are_failing()
        {
            self.inner.env.failing_resolvers_changed(false);
        }

        outcome.result
    }

    async fn query_resolvers(
        &self,
        q: &Query,
        old_cache: Option<RRCache>,
        resolvers: Vec<Arc<Resolver>>,
        try_all: bool,
        cancel: &CancellationToken,
    ) -> QueryOutcome {
        let mut failure_reported = false;

        // Ask failing resolvers last, but still ask them: asking a failing
        // resolver is better than answering nothing, and an answer clears its
        // failing state.
        let resolvers = order_resolvers(resolvers);
        // Set once a resolver answered, even with "not found". Some scopes
        // continue to the next resolver after "not found". A failing resolver
        // is skipped then: its timeout would replace "not found" with a failure.
        let mut answered = false;

        let mut rr_cache: Option<RRCache> = None;
        let mut err: Option<QueryError> = None;

        for resolver in &resolvers {
            if self.inner.shutdown.is_cancelled() {
                return QueryOutcome {
                    resolver_succeeded: false,
                    result: Err(QueryError::ShuttingDown.into()),
                    failure_reported,
                };
            }

            // Skip unreachable link-local resolvers.
            if resolver.link_local_unavailable {
                tracing::trace!("resolver: skipping resolver {resolver}, because it is link-local and not available");
                continue;
            }
            let Some(conn) = &resolver.conn else { continue };

            if conn.is_failing() {
                if answered {
                    tracing::trace!("resolver: skipping failing resolver {resolver}, because another resolver answered");
                    continue;
                }
                tracing::trace!("resolver: asking failing resolver {resolver}, because no resolver answered so far");
            }

            // Resolve.
            tracing::trace!(
                "resolver: sending query for {} to {}",
                query_id(q),
                resolver.info.id()
            );
            let reply = tokio::select! {
                reply = conn.query(q.clone()) => reply,
                _ = cancel.cancelled() => Err(ResolveError::Cancelled),
                _ = self.inner.shutdown.cancelled() => Err(ResolveError::Cancelled),
            };

            rr_cache = None;
            err = None;
            match reply {
                Ok(message) => {
                    rr_cache = Some(RRCache::from_reply(
                        &q.fqdn,
                        q.qtype,
                        message,
                        &resolver.info,
                    ));
                }
                Err(error) => match error {
                    ResolveError::NotFound => {
                        // NXDomain, or similar.
                        if try_all {
                            answered = true;
                            err = Some(QueryError::NotFound);
                            continue;
                        }
                        return QueryOutcome {
                            resolver_succeeded: false,
                            result: Err(QueryError::NotFound.into()),
                            failure_reported,
                        };
                    }
                    ResolveError::BlockedUpstream(_) => {
                        // Some resolvers might also block.
                        return QueryOutcome {
                            resolver_succeeded: false,
                            result: Err(QueryError::BlockedUpstream {
                                resolver_name: resolver.info.descriptive_name(),
                            }
                            .into()),
                            failure_reported,
                        };
                    }
                    _ if self.is_offline_for(q) => {
                        // We are offline and this is not an online check query.
                        return QueryOutcome {
                            resolver_succeeded: false,
                            result: Err(ResolveFailure::with_stale(QueryError::Offline, old_cache)),
                            failure_reported,
                        };
                    }
                    ResolveError::Continue => {
                        err = Some(QueryError::Continue);
                        continue;
                    }
                    ResolveError::Timeout => {
                        self.report_failure(resolver);
                        failure_reported = true;
                        tracing::debug!("resolver: query to {} timed out", resolver.info.id());
                        err = Some(QueryError::Timeout);
                        continue;
                    }
                    ResolveError::Cancelled => {
                        let error = if self.inner.shutdown.is_cancelled() {
                            QueryError::ShuttingDown
                        } else {
                            QueryError::Cancelled
                        };
                        return QueryOutcome {
                            resolver_succeeded: false,
                            result: Err(error.into()),
                            failure_reported,
                        };
                    }
                    other => {
                        self.report_failure(resolver);
                        failure_reported = true;
                        tracing::warn!("resolver: query to {} failed: {other}", resolver.info.id());
                        err = Some(QueryError::Other(other.to_string()));
                        continue;
                    }
                },
            }

            // Check if the request succeeded and whether we should try another resolver.
            let Some(cache) = rr_cache.as_ref() else {
                continue;
            };
            if cache.rcode != RCODE_SUCCESS && try_all {
                answered = true;
                continue;
            }

            // Report a successful connection.
            self.reset_failure(resolver);
            break;
        }

        // Validate the return values.
        if err.is_none() && rr_cache.is_none() {
            err = Some(QueryError::NotFound);
        }

        // Handle the error.
        if let Some(error) = err {
            // Check if we can return an older cache instead of the error.
            if let Some(mut old) = old_cache {
                old.is_backup = true;
                tracing::debug!(
                    "resolver: serving backup cache of {} because query failed: {error}",
                    query_id(q)
                );
                return QueryOutcome {
                    resolver_succeeded: false,
                    result: Ok(old),
                    failure_reported,
                };
            }
            return QueryOutcome {
                resolver_succeeded: false,
                result: Err(QueryError::AllFailed {
                    count: resolvers.len(),
                    last: Box::new(error),
                }
                .into()),
                failure_reported,
            };
        }

        let Some(mut rr_cache) = rr_cache else {
            return QueryOutcome {
                resolver_succeeded: false,
                result: Err(QueryError::NotFound.into()),
                failure_reported,
            };
        };

        // Adjust TTLs.
        rr_cache.clean(MIN_TTL, self.clean_context(&rr_cache.domain));

        // Save the new entry if the cache is enabled and the record may be cached.
        if !q.no_caching && rr_cache.cacheable() {
            if let Err(err) = self.inner.store.save_rrcache(&rr_cache) {
                tracing::warn!("resolver: failed to cache RR for {}: {err}", query_id(q));
            }
        }

        QueryOutcome {
            resolver_succeeded: true,
            result: Ok(rr_cache),
            failure_reported,
        }
    }

    /// Tests whether resolving a query succeeds and returns the IPs, whether the
    /// query itself succeeded (separate from interpreting the result) and the
    /// error. Uses the given resolver or selects one automatically.
    pub async fn test_connectivity(
        &self,
        fqdn: &str,
        resolver: Option<&Arc<Resolver>>,
        cancel: &CancellationToken,
    ) -> (Vec<IpAddr>, bool, Option<QueryError>) {
        let mut q = Query::new(fqdn, RecordType::A);
        q.no_caching = true;
        if !check_query(&mut q) {
            return (Vec::new(), false, Some(QueryError::Invalid));
        }

        // Resolve with the given resolver or auto-select.
        let result: Result<RRCache, ResolveFailure> = match resolver {
            Some(resolver) => match &resolver.conn {
                Some(conn) => {
                    let reply = tokio::select! {
                        reply = conn.query(q.clone()) => reply,
                        _ = cancel.cancelled() => Err(ResolveError::Cancelled),
                    };
                    match reply {
                        Ok(message) => Ok(RRCache::from_reply(
                            &q.fqdn,
                            q.qtype,
                            message,
                            &resolver.info,
                        )),
                        Err(ResolveError::NotFound) => Err(QueryError::NotFound.into()),
                        Err(ResolveError::BlockedUpstream(_)) => Err(QueryError::BlockedUpstream {
                            resolver_name: resolver.info.descriptive_name(),
                        }
                        .into()),
                        Err(ResolveError::Timeout) => Err(QueryError::Timeout.into()),
                        Err(ResolveError::Cancelled) => Err(QueryError::Cancelled.into()),
                        Err(other) => Err(QueryError::Other(other.to_string()).into()),
                    }
                }
                None => Err(QueryError::Failure.into()),
            },
            None => self.resolve_and_cache(&q, None, cancel).await,
        };

        match result {
            Ok(cache) => match cache.rcode {
                RCODE_NAME_ERROR => (Vec::new(), true, Some(QueryError::NotFound)),
                RCODE_REFUSED => (
                    Vec::new(),
                    true,
                    Some(QueryError::Other("refused".to_string())),
                ),
                _ => {
                    let ips = cache.export_all_a_records();
                    if ips.is_empty() {
                        (Vec::new(), true, Some(QueryError::NotFound))
                    } else {
                        (ips, true, None)
                    }
                }
            },
            Err(failure) => {
                let error = failure.error;
                if error.is_not_found() || error.is_blocked() {
                    (Vec::new(), true, Some(error))
                } else {
                    (Vec::new(), false, Some(error))
                }
            }
        }
    }

    /// The interval at which [`ResolverService::check_failing_resolvers`] should run.
    pub fn failing_resolver_check_interval(&self) -> Duration {
        Duration::from_secs(self.inner.settings.nameserver_retry_rate())
    }

    /// Checks whether failing global resolvers are reachable again. The core
    /// should schedule it every [`ResolverService::failing_resolver_check_interval`].
    pub async fn check_failing_resolvers(
        &self,
        cancel: &CancellationToken,
    ) -> Result<(), QueryError> {
        let resolvers = self.global_resolvers();

        tracing::debug!("resolver: checking failed resolvers");

        // Go through all resolvers and check if they are reachable again.
        for (i, resolver) in resolvers.iter().enumerate() {
            // Skip resolvers that are not failing.
            let Some(conn) = &resolver.conn else { continue };
            if !conn.is_failing() {
                continue;
            }

            tracing::trace!(
                "resolver: testing failed resolver [{}/{}] {resolver}",
                i + 1,
                resolvers.len()
            );

            // Test if the resolver answers. It is failing because it did not
            // answer, so any answer clears it, whatever the answer is.
            let (ips, answered, err) = self
                .test_connectivity(DNS_TEST_DOMAIN, Some(resolver), cancel)
                .await;
            if answered {
                tracing::info!(
                    "resolver: check successful, resolver {resolver} is available again (answer: {ips:?}, {err:?})"
                );
                // Go clears the failure through the resolver wrapper so the
                // "connected to DNS" state is updated as well.
                self.reset_failure(resolver);
            } else {
                tracing::debug!("resolver: failed resolver {resolver} is still failing: {err:?}");
            }

            // Check if the context was cancelled.
            if cancel.is_cancelled() {
                return Err(QueryError::Cancelled);
            }
        }

        Ok(())
    }
}

/// Moves the failing resolvers to the end, so that they are asked only when no
/// other resolver answers. This way, a wrongly marked resolver is used again,
/// and its failing state cleared, by normal queries and not only by the
/// periodic recovery check. The order is otherwise kept.
pub fn order_resolvers(resolvers: Vec<Arc<Resolver>>) -> Vec<Arc<Resolver>> {
    let is_failing = |r: &Arc<Resolver>| r.conn.as_ref().is_some_and(|c| c.is_failing());
    if !resolvers.iter().any(is_failing) {
        return resolvers;
    }

    let (failing, mut ordered): (Vec<_>, Vec<_>) = resolvers.into_iter().partition(is_failing);
    ordered.extend(failing);
    ordered
}
