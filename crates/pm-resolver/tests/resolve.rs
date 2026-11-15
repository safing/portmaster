//! Tests of the resolver orchestration with fake resolver connections.

use std::collections::HashMap;
use std::net::Ipv4Addr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use hickory_proto::op::{Message, MessageType, OpCode, ResponseCode};
use hickory_proto::rr::rdata::A;
use hickory_proto::rr::{Name, RData, Record, RecordType};
use pm_database::{Databases, MemoryStorage};
use pm_netenv::OnlineStatus;
use pm_resolver::namerecord::{record_interface_options, NameRecordStore};
use pm_resolver::rrcache::now_unix;
use pm_resolver::{
    BasicResolverConn, Query, QueryError, ResolveError, ResolverConn, ResolverService,
    StaticEnvironment, StaticSettings,
};
use pm_resolver::{Nameserver, RRCache};
use tokio_util::sync::CancellationToken;

#[derive(Clone)]
enum Behavior {
    Answer(Ipv4Addr),
    Rcode(ResponseCode),
    Blocked,
    Timeout,
    Fail,
    NotFound,
    Slow(Duration, Ipv4Addr),
}

struct FakeConn {
    basic: BasicResolverConn,
    calls: AtomicUsize,
    behavior: Mutex<Behavior>,
}

impl FakeConn {
    fn set(&self, behavior: Behavior) {
        *self.behavior.lock().unwrap() = behavior;
    }

    fn calls(&self) -> usize {
        self.calls.load(Ordering::SeqCst)
    }
}

fn reply(q: &Query, rcode: ResponseCode, ip: Option<Ipv4Addr>) -> Message {
    let mut msg = Message::new(1, MessageType::Response, OpCode::Query);
    msg.metadata.response_code = rcode;
    if let Some(ip) = ip {
        msg.add_answer(Record::from_rdata(
            Name::from_ascii(&q.fqdn).unwrap(),
            300,
            RData::A(A(ip)),
        ));
    }
    msg
}

#[async_trait::async_trait]
impl ResolverConn for FakeConn {
    async fn query(&self, q: Query) -> Result<Message, ResolveError> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        let behavior = self.behavior.lock().unwrap().clone();
        match behavior {
            Behavior::Answer(ip) => Ok(reply(&q, ResponseCode::NoError, Some(ip))),
            Behavior::Rcode(rcode) => Ok(reply(&q, rcode, None)),
            Behavior::Blocked => Err(ResolveError::BlockedUpstream("fake".into())),
            Behavior::Timeout => Err(ResolveError::Timeout),
            Behavior::Fail => Err(ResolveError::Failure("boom".into())),
            Behavior::NotFound => Err(ResolveError::NotFound),
            Behavior::Slow(delay, ip) => {
                tokio::time::sleep(delay).await;
                Ok(reply(&q, ResponseCode::NoError, Some(ip)))
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

    async fn force_reconnect(&self) {}
}

struct Harness {
    service: ResolverService,
    settings: Arc<StaticSettings>,
    env: Arc<StaticEnvironment>,
    conns: Arc<Mutex<HashMap<String, Arc<FakeConn>>>>,
}

impl Harness {
    fn new(nameservers: &[&str]) -> Harness {
        let databases = Arc::new(Databases::without_disk());
        databases
            .register_injected("cache", "Cache database")
            .unwrap();
        databases
            .inject("cache", Box::new(MemoryStorage::new()))
            .unwrap();
        let store = NameRecordStore::new(databases.interface(record_interface_options()));

        let settings = Arc::new(StaticSettings::default());
        settings.update(|s| s.nameservers = nameservers.iter().map(|n| (*n).to_string()).collect());
        let env = Arc::new(StaticEnvironment::default());

        let conns: Arc<Mutex<HashMap<String, Arc<FakeConn>>>> = Arc::default();
        let factory_conns = Arc::clone(&conns);
        let service = ResolverService::with_factory(
            settings.clone(),
            env.clone(),
            store,
            Arc::new(move |resolver| {
                let conn = Arc::new(FakeConn {
                    basic: BasicResolverConn::new(),
                    calls: AtomicUsize::new(0),
                    behavior: Mutex::new(Behavior::Answer(Ipv4Addr::new(192, 0, 2, 99))),
                });
                factory_conns
                    .lock()
                    .unwrap()
                    .insert(resolver.config_url.clone(), Arc::clone(&conn));
                Ok(conn as Arc<dyn ResolverConn>)
            }),
        );
        service.load_resolvers();
        Harness {
            service,
            settings,
            env,
            conns,
        }
    }

    fn conn(&self, config_url: &str) -> Arc<FakeConn> {
        Arc::clone(
            self.conns
                .lock()
                .unwrap()
                .get(config_url)
                .unwrap_or_else(|| panic!("no conn for {config_url}")),
        )
    }

    fn total_calls(&self) -> usize {
        self.conns.lock().unwrap().values().map(|c| c.calls()).sum()
    }

    /// Saves a cache entry resolved by the given resolver.
    fn seed(&self, domain: &str, resolver_url: &str, expires_in: i64, ip: Ipv4Addr) {
        let resolver = self
            .service
            .global_resolvers()
            .into_iter()
            .find(|r| r.config_url == resolver_url)
            .unwrap();
        let q = Query::new(domain, RecordType::A);
        let mut cache = RRCache::from_reply(
            &q.fqdn,
            RecordType::A,
            reply(&q, ResponseCode::NoError, Some(ip)),
            &resolver.info,
        );
        cache.expires = now_unix() + expires_in;
        self.service.store().save_rrcache(&cache).unwrap();
    }
}

const A_URL: &str = "dns://192.0.2.1:53?name=a";
const B_URL: &str = "dns://192.0.2.2:53?name=b";

fn q(domain: &str) -> Query {
    Query::new(domain, RecordType::A)
}

fn nocache(domain: &str) -> Query {
    let mut query = q(domain);
    query.no_caching = true;
    query
}

fn first_ip(cache: &RRCache) -> Ipv4Addr {
    match &cache.answer[0].data {
        RData::A(a) => a.0,
        other => panic!("unexpected {other:?}"),
    }
}

#[tokio::test]
async fn success_is_cleaned_cached_and_served_from_cache() {
    let h = Harness::new(&[A_URL]);

    let first = h.service.resolve(q("acme.org")).await.unwrap();
    assert_eq!(first.domain, "acme.org.");
    assert_eq!(first_ip(&first), Ipv4Addr::new(192, 0, 2, 99));
    assert_eq!(first.answer[0].ttl, 17);
    assert!(!first.served_from_cache);
    // Online, record TTL 300.
    let ttl = first.expires - now_unix();
    assert!((299..=300).contains(&ttl), "{ttl}");
    assert_eq!(first.resolver.as_ref().unwrap().name, "a");

    let second = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(second.served_from_cache);
    assert_eq!(h.total_calls(), 1);
}

#[tokio::test]
async fn nxdomain_is_a_cacheable_answer_with_short_ttl() {
    let h = Harness::new(&[A_URL]);
    h.conn(A_URL).set(Behavior::Rcode(ResponseCode::NXDomain));

    let first = h.service.resolve(q("nothing.acme.org")).await.unwrap();
    assert_eq!(first.rcode, 3);
    let ttl = first.expires - now_unix();
    assert!((9..=10).contains(&ttl), "{ttl}");

    let second = h.service.resolve(q("nothing.acme.org")).await.unwrap();
    assert!(second.served_from_cache);
    assert_eq!(h.total_calls(), 1);
}

#[tokio::test]
async fn servfail_is_not_cached() {
    let h = Harness::new(&[A_URL]);
    h.conn(A_URL).set(Behavior::Rcode(ResponseCode::ServFail));

    let first = h.service.resolve(q("broken.acme.org")).await.unwrap();
    assert_eq!(first.rcode, 2);
    h.service.resolve(q("broken.acme.org")).await.unwrap();
    assert_eq!(h.total_calls(), 2);
}

#[tokio::test]
async fn blocked_upstream_is_returned_without_failure_report_or_caching() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.conn(A_URL).set(Behavior::Blocked);

    for _ in 0..8 {
        let err = h
            .service
            .resolve(nocache("ads.acme.org"))
            .await
            .unwrap_err();
        assert!(err.error.is_blocked(), "{err}");
        assert!(matches!(err.error, QueryError::BlockedUpstream { .. }));
    }
    // No failure was counted and the second resolver was never asked.
    assert!(!h.conn(A_URL).is_failing());
    assert_eq!(h.conn(B_URL).calls(), 0);

    let err = h.service.resolve(q("ads.acme.org")).await.unwrap_err();
    assert!(err
        .error
        .to_string()
        .starts_with("query was blocked by upstream DNS resolver a ("));
    h.conn(A_URL)
        .set(Behavior::Answer(Ipv4Addr::new(192, 0, 2, 5)));
    // Nothing was cached for the blocked query.
    let cache = h.service.resolve(q("ads.acme.org")).await.unwrap();
    assert!(!cache.served_from_cache);
}

#[tokio::test]
async fn timeout_fails_over_and_marks_resolver_failing() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.conn(A_URL).set(Behavior::Timeout);

    for i in 0..8 {
        let cache = h
            .service
            .resolve(nocache(&format!("host{i}.acme.org")))
            .await
            .unwrap();
        assert_eq!(cache.resolver.as_ref().unwrap().name, "b");
    }
    assert!(h.conn(A_URL).is_failing());
    assert!(!h.conn(B_URL).is_failing());
    assert!(!h.service.all_configured_resolvers_are_failing());

    // A failing resolver is asked last, so it is not asked at all now.
    let a_calls = h.conn(A_URL).calls();
    h.service
        .resolve(nocache("another.acme.org"))
        .await
        .unwrap();
    assert_eq!(h.conn(A_URL).calls(), a_calls);
}

#[tokio::test]
async fn failing_resolver_is_still_asked_when_nothing_else_answers() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.conn(A_URL).set(Behavior::Timeout);
    h.conn(B_URL).set(Behavior::Timeout);

    for i in 0..8 {
        let err = h
            .service
            .resolve(nocache(&format!("host{i}.acme.org")))
            .await
            .unwrap_err();
        assert!(matches!(
            &err.error,
            QueryError::AllFailed { count: 2, last } if **last == QueryError::Timeout
        ));
    }
    assert!(h.conn(A_URL).is_failing() && h.conn(B_URL).is_failing());
    assert!(h.service.all_configured_resolvers_are_failing());
    assert_eq!(
        h.env.read(|s| s.failing_notifications.last().copied()),
        Some(true)
    );

    // A recovered failing resolver answers and clears its state.
    h.conn(A_URL)
        .set(Behavior::Answer(Ipv4Addr::new(192, 0, 2, 7)));
    let cache = h
        .service
        .resolve(nocache("recovered.acme.org"))
        .await
        .unwrap();
    assert_eq!(cache.resolver.as_ref().unwrap().name, "a");
    assert!(!h.conn(A_URL).is_failing());
    assert!(!h.service.all_configured_resolvers_are_failing());
    assert_eq!(h.env.read(|s| s.connected_to_dns), Some(true));
    assert_eq!(
        h.env.read(|s| s.failing_notifications.last().copied()),
        Some(false)
    );
}

#[tokio::test]
async fn failures_are_not_reported_while_offline() {
    let h = Harness::new(&[A_URL]);
    h.conn(A_URL).set(Behavior::Fail);
    // Limited is not offline, but also not online.
    h.env.update(|s| s.online_status = OnlineStatus::Limited);

    for i in 0..10 {
        let err = h
            .service
            .resolve(nocache(&format!("host{i}.acme.org")))
            .await
            .unwrap_err();
        assert!(matches!(err.error, QueryError::AllFailed { .. }));
    }
    assert!(!h.conn(A_URL).is_failing());
}

#[tokio::test]
async fn offline_returns_error_and_old_cache_except_for_connectivity_domains() {
    let h = Harness::new(&[A_URL]);
    h.seed("acme.org", A_URL, -10, Ipv4Addr::new(192, 0, 2, 50));
    h.env.update(|s| {
        s.online_status = OnlineStatus::Offline;
        s.connectivity_domains = vec!["neverssl.com.".to_string()];
    });

    let err = h.service.resolve(q("acme.org")).await.unwrap_err();
    assert_eq!(err.error, QueryError::Offline);
    assert_eq!(
        first_ip(err.stale.as_ref().unwrap()),
        Ipv4Addr::new(192, 0, 2, 50)
    );
    assert_eq!(h.total_calls(), 0);

    // The online check domain and connectivity domains still resolve.
    h.service
        .resolve(nocache("online-check.safing.io."))
        .await
        .unwrap();
    h.service.resolve(nocache("neverssl.com.")).await.unwrap();
    assert_eq!(h.total_calls(), 2);

    // A transport error while offline is reported as offline.
    h.conn(A_URL).set(Behavior::Fail);
    let err = h
        .service
        .resolve(nocache("other.acme.org"))
        .await
        .unwrap_err();
    assert_eq!(err.error, QueryError::Offline);
}

#[tokio::test]
async fn expired_cache_is_served_as_backup_when_all_resolvers_fail() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.seed("acme.org", A_URL, -10, Ipv4Addr::new(192, 0, 2, 50));
    h.conn(A_URL).set(Behavior::Timeout);
    h.conn(B_URL).set(Behavior::Fail);

    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(cache.is_backup);
    assert!(cache.served_from_cache);
    assert_eq!(first_ip(&cache), Ipv4Addr::new(192, 0, 2, 50));
    assert_eq!(cache.flags(), " [CB]");
}

#[tokio::test]
async fn expired_cache_is_replaced_by_a_fresh_answer() {
    let h = Harness::new(&[A_URL]);
    h.seed("acme.org", A_URL, -10, Ipv4Addr::new(192, 0, 2, 50));

    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(!cache.served_from_cache);
    assert_eq!(first_ip(&cache), Ipv4Addr::new(192, 0, 2, 99));
}

#[tokio::test]
async fn stale_cache_is_served_while_refreshing_in_background() {
    let h = Harness::new(&[A_URL]);
    h.settings.update(|s| s.use_stale_cache = true);
    h.seed("acme.org", A_URL, -10, Ipv4Addr::new(192, 0, 2, 50));

    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(cache.served_from_cache);
    assert!(cache.requesting_new);
    assert_eq!(first_ip(&cache), Ipv4Addr::new(192, 0, 2, 50));

    // The refresh updates the cache.
    for _ in 0..100 {
        if h.total_calls() == 1 {
            let (record, _) = h.service.store().get("acme.org.", "A").unwrap();
            if record.expires > now_unix() {
                return;
            }
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    panic!("background refresh did not happen");
}

#[tokio::test]
async fn expiring_soon_entries_are_refreshed_in_background() {
    let h = Harness::new(&[A_URL]);
    h.seed("acme.org", A_URL, 10, Ipv4Addr::new(192, 0, 2, 50));

    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(cache.requesting_new);
    assert_eq!(first_ip(&cache), Ipv4Addr::new(192, 0, 2, 50));
    for _ in 0..100 {
        if h.total_calls() == 1 {
            return;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    panic!("background refresh did not happen");
}

#[tokio::test]
async fn cache_of_removed_or_noncompliant_resolver_is_ignored() {
    let h = Harness::new(&[A_URL]);
    h.seed("acme.org", A_URL, 600, Ipv4Addr::new(192, 0, 2, 50));

    // Non-compliant: plain DNS is forbidden now.
    h.settings.update(|s| s.no_insecure_protocols = true);
    let err = h.service.resolve(q("acme.org")).await.unwrap_err();
    assert_eq!(err.error, QueryError::NoCompliance);
    assert!(err.error.is_blocked());
    h.settings.update(|s| s.no_insecure_protocols = false);

    // The source server was removed.
    h.settings
        .update(|s| s.nameservers = vec![B_URL.to_string()]);
    h.service.load_resolvers();
    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(!cache.served_from_cache);
}

#[tokio::test]
async fn connectivity_domains_bypass_the_cache() {
    let h = Harness::new(&[A_URL]);
    h.env
        .update(|s| s.connectivity_domains = vec!["neverssl.com.".to_string()]);
    h.settings
        .update(|s| s.nameservers = vec![A_URL.to_string()]);

    h.service.resolve(q("neverssl.com")).await.unwrap();
    let second = h.service.resolve(q("neverssl.com")).await.unwrap();
    assert!(!second.served_from_cache);
    // Connectivity answers are cached for 3 seconds only.
    let ttl = second.expires - now_unix();
    assert!((2..=3).contains(&ttl), "{ttl}");
}

#[tokio::test]
async fn cache_entry_is_reset_when_the_same_query_repeats() {
    let h = Harness::new(&[A_URL]);
    h.service.resolve(q("acme.org")).await.unwrap(); // miss
    for _ in 0..2 {
        assert!(
            h.service
                .resolve(q("acme.org"))
                .await
                .unwrap()
                .served_from_cache
        );
    }
    // The third cache hit in a row resets the entry.
    let cache = h.service.resolve(q("acme.org")).await.unwrap();
    assert!(!cache.served_from_cache);
    assert_eq!(h.total_calls(), 2);

    // Follow-up resets need ten more hits in a row.
    for _ in 0..9 {
        assert!(
            h.service
                .resolve(q("acme.org"))
                .await
                .unwrap()
                .served_from_cache
        );
    }
    assert!(
        !h.service
            .resolve(q("acme.org"))
            .await
            .unwrap()
            .served_from_cache
    );
    assert_eq!(h.total_calls(), 3);
}

#[tokio::test]
async fn concurrent_identical_queries_are_deduplicated() {
    let h = Harness::new(&[A_URL]);
    h.conn(A_URL).set(Behavior::Slow(
        Duration::from_millis(150),
        Ipv4Addr::new(192, 0, 2, 9),
    ));

    let (a, b, c) = tokio::join!(
        h.service.resolve(q("acme.org")),
        h.service.resolve(q("acme.org")),
        h.service.resolve(q("acme.org")),
    );
    for result in [a, b, c] {
        assert_eq!(first_ip(&result.unwrap()), Ipv4Addr::new(192, 0, 2, 9));
    }
    assert_eq!(h.total_calls(), 1);

    // Different queries are not deduplicated.
    let (_, _) = tokio::join!(
        h.service.resolve(nocache("one.acme.org")),
        h.service.resolve(nocache("two.acme.org")),
    );
    assert_eq!(h.total_calls(), 3);
}

#[tokio::test]
async fn not_found_ends_the_query_unless_all_resolvers_are_tried() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.conn(A_URL).set(Behavior::NotFound);
    let err = h.service.resolve(q("acme.org")).await.unwrap_err();
    // Global scope: the first answer counts.
    assert!(err.error.is_not_found());
    assert_eq!(err.error.to_string(), "record could not be found");
    assert_eq!(h.conn(B_URL).calls(), 0);
}

#[tokio::test]
async fn special_scope_tries_all_local_resolvers() {
    let h = Harness::new(&[A_URL]);
    let l1 = "dns://192.168.1.1:53";
    let l2 = "dns://192.168.1.2:53";
    h.settings
        .update(|s| s.nameservers = vec![A_URL.into(), l1.into(), l2.into()]);
    h.service.load_resolvers();

    h.conn(l1).set(Behavior::NotFound);
    h.conn(l2).set(Behavior::Rcode(ResponseCode::NXDomain));
    // NXDOMAIN and "not found" continue; the last answer is returned.
    let cache = h.service.resolve(q("printer.home.arpa")).await.unwrap();
    assert_eq!(cache.rcode, 3);
    assert_eq!(h.conn(A_URL).calls(), 0);

    // The first positive answer wins.
    h.conn(l1)
        .set(Behavior::Answer(Ipv4Addr::new(192, 168, 1, 50)));
    let cache = h
        .service
        .resolve(nocache("printer.home.arpa"))
        .await
        .unwrap();
    assert_eq!(first_ip(&cache), Ipv4Addr::new(192, 168, 1, 50));

    // Only "not found" from all: the last error.
    h.conn(l1).set(Behavior::NotFound);
    h.conn(l2).set(Behavior::NotFound);
    let err = h
        .service
        .resolve(nocache("gone.home.arpa"))
        .await
        .unwrap_err();
    assert!(err.error.is_not_found());
    assert_eq!(
        err.error.to_string(),
        "all 2 query-compliant resolvers failed, last error: record could not be found"
    );
}

#[tokio::test]
async fn special_domains_follow_rfc6761_and_settings() {
    let h = Harness::new(&[A_URL]);

    let err = h.service.resolve(q("foo.localhost")).await.unwrap_err();
    assert_eq!(err.error, QueryError::Localhost);
    let err = h
        .service
        .resolve(Query::new("foo.localhost", RecordType::MX))
        .await
        .unwrap_err();
    assert!(err.error.is_not_found());
    let err = h.service.resolve(q("foo.invalid")).await.unwrap_err();
    assert!(err.error.is_not_found());

    let err = h.service.resolve(q("hidden.onion")).await.unwrap_err();
    assert_eq!(err.error, QueryError::SpecialDomainsDisabled);
    assert!(err.error.is_blocked());
    assert_eq!(
        err.error.to_string(),
        "query was blocked: special domains disabled"
    );

    let mut empty = q("x");
    empty.fqdn = String::new();
    let err = h.service.resolve(empty).await.unwrap_err();
    assert_eq!(err.error, QueryError::Invalid);
    assert!(err.error.is_not_found());

    // The environment resolver is not ported: it has no answer.
    let err = h
        .service
        .resolve(q("router.local.portmaster.home.arpa"))
        .await
        .unwrap_err();
    assert!(err.error.is_not_found());
}

#[tokio::test]
async fn no_compliant_resolver_is_blocked() {
    let h = Harness::new(&[A_URL]);
    h.settings.update(|s| s.no_insecure_protocols = true);
    let err = h.service.resolve(q("acme.org")).await.unwrap_err();
    assert_eq!(err.error, QueryError::NoCompliance);
    assert_eq!(
        err.error.to_string(),
        "query was blocked: no compliant resolvers for this query"
    );
}

#[tokio::test]
async fn cancelled_queries_do_not_count_as_failures() {
    let h = Harness::new(&[A_URL]);
    h.conn(A_URL).set(Behavior::Slow(
        Duration::from_secs(30),
        Ipv4Addr::new(192, 0, 2, 9),
    ));

    let cancel = CancellationToken::new();
    let canceller = cancel.clone();
    tokio::spawn(async move {
        tokio::time::sleep(Duration::from_millis(50)).await;
        canceller.cancel();
    });
    let err = h
        .service
        .resolve_with_cancel(nocache("slow.acme.org"), &cancel)
        .await
        .unwrap_err();
    assert_eq!(err.error, QueryError::Cancelled);
    assert!(!h.conn(A_URL).is_failing());

    h.service.shutdown();
    let err = h.service.resolve(nocache("x.acme.org")).await.unwrap_err();
    assert_eq!(err.error, QueryError::ShuttingDown);
}

#[tokio::test]
async fn failing_resolver_recheck_clears_resolvers_that_answer() {
    let h = Harness::new(&[A_URL, B_URL]);
    h.conn(A_URL).set(Behavior::Timeout);
    h.conn(B_URL).set(Behavior::Timeout);
    for i in 0..8 {
        let _ = h
            .service
            .resolve(nocache(&format!("host{i}.acme.org")))
            .await;
    }
    assert!(h.conn(A_URL).is_failing() && h.conn(B_URL).is_failing());

    // A answers (even with NXDOMAIN, it is reachable), B is still down.
    h.conn(A_URL).set(Behavior::Rcode(ResponseCode::NXDomain));
    h.service
        .check_failing_resolvers(&CancellationToken::new())
        .await
        .unwrap();
    assert!(!h.conn(A_URL).is_failing());
    assert!(h.conn(B_URL).is_failing());
    assert_eq!(
        h.service.failing_resolver_check_interval(),
        Duration::from_secs(300)
    );
}

#[tokio::test]
async fn test_connectivity_reports_whether_the_query_itself_succeeded() {
    let h = Harness::new(&[A_URL]);
    let cancel = CancellationToken::new();

    let (ips, ok, err) = h.service.test_connectivity("acme.org", None, &cancel).await;
    assert!(ok && err.is_none());
    assert_eq!(ips, vec![Ipv4Addr::new(192, 0, 2, 99)]);

    h.conn(A_URL).set(Behavior::Rcode(ResponseCode::NXDomain));
    let (_, ok, err) = h.service.test_connectivity("acme.org", None, &cancel).await;
    assert!(ok);
    assert!(err.unwrap().is_not_found());

    h.conn(A_URL).set(Behavior::Rcode(ResponseCode::Refused));
    let (_, ok, err) = h.service.test_connectivity("acme.org", None, &cancel).await;
    assert!(ok);
    assert_eq!(err, Some(QueryError::Other("refused".to_string())));

    h.conn(A_URL).set(Behavior::Timeout);
    let (_, ok, err) = h.service.test_connectivity("acme.org", None, &cancel).await;
    assert!(!ok);
    assert!(err.is_some());

    h.conn(A_URL).set(Behavior::Blocked);
    let resolver = h.service.global_resolvers().remove(0);
    let (_, ok, err) = h
        .service
        .test_connectivity("acme.org", Some(&resolver), &cancel)
        .await;
    assert!(ok);
    assert!(err.unwrap().is_blocked());
}

#[tokio::test]
async fn cache_api_and_clear() {
    use pm_api::{Api, Request};

    let h = Harness::new(&[A_URL]);
    h.service.resolve(q("acme.org")).await.unwrap();

    let api = Api::new("127.0.0.1:0");
    api.set_dev_mode(true);
    pm_resolver::api::register_api(&api, &h.service).unwrap();

    let get = |path: &str| api.handle(Request::new("GET", path).with_host("127.0.0.1:817"));

    let response = get("/api/v1/dns/cache/acme.org.A");
    assert_eq!(response.status, 200, "{}", response.body_string());
    let body: serde_json::Value = serde_json::from_str(&response.body_string()).unwrap();
    assert_eq!(body["Domain"], "acme.org.");
    assert_eq!(body["Question"], "A");
    assert_eq!(body["Answer"][0], "acme.org.\t17\tIN\tA\t192.0.2.99");
    assert_eq!(body["_meta"]["Key"], "cache:intel/nameRecord/acme.org.A");
    // The database expiry is the record expiry plus 14 days.
    assert_eq!(
        body["_meta"]["Expires"].as_i64().unwrap(),
        body["Expires"].as_i64().unwrap() + 86400 * 14
    );

    assert_eq!(get("/api/v1/dns/cache/missing.acme.org.A").status, 500);
    assert_eq!(get("/api/v1/dns/cache/Invalid.A").status, 404);

    let response = get("/api/v1/dns/resolvers");
    assert_eq!(response.status, 200);
    let body: serde_json::Value = serde_json::from_str(&response.body_string()).unwrap();
    assert_eq!(body.as_array().unwrap().len(), 1);
    assert_eq!(body[0]["ConfigURL"], A_URL);
    assert_eq!(body[0]["Failing"], false);
    assert_eq!(body[0]["Info"]["Name"], "a");
    assert!(body[0]["Search"].is_null());

    // Clearing needs the write permission and POST.
    assert_eq!(get("/api/v1/dns/clear").status, 405);
    let response = api.handle(Request::new("POST", "/api/v1/dns/clear").with_host("127.0.0.1:817"));
    assert_eq!(response.status, 200);
    assert_eq!(
        response.body_string().trim_end(),
        "cleared 1 dns cache entries"
    );
    assert_eq!(get("/api/v1/dns/cache/acme.org.A").status, 500);

    // Endpoint metadata.
    let cache = api.endpoint("dns/cache/{query}").unwrap();
    assert_eq!(cache.read, pm_api::PERMIT_USER);
    assert_eq!(
        api.endpoint("dns/resolvers").unwrap().read,
        pm_api::PERMIT_ANYONE
    );
    assert_eq!(
        api.endpoint("dns/clear").unwrap().write,
        pm_api::PERMIT_USER
    );
    assert_eq!(api.endpoint("dns/clear").unwrap().write_method, "POST");
}

#[tokio::test]
async fn resolver_list_changes_clear_the_cache() {
    let h = Harness::new(&[A_URL]);
    h.service.resolve(q("acme.org")).await.unwrap();
    assert!(h.service.store().get("acme.org.", "A").is_ok());

    // Unchanged config: kept.
    h.service.load_resolvers();
    assert!(h.service.store().get("acme.org.", "A").is_ok());

    h.settings
        .update(|s| s.nameservers = vec![A_URL.to_string(), B_URL.to_string()]);
    h.service.load_resolvers();
    assert!(h.service.store().get("acme.org.", "A").is_err());
}

#[tokio::test]
async fn nameserver_helpers() {
    let h = Harness::new(&[A_URL]);
    assert!(h
        .service
        .is_resolver_address("192.0.2.1".parse().unwrap(), 53));
    assert!(!h
        .service
        .is_resolver_address("192.0.2.1".parse().unwrap(), 54));
    let _ = Nameserver {
        ip: "192.0.2.1".parse().unwrap(),
        search: vec![],
    };
    h.service.force_resolver_reconnect().await;
}
