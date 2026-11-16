//! Tests of the varlink event parser and the DNS monitor, using real-shaped
//! systemd-resolved messages and injected sources/sinks (no systemd, no root).

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};

use pm_dnsmonitor::monitor::DnsMonitorError;
use pm_dnsmonitor::source::SourceError;
use pm_dnsmonitor::{
    parse_event, parse_query_result, DnsEventSource, DnsInfoSink, DnsMonitor, EventOutcome,
    FakeEventSource, NoopSelfCheck, SystemdVarlinkSource,
};
use pm_resolver::{ResolvedDomain, SERVER_SOURCE_SYSTEMD, SERVER_TYPE_MONITOR};

#[derive(Default)]
struct RecordingSink {
    saves: Mutex<Vec<(String, String, ResolvedDomain)>>,
    changes: AtomicUsize,
}

impl DnsInfoSink for RecordingSink {
    fn save(&self, scope: &str, ip: &str, domain: ResolvedDomain) -> Result<(), String> {
        self.saves
            .lock()
            .unwrap()
            .push((scope.to_string(), ip.to_string(), domain));
        Ok(())
    }

    fn changed(&self) {
        self.changes.fetch_add(1, Ordering::SeqCst);
    }
}

fn a_event() -> String {
    // 93.184.216.34, base64-encoded exactly like a varlink `bytes` field.
    r#"{"parameters":{"question":[{"class":1,"type":1,"name":"example.com."}],"answer":[{"ifindex":0,"raw":"example.com. IN A 93.184.216.34","rr":{"key":{"class":1,"type":1,"name":"example.com."},"address":"XbjYIg=="}}],"ready":true,"state":"success"},"continues":true}"#.into()
}

fn monitor(source: FakeEventSource, sink: Arc<RecordingSink>) -> DnsMonitor {
    DnsMonitor::new(Box::new(source), sink, Arc::new(NoopSelfCheck))
}

#[test]
fn parses_real_varlink_event() {
    let event = parse_event(&a_event()).unwrap();
    assert!(event.continues);
    let question = &event.parameters.question.as_ref().unwrap()[0];
    assert_eq!(question.name, "example.com.");
    assert_eq!((question.class, question.r#type), (1, 1));

    let answer = &event.parameters.answer.as_ref().unwrap()[0];
    let rr = answer.rr.as_ref().unwrap();
    assert_eq!(rr.key.name, "example.com.");
    assert_eq!(rr.address.as_deref(), Some(&[93, 184, 216, 34][..]));
    assert_eq!(answer.ifindex, Some(0));
    assert_eq!(answer.raw, "example.com. IN A 93.184.216.34");
}

#[test]
fn parses_numeric_address_array_and_collected_questions() {
    let line = r#"{"parameters":{"collectedQuestions":[{"class":1,"type":1,"name":"one.example."},{"class":1,"type":28,"name":"two.example."}],"answer":[{"rr":{"key":{"class":1,"type":1,"name":"one.example."},"address":[1,1,1,1]}}],"rcode":0,"ready":true},"continues":true}"#;
    let event = parse_event(line).unwrap();
    assert_eq!(event.parameters.collected_questions.as_ref().unwrap().len(), 2);
    assert_eq!(
        event.parameters.answer.as_ref().unwrap()[0]
            .rr
            .as_ref()
            .unwrap()
            .address
            .as_deref(),
        Some(&[1, 1, 1, 1][..])
    );

    // A bare QueryResult is accepted as a continuing message.
    let bare = parse_event(r#"{"question":[{"class":1,"type":1,"name":"x."}],"rcode":0}"#).unwrap();
    assert!(bare.continues);
    assert_eq!(parse_query_result(line).unwrap().rcode, Some(0));
}

#[test]
fn rejects_empty_and_malformed_events() {
    assert!(parse_event("   ").is_err());
    assert!(parse_event("{not json}").is_err());
    // Invalid base64 is rejected while parsing the address.
    let bad = r#"{"parameters":{"answer":[{"rr":{"address":"!!!"}}]},"continues":true}"#;
    assert!(parse_event(bad).is_err());
}

#[test]
fn varlink_error_is_reported() {
    let line = r#"{"error":{"name":"io.systemd.Resolve.Monitor.NoSubscription","description":"no subscription"},"continues":false}"#;
    let event = parse_event(line).unwrap();
    assert!(event.varlink_error().is_some());

    let sink = Arc::new(RecordingSink::default());
    let result = monitor(
        FakeEventSource::new(vec![line.to_string()]),
        Arc::clone(&sink),
    )
    .process_event(line);
    assert!(matches!(result, Err(DnsMonitorError::Varlink(_))));
    assert!(sink.saves.lock().unwrap().is_empty());
}

#[test]
fn saves_ip_and_cname_with_system_resolver_metadata() {
    let line = r#"{"parameters":{"question":[{"class":1,"type":1,"name":"example.com."}],"answer":[{"rr":{"key":{"class":1,"type":1,"name":"example.com."},"address":"yBP4AA=="}},{"rr":{"key":{"class":1,"type":5,"name":"example.com."},"name":"www.example.com."}}]},"continues":true}"#;
    let sink = Arc::new(RecordingSink::default());
    let dns = monitor(FakeEventSource::new(vec![]), Arc::clone(&sink));
    assert_eq!(dns.process_event(line).unwrap(), EventOutcome::Continue);

    let saves = sink.saves.lock().unwrap();
    assert_eq!(saves.len(), 1);
    assert_eq!(saves[0].0, "global");
    assert_eq!(saves[0].1, "200.19.248.0");
    let record = &saves[0].2;
    assert_eq!(record.domain, "example.com.");
    assert_eq!(record.cnames.as_deref(), Some(&["www.example.com.".to_string()][..]));
    assert_eq!(record.expires, 0);
    assert!(record.dns_request_context.is_some());
    let resolver = record.resolver.as_ref().unwrap();
    assert_eq!(resolver.name, "SystemResolver");
    assert_eq!(resolver.r#type, SERVER_TYPE_MONITOR);
    assert_eq!(resolver.source, SERVER_SOURCE_SYSTEMD);
    assert_eq!(sink.changes.load(Ordering::SeqCst), 1);
}

#[test]
fn ignores_no_question_dns_errors_and_self_check_domains() {
    let sink = Arc::new(RecordingSink::default());
    let self_checks = Arc::new(Mutex::new(Vec::new()));
    let seen = Arc::clone(&self_checks);
    let dns = DnsMonitor::new(
        Box::new(FakeEventSource::new(vec![])),
        sink.clone(),
        Arc::new(move |subdomain: &str| seen.lock().unwrap().push(subdomain.to_string())),
    );

    dns.process_event(r#"{"parameters":{"answer":[{"rr":{"address":"XbjYIg=="}}]},"continues":true}"#)
        .unwrap();
    dns.process_event(r#"{"parameters":{"question":[{"name":"example.com."}],"rcode":2,"answer":[{"rr":{"address":"XbjYIg=="}}]},"continues":true}"#)
        .unwrap();
    dns.process_event(&format!(
        r#"{{"parameters":{{"question":[{{"name":"a1b2{}"}}],"answer":[{{"rr":{{"address":"XbjYIg=="}}}}]}},"continues":true}}"#,
        pm_compat::DNS_CHECK_INTERNAL_DOMAIN_SCOPE
    ))
    .unwrap();

    assert!(sink.saves.lock().unwrap().is_empty());
    assert_eq!(*self_checks.lock().unwrap(), ["a1b2"]);
}

#[test]
fn skips_localhost_ips() {
    let sink = Arc::new(RecordingSink::default());
    let dns = monitor(FakeEventSource::new(vec![]), Arc::clone(&sink));
    dns.process_event(
        r#"{"parameters":{"question":[{"name":"localhost."}],"answer":[{"rr":{"address":"fwAAAQ=="}}]},"continues":true}"#,
    )
    .unwrap();
    assert!(sink.saves.lock().unwrap().is_empty());
}

#[test]
fn continues_false_ends_the_stream() {
    let sink = Arc::new(RecordingSink::default());
    let dns = monitor(FakeEventSource::new(vec![]), Arc::clone(&sink));
    let outcome = dns
        .process_event(r#"{"parameters":{"question":[{"name":"example.com."}]},"continues":false}"#)
        .unwrap();
    assert_eq!(outcome, EventOutcome::EndStream);
    assert!(sink.saves.lock().unwrap().is_empty());
}

#[test]
fn missing_and_failing_socket_are_errors() {
    let mut source = SystemdVarlinkSource::with_path("/nonexistent/pm-dnsmonitor-test.socket");
    assert!(matches!(source.subscribe(), Err(SourceError::Unavailable(_))));
    assert!(matches!(source.recv(), Err(SourceError::Varlink(_))));

    let sink = Arc::new(RecordingSink::default());
    let mut dns = monitor(
        FakeEventSource::failing("failed to issue Varlink call"),
        Arc::clone(&sink),
    );
    assert!(matches!(dns.run_once(), Err(DnsMonitorError::Source(SourceError::Varlink(_)))));
    assert!(sink.saves.lock().unwrap().is_empty());
}

#[test]
fn start_and_stop_process_events_without_a_socket() {
    let sink = Arc::new(RecordingSink::default());
    let source = FakeEventSource::new(vec![a_event()]);
    let mut running = monitor(source, Arc::clone(&sink)).start();
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
    while !running.is_finished() && std::time::Instant::now() < deadline {
        std::thread::sleep(std::time::Duration::from_millis(1));
    }
    assert!(running.is_finished(), "listener did not finish");
    running.stop();
    running.flush();

    let saves = sink.saves.lock().unwrap();
    assert_eq!(saves.len(), 1);
    assert_eq!(saves[0].1, "93.184.216.34");
}
