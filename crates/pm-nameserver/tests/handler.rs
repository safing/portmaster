//! Request flow tests: the handler is driven directly with parsed messages.

mod common;

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};
use std::sync::atomic::Ordering;
use std::sync::Arc;

use common::*;
use hickory_proto::op::{Message, ResponseCode};
use hickory_proto::rr::{DNSClass, RData, RecordType};
use pm_nameserver::{FilterOutcome, Responder, StaticResponder};
use pm_network::Verdict;
use pm_resolver::QueryError;

async fn ask(rig: &Rig, name: &str, qtype: RecordType) -> Option<Message> {
    rig.ns.handle_request(&query_message(name, qtype), remote()).await
}

#[tokio::test]
async fn resolves_and_calls_hook_around_the_resolver() {
    let rig = rig(FakeResolver::answering());
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();

    assert_eq!(reply.metadata.id, 0x1234);
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    assert!(reply.metadata.recursion_available);
    assert!(reply.metadata.recursion_desired);
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1))]);
    assert_eq!(reply.queries.len(), 1);

    let extras = txt_extras(&reply);
    assert!(extras[0].starts_with("freshly resolved by Fake"), "{extras:?}");
    assert!(extras[1].starts_with("record valid for "), "{extras:?}");

    assert_eq!(rig.hook.log.filter_connection.load(Ordering::SeqCst), 1);
    assert_eq!(rig.hook.log.filter_resolved.load(Ordering::SeqCst), 1);
    assert_eq!(rig.hook.log.update_ips.load(Ordering::SeqCst), 1);
    assert_eq!(rig.hook.log.saved_open.load(Ordering::SeqCst), 1);
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 0);

    let requests = rig.attributor.requests.lock().unwrap();
    assert_eq!(requests.len(), 1);
    assert_eq!(requests[0].fqdn, "example.com.");
    assert!(requests[0].local);
    assert_eq!(requests[0].conn_id, "127.0.0.1-40000-#4660-example.com.A");
}

#[tokio::test]
async fn connection_receives_dns_context_and_resolver() {
    let rig = rig(FakeResolver::answering());
    let _ = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    let conn = rig.attributor.conn();
    let conn = conn.lock().unwrap();
    let ctx = conn.dns_context.as_ref().unwrap();
    assert_eq!(ctx.domain, "example.com.");
    assert_eq!(ctx.question, "A");
    assert_eq!(ctx.rcode, "NOERROR");
    assert!(!ctx.served_from_cache);
    assert_eq!(conn.resolver.as_ref().unwrap().name, "Fake");
    assert!(!conn.internal);
}

#[tokio::test]
async fn non_standard_question_case_is_restored_in_answers() {
    let rig = rig(FakeResolver::answering());
    let reply = ask(&rig, "ExAmPlE.CoM.", RecordType::A).await.unwrap();
    assert_eq!(reply.answers[0].name.to_ascii(), "ExAmPlE.CoM.");
    assert_eq!(reply.queries[0].name().to_ascii(), "ExAmPlE.CoM.");
    // The resolver was asked with the lower case name.
    assert_eq!(rig.attributor.requests.lock().unwrap()[0].fqdn, "example.com.");
}

#[tokio::test]
async fn block_verdict_before_resolving() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.pre.lock().unwrap() = Some((Verdict::Block, "test reason".into()));

    let reply = ask(&rig, "ads.example.", RecordType::A).await.unwrap();
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 0);
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);
    assert_eq!(reply.answers[0].ttl, 1);
    assert!(reply.metadata.recursion_available);
    assert_eq!(txt_extras(&reply), vec!["block: test reason".to_string()]);
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);
    assert_eq!(rig.hook.log.filter_resolved.load(Ordering::SeqCst), 0);

    let reply = ask(&rig, "ads.example.", RecordType::AAAA).await.unwrap();
    assert_eq!(
        answer_ips(&reply),
        vec![IpAddr::V6("::17".parse::<Ipv6Addr>().unwrap())]
    );
}

#[tokio::test]
async fn failed_verdict_blocks_with_error_record() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.pre.lock().unwrap() = Some((Verdict::Failed, "no profile".into()));
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);
    assert_eq!(reply.additionals[0].name.to_ascii(), "err.portmaster.");
    assert_eq!(txt_extras(&reply), vec!["failed: no profile".to_string()]);
}

#[tokio::test]
async fn drop_verdict_sends_nothing() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.pre.lock().unwrap() = Some((Verdict::Drop, "silently".into()));
    assert!(ask(&rig, "example.com.", RecordType::A).await.is_none());
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn block_verdict_after_resolving_keeps_cache_info() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.post.lock().unwrap() = Some((Verdict::Block, "filtered".into()));
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);
    let extras = txt_extras(&reply);
    assert_eq!(extras[0], "block: filtered");
    assert!(extras[1].starts_with("freshly resolved by "), "{extras:?}");
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn hook_responder_takes_over_before_resolving() {
    let rig = rig(FakeResolver::answering());
    let responder: Arc<dyn Responder> = Arc::new(StaticResponder::zero_ip(&["sinkholed"]));
    *rig.hook.pre_responder.lock().unwrap() = Some(responder);
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 0);
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::UNSPECIFIED)]);
    assert!(reply.metadata.recursion_available);
    assert!(txt_extras(&reply).contains(&"sinkholed".to_string()));
    let _ = FilterOutcome::default();
}

#[tokio::test]
async fn hook_dropping_the_cache_is_a_server_failure() {
    let rig = rig(FakeResolver::answering());
    *rig.hook.post_drops_cache.lock().unwrap() = true;
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::ServFail);
}

#[tokio::test(start_paused = true)]
async fn not_found_becomes_nxdomain_after_a_delay() {
    let rig = rig(FakeResolver::failing(QueryError::NotFound));
    let start = tokio::time::Instant::now();
    let reply = ask(&rig, "missing.example.", RecordType::A).await.unwrap();
    assert!(start.elapsed() >= std::time::Duration::from_millis(500));
    assert_eq!(reply.metadata.response_code, ResponseCode::NXDomain);
    assert_eq!(
        txt_extras(&reply),
        vec!["nxdomain: record could not be found".to_string()]
    );
    // Go sets the verdict to failed and saves the connection.
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn blocked_error_becomes_block_ip_answer() {
    let rig = rig(FakeResolver::failing(QueryError::Blocked));
    let reply = ask(&rig, "blocked.example.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);
    assert_eq!(txt_extras(&reply), vec!["blocked: query was blocked".to_string()]);
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);

    let rig = self::rig(FakeResolver::failing(QueryError::NoCompliance));
    let reply = ask(&rig, "blocked.example.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(0, 0, 0, 17))]);
}

#[tokio::test]
async fn localhost_error_answers_localhost_and_accepts() {
    let rig = rig(FakeResolver::failing(QueryError::Localhost));
    let reply = ask(&rig, "foo.example.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::LOCALHOST)]);
    // Accepted connections are saved by the deferred code only with a reply.
    assert_eq!(rig.hook.log.saved_open.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn offline_without_cache_is_servfail() {
    let rig = rig(FakeResolver::failing(QueryError::Offline));
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::ServFail);
    assert_eq!(txt_extras(&reply), vec!["device is offine".to_string()]);
    assert_eq!(rig.hook.log.saved_conn.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn offline_with_stale_cache_serves_backup() {
    let stale = rr_a("example.com.", Ipv4Addr::new(192, 0, 2, 9));
    let rig = rig(FakeResolver::new(Box::new(move |_| {
        Err(pm_resolver::ResolveFailure {
            error: QueryError::Offline,
            stale: Some(Box::new(stale.clone())),
        })
    })));
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::new(192, 0, 2, 9))]);
    assert!(txt_extras(&reply)
        .contains(&"this record is served because a fresh request was unsuccessful".to_string()));
}

#[tokio::test(start_paused = true)]
async fn other_errors_are_servfail_and_cached_as_failing() {
    let rig = rig(FakeResolver::failing(QueryError::Failure));
    let reply = ask(&rig, "broken.example.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::ServFail);
    assert_eq!(
        txt_extras(&reply),
        vec!["internal error: query failed".to_string()]
    );
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 1);

    // The immediate retry is answered from the failing cache, delayed.
    let start = tokio::time::Instant::now();
    let reply = ask(&rig, "broken.example.", RecordType::A).await.unwrap();
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 1);
    assert_eq!(reply.metadata.response_code, ResponseCode::ServFail);
    let extras = txt_extras(&reply);
    assert_eq!(extras[0], "internal error: query failed");
    assert_eq!(extras[1], "delayed failing query to mitigate request flooding");
    assert!(start.elapsed() >= std::time::Duration::from_millis(1));
    // Both the hook and the resolver were skipped for the failing query.
    assert_eq!(rig.hook.log.filter_connection.load(Ordering::SeqCst), 1);
}

#[tokio::test(start_paused = true)]
async fn nxdomain_answer_without_alternatives() {
    let rig = rig(FakeResolver::new(Box::new(|q| {
        let mut rr = rr_a(&q.fqdn, Ipv4Addr::LOCALHOST);
        rr.rcode = 3;
        rr.answer.clear();
        Ok(rr)
    })));
    let reply = ask(&rig, "nothing.example.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NXDomain);
    assert_eq!(
        txt_extras(&reply),
        vec!["nxdomain: no answer found (NXDomain)".to_string()]
    );
    // The deferred saving still runs with the NXDOMAIN cache.
    assert_eq!(rig.hook.log.saved_open.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn nxdomain_uses_alternative_local_cache() {
    let resolver = FakeResolver::new(Box::new(|q| {
        let mut rr = rr_a(&q.fqdn, Ipv4Addr::LOCALHOST);
        rr.rcode = 3;
        rr.answer.clear();
        Ok(rr)
    }));
    // Entry for "<name>local." as the Go code looks it up.
    *resolver.alt.lock().unwrap() = Some(rr_a("printer.lanlocal.", Ipv4Addr::new(10, 0, 0, 7)));
    let rig = rig(resolver);
    let reply = ask(&rig, "printer.lan.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    let names: Vec<String> = reply.answers.iter().map(|r| r.name.to_ascii()).collect();
    assert!(names.contains(&"printer.lan.".to_string()), "{names:?}");
}

#[tokio::test]
async fn alternative_caches_are_skipped_in_icann_space() {
    let resolver = FakeResolver::new(Box::new(|q| {
        let mut rr = rr_a(&q.fqdn, Ipv4Addr::LOCALHOST);
        rr.rcode = 3;
        rr.answer.clear();
        Ok(rr)
    }));
    *resolver.alt.lock().unwrap() = Some(rr_a("host.example.comlocal.", Ipv4Addr::new(10, 0, 0, 7)));
    let rig = rig(resolver);
    tokio::time::pause();
    let reply = ask(&rig, "host.example.com.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NXDomain);
}

#[tokio::test]
async fn localhost_and_hostname_are_answered_locally() {
    let rig = rig(FakeResolver::answering());
    let reply = ask(&rig, "foo.localhost.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::LOCALHOST)]);
    let reply = ask(&rig, "localhost.", RecordType::AAAA).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V6(Ipv6Addr::LOCALHOST)]);
    let reply = ask(&rig, "testhost.", RecordType::A).await.unwrap();
    assert_eq!(answer_ips(&reply), vec![IpAddr::V4(Ipv4Addr::LOCALHOST)]);
    // TXT has no localhost answer: NXDOMAIN after the delay.
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 0);
    assert_eq!(rig.hook.log.filter_connection.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn unsupported_class_is_refused() {
    let rig = rig(FakeResolver::answering());
    let mut msg = query_message("example.com.", RecordType::A);
    msg.queries[0].set_query_class(DNSClass::CH);
    let reply = rig.ns.handle_request(&msg, remote()).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::Refused);
    assert_eq!(txt_extras(&reply), vec!["unsupported qclass".to_string()]);
    assert!(reply.metadata.recursion_available);
}

#[tokio::test]
async fn missing_and_multiple_questions_are_refused() {
    let rig = rig(FakeResolver::answering());
    let mut msg = query_message("example.com.", RecordType::A);
    msg.queries.clear();
    let reply = rig.ns.handle_request(&msg, remote()).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::Refused);
    assert_eq!(txt_extras(&reply), vec!["no question provided".to_string()]);

    let mut msg = query_message("example.com.", RecordType::A);
    let extra = query_message("example.org.", RecordType::A).queries[0].clone();
    msg.queries.push(extra);
    let reply = rig.ns.handle_request(&msg, remote()).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::Refused);
    assert_eq!(txt_extras(&reply), vec!["multiple question provided".to_string()]);
}

#[tokio::test]
async fn invalid_domain_is_refused() {
    let rig = rig(FakeResolver::answering());
    let reply = ask(&rig, "exa mple.com.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::Refused);
    assert_eq!(txt_extras(&reply), vec!["invalid domain".to_string()]);
    assert_eq!(rig.resolver.calls.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn external_requests_are_refused_unless_network_service() {
    let env = FakeEnv {
        local: false,
        ..FakeEnv::default()
    };
    let rig = rig_with(FakeResolver::answering(), env, FakeAttributor::new());
    let external: SocketAddr = "192.0.2.77:5353".parse().unwrap();
    let reply = rig
        .ns
        .handle_request(&query_message("example.com.", RecordType::A), external)
        .await
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::Refused);
    assert_eq!(
        txt_extras(&reply),
        vec!["external queries are not permitted".to_string()]
    );
    assert!(rig.attributor.requests.lock().unwrap().is_empty());

    let env = FakeEnv {
        local: false,
        network_service: true,
        ..FakeEnv::default()
    };
    let rig = rig_with(FakeResolver::answering(), env, FakeAttributor::new());
    let reply = rig
        .ns
        .handle_request(&query_message("example.com.", RecordType::A), external)
        .await
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    let requests = rig.attributor.requests.lock().unwrap();
    assert!(!requests[0].local);
}

#[tokio::test]
async fn attribution_failure_is_a_server_failure() {
    let env = FakeEnv {
        local: false,
        network_service: true,
        ..FakeEnv::default()
    };
    let attributor = FakeAttributor::with(true, false);
    let rig = rig_with(FakeResolver::answering(), env, attributor);
    let external: SocketAddr = "192.0.2.77:5353".parse().unwrap();
    let reply = rig
        .ns
        .handle_request(&query_message("example.com.", RecordType::A), external)
        .await
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::ServFail);
    assert_eq!(
        txt_extras(&reply),
        vec!["internal error: failed to get profile".to_string()]
    );
}

#[tokio::test]
async fn raw_packets_follow_the_server_library_rules() {
    let rig = rig(FakeResolver::answering());
    let a = remote();

    // Too short: ignored.
    assert!(rig.ns.handle_packet(&[1, 2, 3], a).await.is_none());

    // A response is ignored.
    let mut response = query_message("example.com.", RecordType::A);
    response.metadata.message_type = hickory_proto::op::MessageType::Response;
    assert!(rig.ns.handle_packet(&response.to_vec().unwrap(), a).await.is_none());

    // Valid header (one question) but garbage body: FORMERR.
    let mut garbage = vec![0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0];
    garbage.extend_from_slice(&[0xff, 0xff, 0xff]);
    let reply = Message::from_vec(&rig.ns.handle_packet(&garbage, a).await.unwrap()).unwrap();
    assert_eq!(reply.metadata.id, 0x1234);
    assert_eq!(reply.metadata.response_code, ResponseCode::FormErr);
    assert!(reply.metadata.recursion_desired);
    assert!(reply.queries.is_empty());

    // Zero questions: FORMERR without calling the handler.
    let mut empty = query_message("example.com.", RecordType::A);
    empty.queries.clear();
    let reply = Message::from_vec(&rig.ns.handle_packet(&empty.to_vec().unwrap(), a).await.unwrap())
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::FormErr);

    // Unsupported opcode: NOTIMP.
    let mut update = query_message("example.com.", RecordType::A);
    update.metadata.op_code = hickory_proto::op::OpCode::Update;
    let reply = Message::from_vec(&rig.ns.handle_packet(&update.to_vec().unwrap(), a).await.unwrap())
        .unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NotImp);
    assert_eq!(reply.metadata.op_code, hickory_proto::op::OpCode::Update);

    // A normal query works and is not answered with hickory extras.
    let wire = rig
        .ns
        .handle_packet(&query_message("example.com.", RecordType::A).to_vec().unwrap(), a)
        .await
        .unwrap();
    let reply = Message::from_vec(&wire).unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    assert!(!reply.answers.is_empty());
    assert!(matches!(reply.answers[0].data, RData::A(_)));
}

#[tokio::test]
async fn system_resolver_requests_are_marked_internal() {
    let attributor = FakeAttributor::with(false, true);
    let rig = rig_with(FakeResolver::answering(), FakeEnv::default(), attributor);
    let reply = ask(&rig, "example.com.", RecordType::A).await.unwrap();
    assert_eq!(reply.metadata.response_code, ResponseCode::NoError);
    assert!(rig.attributor.conn().lock().unwrap().internal);
}
