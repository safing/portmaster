//! DNS monitor: consumes varlink query-result events and stores the reported
//! domain/IP attribution in the resolver's IP info cache.
//!
//! Port of Go `service/firewall/interception/dnsmonitor/module.go` and the
//! Linux `eventlistener_linux.go`. The event source and the cache sink are
//! injected, so all of the parsing and decision logic is testable without
//! systemd-resolved, a varlink socket or root privileges.

use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::{self, JoinHandle};

use pm_compat::DNS_CHECK_INTERNAL_DOMAIN_SCOPE;
use pm_netstate::IpScope;
use pm_resolver::ipinfo::{DnsRequestContext, IpInfoError, IP_INFO_PROFILE_SCOPE_GLOBAL};
use pm_resolver::model::{IpScope as ResolverIpScope, SERVER_SOURCE_SYSTEMD, SERVER_TYPE_MONITOR};
use pm_resolver::{IpInfo, IpInfoStore, ResolverInfo, ResolvedDomain};

use crate::event::{parse_event, EventParseError};
use crate::source::{DnsEventSource, SourceError};
use crate::varlinktypes::QueryResult;

/// How often the Go worker is restarted after a failed connection.
pub const RESTART_LIMIT: usize = 10;

/// Error of the DNS monitor.
#[derive(Debug, thiserror::Error)]
pub enum DnsMonitorError {
    #[error("{0}")]
    Source(#[from] SourceError),
    #[error("{0}")]
    Parse(#[from] EventParseError),
    #[error("failed to receive Varlink reply: {0}")]
    Varlink(String),
}

/// What the monitor should do after processing one message.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EventOutcome {
    /// More messages are expected.
    Continue,
    /// The varlink stream ended (`continues == 0`); the worker is done.
    EndStream,
}

/// Sink for the IP info records the monitor produces.
pub trait DnsInfoSink: Send + Sync {
    /// Saves one resolved domain for an IP in a profile scope.
    fn save(
        &self,
        profile_scope: &str,
        ip: &str,
        domain: ResolvedDomain,
    ) -> Result<(), String>;

    /// Called after a successful save to announce the change. The production
    /// sink notifies the network environment (Go's kernel-facing notification).
    fn changed(&self) {}
}

/// Adapter from the resolver's IP info store to [`DnsInfoSink`].
pub struct IpInfoStoreSink {
    store: Arc<IpInfoStore>,
}

impl IpInfoStoreSink {
    /// Creates a sink writing to the given store.
    pub fn new(store: Arc<IpInfoStore>) -> IpInfoStoreSink {
        IpInfoStoreSink { store }
    }
}

impl DnsInfoSink for IpInfoStoreSink {
    fn save(
        &self,
        profile_scope: &str,
        ip: &str,
        domain: ResolvedDomain,
    ) -> Result<(), String> {
        let mut info = match self.store.get(profile_scope, ip) {
            Ok(info) => info,
            Err(IpInfoError::NotFound) => IpInfo::new(profile_scope, ip),
            Err(error) => return Err(error.to_string()),
        };
        info.add_domain(domain);
        self.store.save(&mut info).map_err(|error| error.to_string())
    }

    fn changed(&self) {
        pm_netenv::notify_network_change();
    }
}

/// Sink for detected Portmaster self-check domains (Go
/// `compat.SubmitDNSCheckDomain`).
pub trait SelfCheckSink: Send + Sync {
    fn submit_dns_check_domain(&self, subdomain: &str);
}

/// Adapter for closures, so callers can forward to
/// `pm_compat::Compat::submit_dns_check_domain` without another wrapper type.
impl<F: Fn(&str) + Send + Sync> SelfCheckSink for F {
    fn submit_dns_check_domain(&self, subdomain: &str) {
        self(subdomain)
    }
}

/// Self-check sink that ignores detections.
pub struct NoopSelfCheck;

impl SelfCheckSink for NoopSelfCheck {
    fn submit_dns_check_domain(&self, _subdomain: &str) {}
}

/// Resolver info attached to every saved domain, like Go's `ResolverInfo`
/// variable with `Source = resolver.ServerSourceSystemd`.
pub fn system_resolver_info() -> ResolverInfo {
    ResolverInfo {
        name: "SystemResolver".into(),
        r#type: SERVER_TYPE_MONITOR.into(),
        source: SERVER_SOURCE_SYSTEMD.into(),
        ip: None,
        domain: String::new(),
        ip_scope: ResolverIpScope::Undefined,
        port: 0,
    }
}

/// Shared, source-independent state of the monitor.
struct MonitorState {
    sink: Arc<dyn DnsInfoSink>,
    self_check: Arc<dyn SelfCheckSink>,
    resolver_info: ResolverInfo,
}

impl MonitorState {
    fn process_event(&self, line: &str) -> Result<EventOutcome, DnsMonitorError> {
        let event = parse_event(line)?;
        if let Some(error) = event.varlink_error() {
            return Err(DnsMonitorError::Varlink(error));
        }
        // Go checks the varlink `continues` flag before looking at the result.
        if !event.continues {
            return Ok(EventOutcome::EndStream);
        }
        self.process_query_result(&event.parameters);
        Ok(EventOutcome::Continue)
    }

    fn process_query_result(&self, result: &QueryResult) {
        let Some(question) = result.question.as_ref().and_then(|q| q.first()) else {
            // Ignore events without a question.
            return;
        };

        let domain = &question.name;
        if self.process_self_check(&fqdn(domain)) {
            return;
        }
        if result.rcode.is_some() {
            // Ignore DNS errors.
            return;
        }

        self.process_answer(domain, result);
    }

    fn process_self_check(&self, fqdn: &str) -> bool {
        if let Some(subdomain) = fqdn.strip_suffix(DNS_CHECK_INTERNAL_DOMAIN_SCOPE) {
            self.self_check.submit_dns_check_domain(subdomain);
            tracing::info!("dnsmonitor: self-check domain received");
            true
        } else {
            false
        }
    }

    fn process_answer(&self, domain: &str, result: &QueryResult) {
        let Some(answers) = result.answer.as_ref() else {
            return;
        };

        let mut cnames = HashMap::new();
        let mut ips = Vec::with_capacity(5);

        for answer in answers {
            let Some(rr) = answer.rr.as_ref() else {
                continue;
            };
            if let Some(address) = rr.address.as_ref() {
                if let Some(ip) = bytes_to_ip(address) {
                    ips.push(ip);
                }
            } else if let Some(name) = rr.name.as_ref() {
                cnames.insert(domain.to_string(), name.clone());
            }
        }

        self.save_domain(domain, &ips, &cnames);
    }

    fn save_domain(&self, domain: &str, ips: &[IpAddr], cnames: &HashMap<String, String>) {
        let mut record = ResolvedDomain {
            domain: fqdn(domain),
            resolver: Some(self.resolver_info.clone()),
            dns_request_context: Some(DnsRequestContext::default()),
            expires: 0,
            ..ResolvedDomain::default()
        };
        record.add_cnames(cnames);

        for ip in ips {
            // Never save domain attributions for localhost IPs.
            if pm_netstate::ip_scope(*ip) == IpScope::HostLocal {
                continue;
            }
            let ip_string = ip.to_string();
            match self
                .sink
                .save(IP_INFO_PROFILE_SCOPE_GLOBAL, &ip_string, record.clone())
            {
                Ok(()) => self.sink.changed(),
                Err(error) => tracing::error!(%error, "dnsmonitor: failed to save IP info record"),
            }
        }
    }
}

fn fqdn(domain: &str) -> String {
    if domain.ends_with('.') {
        domain.to_string()
    } else {
        format!("{domain}.")
    }
}

fn bytes_to_ip(bytes: &[u8]) -> Option<IpAddr> {
    match bytes.len() {
        4 => {
            let mut octets = [0u8; 4];
            octets.copy_from_slice(bytes);
            Some(IpAddr::V4(Ipv4Addr::from(octets)))
        }
        16 => {
            let mut octets = [0u8; 16];
            octets.copy_from_slice(bytes);
            Some(IpAddr::V6(Ipv6Addr::from(octets)))
        }
        _ => None,
    }
}

fn run_stream(
    source: &mut dyn DnsEventSource,
    state: &MonitorState,
    cancel: &AtomicBool,
) -> Result<(), DnsMonitorError> {
    source.subscribe()?;
    loop {
        if cancel.load(Ordering::SeqCst) {
            return Ok(());
        }
        let Some(line) = source.recv()? else {
            return Ok(());
        };
        match state.process_event(&line)? {
            EventOutcome::Continue => {}
            EventOutcome::EndStream => return Ok(()),
        }
    }
}

/// Configured DNS monitor. Call [`DnsMonitor::start`] to run it on a thread,
/// or use [`DnsMonitor::process_event`] / [`DnsMonitor::run_once`] directly.
pub struct DnsMonitor {
    source: Box<dyn DnsEventSource>,
    state: Arc<MonitorState>,
}

impl DnsMonitor {
    /// Creates a monitor with an injected source, sink and self-check handler.
    pub fn new(
        source: Box<dyn DnsEventSource>,
        sink: Arc<dyn DnsInfoSink>,
        self_check: Arc<dyn SelfCheckSink>,
    ) -> DnsMonitor {
        DnsMonitor {
            source,
            state: Arc::new(MonitorState {
                sink,
                self_check,
                resolver_info: system_resolver_info(),
            }),
        }
    }

    /// Processes one raw varlink message without involving a socket or a
    /// thread. Intended for tests and for callers that drive the source
    /// themselves.
    pub fn process_event(&self, line: &str) -> Result<EventOutcome, DnsMonitorError> {
        self.state.process_event(line)
    }

    /// Subscribes and consumes messages from the source until the stream
    /// ends, without spawning a thread.
    pub fn run_once(&mut self) -> Result<(), DnsMonitorError> {
        let cancel = AtomicBool::new(false);
        run_stream(self.source.as_mut(), &self.state, &cancel)
    }

    /// Starts the event listener on a background thread.
    pub fn start(self) -> RunningDnsMonitor {
        let shutdown = self.source.shutdown_fn();
        let cancel = Arc::new(AtomicBool::new(false));
        let thread_cancel = Arc::clone(&cancel);
        let mut source = self.source;
        let state = Arc::clone(&self.state);

        let handle = thread::spawn(move || {
            for attempt in 1..=RESTART_LIMIT + 1 {
                if thread_cancel.load(Ordering::SeqCst) {
                    return;
                }
                match run_stream(source.as_mut(), &state, &thread_cancel) {
                    Ok(()) => return,
                    Err(error) => {
                        tracing::warn!(%error, attempt, "dnsmonitor: resolver event listener failed");
                    }
                }
            }
        });

        RunningDnsMonitor {
            cancel,
            shutdown,
            handle: Some(handle),
        }
    }
}

/// Handle of a running monitor, with `stop`/`flush` like the Go module.
pub struct RunningDnsMonitor {
    cancel: Arc<AtomicBool>,
    shutdown: Option<crate::source::ShutdownFn>,
    handle: Option<JoinHandle<()>>,
}

impl RunningDnsMonitor {
    /// Stops the listener and waits for its thread.
    pub fn stop(&mut self) {
        self.cancel.store(true, Ordering::SeqCst);
        if let Some(shutdown) = &self.shutdown {
            shutdown();
        }
        if let Some(handle) = self.handle.take() {
            let _ = handle.join();
        }
    }

    /// Flushes buffered events. The varlink source has no buffer of its own,
    /// so this is a no-op like Go `Listener.flush`.
    pub fn flush(&self) {}

    /// Returns whether the listener thread has finished.
    pub fn is_finished(&self) -> bool {
        self.handle.as_ref().is_some_and(|handle| handle.is_finished())
    }
}

impl Drop for RunningDnsMonitor {
    fn drop(&mut self) {
        self.stop();
    }
}
