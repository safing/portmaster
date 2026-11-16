//! Port of Go `nameserver.go` and `response.go`: the request flow.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Instant;

use hickory_proto::op::{Message, OpCode, ResponseCode};
use hickory_proto::rr::{DNSClass, Name, Record};
use pm_network::{DnsRequestContext, SharedConnection, Verdict};
use pm_resolver::resolve::query_id;
use pm_resolver::rr_text::format_name;
use pm_resolver::rrcache::qtype_string;
use pm_resolver::scopes::INTERNAL_SPECIAL_USE_DOMAIN;
use pm_resolver::{Query, QueryError, RRCache};
use tokio_util::sync::CancellationToken;

use crate::env::ServerEnv;
use crate::failing::{FailingQueries, FAILING_DELAY};
use crate::hooks::{
    DnsRequestInfo, DnsResolve, FilterOutcome, FirewallHook, RequestAttributor,
};
use crate::responder::{
    question_text, rcode_to_string, Responder, RrCacheExtras, RrCacheResponder, RrProvider,
    StaticResponder, VerdictExtras, VerdictResponder,
};

const INTERNAL_ERROR: &str = "internal error: ";

/// The injected parts of the nameserver.
pub struct NameserverDeps {
    pub resolver: Arc<dyn DnsResolve>,
    pub attributor: Arc<dyn RequestAttributor>,
    pub hook: Arc<dyn FirewallHook>,
    pub env: Arc<dyn ServerEnv>,
}

/// The request handler of the local DNS server.
pub struct Nameserver {
    deps: NameserverDeps,
    failing: FailingQueries,
}

/// Mutable state of one request that the final bookkeeping needs.
#[derive(Default)]
struct Flow {
    rr_cache: Option<RRCache>,
}

struct CleanOnDrop<'a>(&'a FailingQueries);

impl Drop for CleanOnDrop<'_> {
    fn drop(&mut self) {
        self.0.clean(10, 3, Instant::now());
    }
}

/// Go `InitPublicSuffixData`: whether the domain is in ICANN space.
pub fn icann_space(fqdn: &str) -> bool {
    let domain = fqdn.strip_suffix('.').unwrap_or(fqdn);
    let (suffix, icann) = match psl::suffix(domain.as_bytes()) {
        Some(s) => (
            String::from_utf8_lossy(s.as_bytes()).into_owned(),
            s.typ() == Some(psl::Type::Icann),
        ),
        None => (domain.rsplit('.').next().unwrap_or("").to_string(), false),
    };
    let mut space = icann || suffix.contains('.');
    match suffix.as_str() {
        "example" | "invalid" | "local" | "localhost" | "test" => space = true,
        "onion" => space = false,
        _ => {}
    }
    space
}

/// Formats a unix time like Go's JSON `time.Time` in UTC.
fn rfc3339(unix: i64) -> String {
    let days = unix.div_euclid(86_400);
    let secs = unix.rem_euclid(86_400);
    // Civil date from days since the epoch (Howard Hinnant's algorithm).
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = yoe + era * 400 + i64::from(month <= 2);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
        secs / 3600,
        (secs % 3600) / 60,
        secs % 60
    )
}

/// Go `RRCache.ToDNSRequestContext`.
pub fn to_dns_request_context(rr: &RRCache) -> DnsRequestContext {
    DnsRequestContext {
        domain: rr.domain.clone(),
        question: question_text(rr.question),
        rcode: rcode_to_string(rr.rcode).to_string(),
        served_from_cache: rr.served_from_cache,
        requesting_new: rr.requesting_new,
        is_backup: rr.is_backup,
        filtered: rr.filtered,
        modified: rfc3339(rr.modified),
        expires: rfc3339(rr.expires),
    }
}

/// Go `RRCache.ReplaceAnswerNames`.
fn replace_answer_names(rr: &mut RRCache, fqdn: &Name) {
    for answer in &mut rr.answer {
        if format_name(&answer.name) == rr.domain {
            answer.name = fqdn.clone();
        }
    }
}

/// What a reply is made of: a responder and providers of extra records.
struct Reply<'a> {
    responder: &'a dyn Responder,
    providers: Vec<&'a dyn RrProvider>,
}

/// A reply of the packet level (before the handler), like miekg's server.
fn header_reply(id: u16, opcode: OpCode, rd: bool, cd: bool, rcode: ResponseCode) -> Message {
    let mut reply = Message::response(id, opcode);
    if opcode == OpCode::Query {
        reply.metadata.recursion_desired = rd;
        reply.metadata.checking_disabled = cd;
    }
    reply.metadata.response_code = rcode;
    reply
}

impl Nameserver {
    pub fn new(deps: NameserverDeps) -> Nameserver {
        Nameserver {
            deps,
            failing: FailingQueries::new(),
        }
    }

    pub fn env(&self) -> &Arc<dyn ServerEnv> {
        &self.deps.env
    }

    /// Handles a received datagram like miekg `Server.serveDNS` followed by
    /// the handler, and returns the wire reply, if any.
    pub async fn handle_packet(&self, packet: &[u8], remote: SocketAddr) -> Option<Vec<u8>> {
        // Packets without a full header are ignored: any reply could be used
        // to amplify.
        if packet.len() < 12 {
            return None;
        }
        let id = u16::from_be_bytes([packet[0], packet[1]]);
        let bits = u16::from_be_bytes([packet[2], packet[3]]);
        let counts: Vec<u16> = (0..4)
            .map(|i| u16::from_be_bytes([packet[4 + i * 2], packet[5 + i * 2]]))
            .collect();

        // Responses are ignored.
        if bits & 0x8000 != 0 {
            return None;
        }
        let opcode_num = ((bits >> 11) & 0xF) as u8;
        let rd = bits & 0x0100 != 0;
        let cd = bits & 0x0010 != 0;
        let opcode = OpCode::from_u8(opcode_num);

        // The default accept function of the server library.
        let reject = if opcode_num != 0 && opcode_num != 4 {
            Some(ResponseCode::NotImp)
        } else if counts[0] != 1 || counts[1] > 1 || counts[2] > 1 || counts[3] > 2 {
            Some(ResponseCode::FormErr)
        } else {
            None
        };
        if let Some(rcode) = reject {
            let mut reply = header_reply(id, opcode, rd, cd, ResponseCode::FormErr);
            if rcode == ResponseCode::NotImp {
                reply.metadata.response_code = ResponseCode::NotImp;
            }
            return reply.to_vec().ok();
        }

        let request = match Message::from_vec(packet) {
            Ok(request) => request,
            Err(err) => {
                tracing::debug!("nameserver: failed to unpack message: {err}");
                let reply = header_reply(id, opcode, rd, cd, ResponseCode::FormErr);
                return reply.to_vec().ok();
            }
        };

        let reply = self.handle_request(&request, remote).await?;
        encode_reply(reply)
    }

    /// Handles a parsed request, like Go `handleRequest`. `None` drops the
    /// request.
    pub async fn handle_request(&self, request: &Message, remote: SocketAddr) -> Option<Message> {
        // Check the question; only the first is processed.
        let original_question = match request.queries.len() {
            0 => {
                tracing::warn!("nameserver: received query without question");
                return self
                    .send(request, &StaticResponder::refused(&["no question provided"]), &[])
                    .await;
            }
            1 => request.queries[0].clone(),
            _ => {
                tracing::warn!(
                    "nameserver: received query with multiple questions, first is {}.{}",
                    request.queries[0].name(),
                    request.queries[0].query_type()
                );
                return self
                    .send(
                        request,
                        &StaticResponder::refused(&["multiple question provided"]),
                        &[],
                    )
                    .await;
            }
        };

        // We only serve IN records.
        if original_question.query_class() != DNSClass::IN {
            tracing::warn!(
                "nameserver: received unsupported qclass {} question for {}",
                u16::from(original_question.query_class()),
                original_question.name()
            );
            return self
                .send(request, &StaticResponder::refused(&["unsupported qclass"]), &[])
                .await;
        }

        // Non-standard query names are resolved normalized.
        let original_name = original_question.name().to_ascii();
        let lower_case = original_name.to_lowercase();
        let non_standard_format = lower_case != original_name;
        let mut q = Query::new(lower_case, original_question.query_type());

        tracing::trace!(
            "nameserver: handling new request for {} from {}",
            query_id(&q),
            remote
        );

        // Handle requests for localhost and the hostname.
        if q.fqdn.ends_with("localhost.") || q.fqdn == self.deps.env.hostname() {
            tracing::trace!("nameserver: returning localhost records");
            return self
                .send(request, &StaticResponder::localhost::<&str>(&[]), &[])
                .await;
        }

        // Validate the domain name.
        if !pm_netutils::is_valid_fqdn(&q.fqdn) {
            tracing::debug!("nameserver: domain name {} is invalid, refusing", q.fqdn);
            return self
                .send(request, &StaticResponder::refused(&["invalid domain"]), &[])
                .await;
        }
        let icann = icann_space(&q.fqdn);
        let id = query_id(&q);

        // Some software retries failing queries excessively. Handling a
        // request is expensive, so queries that fail with a high probability
        // are answered from the failing cache for some time.
        let _clean = CleanOnDrop(&self.failing);
        let network_changed = self.deps.env.take_network_changed();
        if let Some(failing) = self.failing.check(&id, network_changed, Instant::now()) {
            let remaining = failing.until.saturating_duration_since(Instant::now());
            tracing::debug!(
                "nameserver: returning previous error for {id}: {}",
                failing.err
            );
            let msg = format!("{INTERNAL_ERROR}{}", failing.err);
            let flood = "delayed failing query to mitigate request flooding";
            // Delay the response a bit in order to mitigate request flooding.
            if remaining < FAILING_DELAY {
                tokio::time::sleep(remaining).await;
                return self
                    .send(request, &StaticResponder::server_failure(&[msg.as_str(), flood]), &[])
                    .await;
            }
            tokio::time::sleep(FAILING_DELAY).await;
            let cached = format!(
                "error is cached for another {}",
                crate::responder::format_go_duration(
                    (remaining.as_secs_f64().round()) as i64
                )
            );
            return self
                .send(
                    request,
                    &StaticResponder::server_failure(&[msg.as_str(), flood, cached.as_str()]),
                    &[],
                )
                .await;
        }

        // Check if the request is local.
        let local = match self.deps.env.is_my_ip(remote.ip()) {
            Ok(local) => local,
            Err(err) => {
                tracing::warn!(
                    "nameserver: failed to check if request for {id} is local: {err}"
                );
                let msg = format!("{INTERNAL_ERROR} failed to check if request is local");
                return self
                    .send(request, &StaticResponder::server_failure(&[&msg]), &[])
                    .await;
            }
        };

        let conn_id = format!(
            "{}-{}-#{}-{}",
            remote.ip().to_canonical(),
            remote.port(),
            request.metadata.id,
            id
        );

        // The connection identifies the process behind the request.
        if !local && !self.deps.env.network_service_mode() {
            tracing::warn!(
                "nameserver: external request from {remote} for {}{}, ignoring",
                q.fqdn,
                qtype_string(q.qtype)
            );
            return self
                .send(
                    request,
                    &StaticResponder::refused(&["external queries are not permitted"]),
                    &[],
                )
                .await;
        }
        let info = DnsRequestInfo {
            fqdn: q.fqdn.clone(),
            conn_id,
            remote,
            local,
        };
        let conn = match self.deps.attributor.attribute(&info).await {
            Ok(conn) => conn,
            Err(err) => {
                tracing::warn!(
                    "nameserver: failed to get host/profile for request for {}{}: {err}",
                    q.fqdn,
                    qtype_string(q.qtype)
                );
                let msg = format!("{INTERNAL_ERROR}failed to get profile");
                return self
                    .send(request, &StaticResponder::server_failure(&[&msg]), &[])
                    .await;
            }
        };

        let mut flow = Flow::default();
        let reply = self
            .process(
                request,
                &conn,
                &mut q,
                icann,
                &mut flow,
                non_standard_format.then_some(original_question.name()),
            )
            .await;
        self.finish(&conn, &q, flow.rr_cache.as_ref());
        reply
    }

    /// The part of the flow that runs with an attributed connection.
    async fn process(
        &self,
        request: &Message,
        conn: &SharedConnection,
        q: &mut Query,
        icann: bool,
        flow: &mut Flow,
        restore_name: Option<&Name>,
    ) -> Option<Message> {
        let id = query_id(q);

        // Check the request with the privacy filter before resolving.
        let outcome = self.deps.hook.filter_connection(conn).await;

        // The firewall may want to respond itself.
        if let Some(responder) = &outcome.responder {
            tracing::info!("nameserver: handing over request for {id} to special filter responder");
            let extras = self.verdict_extras(conn, &outcome);
            return self.send(request, responder.as_ref(), &[&extras]).await;
        }

        // Act upon a verdict.
        if let Some(reply) = self.deny_reply(request, conn, &outcome, None).await {
            return reply;
        }

        // Resolve the request.
        let cancel = CancellationToken::new();
        let rr_cache: Option<RRCache>;
        match self.deps.resolver.resolve(q.clone(), &cancel).await {
            Ok(cache) => rr_cache = Some(cache),
            Err(failure) => {
                let err = failure.error.clone();
                if err.is_not_found() {
                    // Try alternative domain names for unofficial domain spaces.
                    rr_cache = self.check_alternative_caches(q, icann).await;
                    if rr_cache.is_none() {
                        tracing::trace!("nameserver: {err}");
                        conn.lock()
                            .expect("lock poisoned")
                            .failed("domain does not exist", "");
                        let msg = format!("nxdomain: {err}");
                        return self
                            .send(request, &StaticResponder::nx_domain(&[&msg]), &[])
                            .await;
                    }
                } else if err.is_blocked() {
                    tracing::trace!("nameserver: {err}");
                    conn.lock()
                        .expect("lock poisoned")
                        .block(&err.to_string(), "");
                    let msg = format!("blocked: {err}");
                    return self
                        .send(request, &StaticResponder::block_ip(&[&msg]), &[])
                        .await;
                } else if err == QueryError::Localhost {
                    tracing::trace!("nameserver: returning localhost records");
                    conn.lock()
                        .expect("lock poisoned")
                        .accept("allowing query for localhost", "");
                    return self
                        .send(request, &StaticResponder::localhost::<&str>(&[]), &[])
                        .await;
                } else if err == QueryError::Offline {
                    match failure.stale {
                        None => {
                            tracing::debug!(
                                "nameserver: not resolving {id}, device is offline"
                            );
                            conn.lock()
                                .expect("lock poisoned")
                                .failed("not resolving, device is offline", "");
                            return self
                                .send(
                                    request,
                                    &StaticResponder::server_failure(&[err.to_string()]),
                                    &[],
                                )
                                .await;
                        }
                        Some(stale) => {
                            // The expired entry is usable as a backup.
                            let mut stale = *stale;
                            stale.is_backup = true;
                            tracing::debug!(
                                "nameserver: device is offline, using backup cache for {id}"
                            );
                            rr_cache = Some(stale);
                        }
                    }
                } else {
                    tracing::warn!("nameserver: failed to resolve {id}: {err}");
                    conn.lock()
                        .expect("lock poisoned")
                        .failed(&format!("query failed: {err}"), "");
                    self.failing.add(&id, &err.to_string(), Instant::now());
                    let msg = format!("{INTERNAL_ERROR}{err}");
                    return self
                        .send(request, &StaticResponder::server_failure(&[&msg]), &[])
                        .await;
                }
            }
        }

        // Handle special cases.
        flow.rr_cache = rr_cache.clone();
        let Some(mut cache) = rr_cache else {
            tracing::warn!("nameserver: received successful, but empty reply from resolver");
            self.failing
                .add(&id, "emptry reply from resolver", Instant::now());
            let msg = format!("{INTERNAL_ERROR}empty reply");
            return self
                .send(request, &StaticResponder::server_failure(&[&msg]), &[])
                .await;
        };
        if cache.rcode == 3 {
            // Try alternative domain names for unofficial domain spaces.
            match self.check_alternative_caches(q, icann).await {
                Some(alt) => {
                    cache = alt;
                    flow.rr_cache = Some(cache.clone());
                }
                None => {
                    return self
                        .send(
                            request,
                            &StaticResponder::nx_domain(&["no answer found (NXDomain)"]),
                            &[],
                        )
                        .await;
                }
            }
        }

        // Check with the firewall again after resolving.
        tracing::trace!("nameserver: deciding on resolved dns");
        let (filtered, outcome) = self.deps.hook.filter_resolved_dns(conn, q, cache).await;
        flow.rr_cache = filtered.clone();

        if let Some(responder) = &outcome.responder {
            tracing::info!("nameserver: handing over request for {id} to special filter responder");
            return self.send(request, responder.as_ref(), &[]).await;
        }

        if let Some(reply) = self
            .deny_reply(request, conn, &outcome, filtered.as_ref())
            .await
        {
            return reply;
        }

        let Some(mut cache) = filtered else {
            tracing::warn!("nameserver: firewall returned no answer for {id}");
            let msg = format!("{INTERNAL_ERROR}empty reply");
            return self
                .send(request, &StaticResponder::server_failure(&[&msg]), &[])
                .await;
        };

        // Revert to the non-standard question format, if we had to convert.
        if let Some(name) = restore_name {
            replace_answer_names(&mut cache, name);
            flow.rr_cache = Some(cache.clone());
        }

        let (verdict, _) = Self::snapshot(conn);
        tracing::info!(
            "nameserver: returning {} response ({}{}) for {id}",
            verdict.verb(),
            rcode_to_string(cache.rcode),
            if cache.answer.is_empty() { "/no answer" } else { "" },
        );
        let extras = self.verdict_extras(conn, &outcome);
        let cache_extras = RrCacheExtras(&cache);
        self.send(
            request,
            &RrCacheResponder(&cache),
            &[&extras, &cache_extras],
        )
        .await
    }

    fn snapshot(conn: &SharedConnection) -> (Verdict, String) {
        let conn = conn.lock().expect("lock poisoned");
        (conn.verdict, conn.reason.msg.clone())
    }

    fn verdict_extras(&self, conn: &SharedConnection, outcome: &FilterOutcome) -> VerdictExtras {
        let (verdict, reason) = Self::snapshot(conn);
        VerdictExtras {
            verdict,
            reason,
            reason_rrs: outcome.extra_rrs.clone(),
        }
    }

    /// If the verdict denies the request, builds the reply. The outer option
    /// tells whether the request was denied, the inner one whether a reply is sent.
    async fn deny_reply(
        &self,
        request: &Message,
        conn: &SharedConnection,
        outcome: &FilterOutcome,
        cache: Option<&RRCache>,
    ) -> Option<Option<Message>> {
        let (verdict, reason) = Self::snapshot(conn);
        if !matches!(verdict, Verdict::Block | Verdict::Drop | Verdict::Failed) {
            return None;
        }
        tracing::info!(
            "nameserver: returning {} response: {reason}",
            verdict.verb()
        );
        let extras = self.verdict_extras(conn, outcome);
        let cache_extras = cache.map(RrCacheExtras);
        let mut providers: Vec<&dyn RrProvider> = vec![&extras];
        if let Some(cache_extras) = &cache_extras {
            providers.push(cache_extras);
        }
        Some(
            self.send(request, &VerdictResponder { verdict }, &providers)
                .await,
        )
    }

    /// Go `sendResponse` without the writing.
    async fn send(
        &self,
        request: &Message,
        responder: &dyn Responder,
        providers: &[&dyn RrProvider],
    ) -> Option<Message> {
        let reply = Reply {
            responder,
            providers: providers.to_vec(),
        };
        let mut message = reply.responder.reply(request).await?;
        // We always forward to a recursive resolver.
        message.metadata.recursion_available = true;
        for provider in reply.providers {
            message.additionals.extend(provider.extra_rrs(request));
        }
        Some(message)
    }

    /// The deferred bookkeeping of Go `handleRequest`.
    fn finish(&self, conn: &SharedConnection, q: &Query, rr_cache: Option<&RRCache>) {
        let scope = match rr_cache {
            Some(_) => None,
            None => self.deps.resolver.resolver_ip_scope(q),
        };
        let verdict = {
            let mut guard = conn.lock().expect("lock poisoned");
            if let Some(rr) = rr_cache {
                guard.dns_context = Some(to_dns_request_context(rr));
                guard.resolver = rr.resolver.clone();
                if let (Some(entity), Some(resolver)) = (guard.entity.as_mut(), &rr.resolver) {
                    entity.ip_scope = resolver.ip_scope;
                }
            } else if let (Some(entity), Some(scope)) = (guard.entity.as_mut(), scope) {
                entity.ip_scope = scope;
            }
            guard.verdict
        };

        match verdict {
            // Blocked, dropped or failed requests are saved right away so
            // they show up in the UI.
            Verdict::Block
            | Verdict::Drop
            | Verdict::Failed
            | Verdict::RerouteToNameserver
            | Verdict::RerouteToTunnel
            | Verdict::RerouteToSplitTun => self.deps.hook.save_connection(conn),

            // Undecided or accepted requests are saved later, with the
            // connection that follows.
            Verdict::Undecided | Verdict::Accept => {
                let Some(rr) = rr_cache else {
                    conn.lock()
                        .expect("lock poisoned")
                        .failed(&format!("{INTERNAL_ERROR}no reply"), "");
                    return;
                };

                // Mark successful queries as internal to hide them in the
                // simple interface: they were most probably made for another
                // process.
                {
                    let mut guard = conn.lock().expect("lock poisoned");
                    if self.deps.attributor.is_system_resolver(&guard) {
                        guard.internal = true;
                    }
                }

                // Save the request as open, as we don't know if there will be
                // a connection or not.
                self.deps.hook.update_ips_and_cnames(q, rr, conn);
                self.deps.hook.save_open_dns_request(q, rr, conn);
            }

            Verdict::Undeterminable => {
                tracing::warn!(
                    "nameserver: unexpected verdict {} for connection, not saving",
                    verdict.verb()
                );
            }
        }
    }

    /// Go `checkAlternativeCaches`.
    async fn check_alternative_caches(&self, q: &Query, icann: bool) -> Option<RRCache> {
        // Do not try alternatives when the query is in a public suffix. This
        // also includes arpa. and local.
        if icann {
            return None;
        }

        // Check if the env resolver has something.
        let env_q = Query::new(
            format!("{}local.{}", q.fqdn, INTERNAL_SPECIAL_USE_DOMAIN),
            q.qtype,
        );
        if let Some(mut cache) = self.deps.resolver.query_portmaster_env(&env_q).await {
            if cache.rcode == 0 {
                make_alternative_record(q, &mut cache, &env_q.fqdn);
                return Some(cache);
            }
        }

        // Check if we have anything in the cache.
        let local_fqdn = format!("{}local.", q.fqdn);
        if let Some(mut cache) = self.deps.resolver.get_rr_cache(&local_fqdn, q.qtype) {
            if cache.rcode == 0 {
                make_alternative_record(q, &mut cache, &local_fqdn);
                return Some(cache);
            }
        }
        None
    }
}

/// Go `makeAlternativeRecord`.
fn make_alternative_record(q: &Query, cache: &mut RRCache, alt_name: &str) {
    tracing::debug!("using {alt_name} to answer query");

    let Ok(name) = Name::from_ascii(&q.fqdn) else {
        return;
    };
    // Duplicate the answers so they match the query.
    let copied: Vec<Record> = cache
        .answer
        .iter()
        .filter(|answer| format_name(&answer.name).to_lowercase() == alt_name)
        .map(|answer| {
            let mut copy = answer.clone();
            copy.name = name.clone();
            copy
        })
        .collect();
    cache.answer.extend(copied);
    cache.domain = q.fqdn.clone();
}

/// Writes the reply to the wire. Like Go, the extra section is dropped if the
/// message does not fit.
pub fn encode_reply(mut reply: Message) -> Option<Vec<u8>> {
    const MAX_UDP: usize = 65_507;
    match reply.to_vec() {
        Ok(wire) if wire.len() <= MAX_UDP => return Some(wire),
        Ok(_) | Err(_) => {}
    }
    tracing::trace!("nameserver: retrying to write dns message without extra section");
    reply.additionals.clear();
    match reply.to_vec() {
        Ok(wire) if wire.len() <= MAX_UDP => Some(wire),
        _ => {
            tracing::warn!("nameserver: failed to encode reply");
            None
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rfc3339_formats_like_go() {
        assert_eq!(rfc3339(0), "1970-01-01T00:00:00Z");
        assert_eq!(rfc3339(1_700_000_000), "2023-11-14T22:13:20Z");
        assert_eq!(rfc3339(951_782_400), "2000-02-29T00:00:00Z");
    }

    #[test]
    fn icann_space_matches_go_rules() {
        assert!(icann_space("example.com."));
        assert!(icann_space("foo.example."));
        assert!(icann_space("host.local."));
        assert!(icann_space("a.test."));
        assert!(!icann_space("a.onion."));
        assert!(!icann_space("printer.lan."));
        assert!(icann_space("a.b.co.uk."));
    }

    #[test]
    fn message_type_is_response() {
        use hickory_proto::op::MessageType;
        let m = header_reply(1, OpCode::Query, true, false, ResponseCode::FormErr);
        assert_eq!(m.metadata.message_type, MessageType::Response);
        assert!(m.metadata.recursion_desired);
    }
}
