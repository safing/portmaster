//! Self-check logic, port of Go `service/compat/selfcheck.go` and the related
//! hooks in `callbacks.go`.
//!
//! All I/O (online status, raw packet, DNS lookup and the two observation
//! channels) is abstracted by [`Probe`]. The [`selfcheck`] function contains
//! only the decision logic, so it can be driven by a fake probe in tests.
//! [`RealProbe`] is the production adapter used by the core.

use std::net::{IpAddr, SocketAddr, ToSocketAddrs};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use crate::notify::SystemIssueKind;

/// Destination IP of the system-integration test packet: `127.65.67.75`.
pub const SYSTEM_INTEGRATION_CHECK_DST_IP: [u8; 4] = [127, 65, 67, 75];
/// IP protocol of the system-integration test packet (Go `AnyHostInternalProtocol61`).
pub const SYSTEM_INTEGRATION_CHECK_PROTOCOL: u8 = 61;
/// Payload written by Go onto the raw connection.
pub const SELF_CHECK_PAYLOAD: &[u8] = b"PORTMASTER SELF CHECK";
/// `".self-check." + resolver.InternalSpecialUseDomain` (`portmaster.home.arpa.`).
pub const DNS_CHECK_INTERNAL_DOMAIN_SCOPE: &str = ".self-check.portmaster.home.arpa.";

const DEFAULT_WAIT: Duration = Duration::from_secs(45);

/// Result of a DNS lookup performed by [`Probe::start_dns_lookup`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DnsResult {
    /// The first IPv4 answer, if any (Go uses `ips[0]`).
    Answer(Option<[u8; 4]>),
    /// The resolver returned an error (Go `net.LookupIP` error).
    LookupError(String),
}

/// Errors of the raw-packet send step, mapped to the two Go error messages.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PacketSendError {
    Create(String),
    Write(String),
}

/// Network and randomness operations needed by [`selfcheck`].
pub trait Probe: Send + Sync {
    /// Go `netenv.Online`.
    fn online(&self) -> bool;

    /// Drains stale observations before a check, like the Go `select`/`default`
    /// channel drains.
    fn reset(&self) {}

    /// Sends the raw "PORTMASTER SELF CHECK" packet to the test destination.
    fn send_system_integration_packet(&self) -> Result<(), PacketSendError>;

    /// Waits for the firewall to report the packet back. `true` means seen.
    fn wait_system_integration_packet(&self, timeout: Duration) -> bool;

    /// 16 random bytes for the query subdomain (Go `rng.Bytes`).
    fn random_bytes(&self, len: usize) -> Result<Vec<u8>, String>;

    /// Random number in `0..=max` (Go `rng.Number` is inclusive).
    fn random_number(&self, max: u64) -> Result<u64, String>;

    /// Publishes the random answer so the DNS hook can return it.
    fn set_dns_answer(&self, ip: [u8; 4]);

    /// Starts the lookup in the background, like the Go lookup worker.
    fn start_dns_lookup(&self, domain: String);

    /// Waits for the nameserver/firewall to report the queried subdomain.
    fn wait_dns_query(&self, timeout: Duration) -> Option<String>;

    /// Waits for the background lookup result.
    fn wait_dns_result(&self, timeout: Duration) -> Option<DnsResult>;
}

/// Configuration values read from the environment by the self-check.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SelfcheckConfig {
    /// Go `dnsQueryInterception()`, used to pick the compatibility vs manual-DNS
    /// issue in step 2.
    pub dns_query_interception: bool,
    /// Go `module.instance.Resolver().IsDisabled()`.
    pub resolver_disabled: bool,
    pub integration_wait: Duration,
    pub dns_wait: Duration,
}

impl Default for SelfcheckConfig {
    fn default() -> Self {
        SelfcheckConfig {
            dns_query_interception: true,
            resolver_disabled: false,
            integration_wait: DEFAULT_WAIT,
            dns_wait: DEFAULT_WAIT,
        }
    }
}

/// Kind of a self-check error, mirroring the `(issue, err)` return of Go.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SelfcheckErrorKind {
    /// The check was skipped (offline / limited network).
    Skipped,
    /// An internal error, not attributable to a system issue.
    Internal,
    /// A compatibility failure with a concrete issue.
    Issue,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SelfcheckError {
    pub kind: SelfcheckErrorKind,
    pub issue: Option<SystemIssueKind>,
    pub message: String,
}

impl SelfcheckError {
    fn skipped(message: String) -> Self {
        Self {
            kind: SelfcheckErrorKind::Skipped,
            issue: None,
            message,
        }
    }

    fn internal(message: String) -> Self {
        Self {
            kind: SelfcheckErrorKind::Internal,
            issue: None,
            message,
        }
    }

    fn issue(issue: SystemIssueKind, message: String) -> Self {
        Self {
            kind: SelfcheckErrorKind::Issue,
            issue: Some(issue),
            message,
        }
    }
}

/// Renders a duration like Go's `time.Duration.String()` for whole seconds.
fn format_wait(duration: Duration) -> String {
    format!("{}s", duration.as_secs())
}

fn issue_for_dns_step(config: &SelfcheckConfig) -> SystemIssueKind {
    if config.dns_query_interception {
        SystemIssueKind::SystemCompatibility
    } else {
        SystemIssueKind::ManualDnsSetup
    }
}

fn format_ip(ip: Option<[u8; 4]>) -> String {
    match ip {
        Some([a, b, c, d]) => format!("{a}.{b}.{c}.{d}"),
        None => String::new(),
    }
}

/// Runs the three self-check steps. Pure with respect to its arguments: all
/// I/O goes through `probe`.
pub fn selfcheck(probe: &dyn Probe, config: &SelfcheckConfig) -> Result<(), SelfcheckError> {
    // Step 0: the check only makes sense when online.
    if !probe.online() {
        return Err(SelfcheckError::skipped(
            "self-check skipped: device is offline or in limited network".to_string(),
        ));
    }

    probe.reset();

    // Step 1: the system integration must see the raw packet.
    if let Err(error) = probe.send_system_integration_packet() {
        let detail = match error {
            PacketSendError::Create(detail) => {
                format!("failed to create system integration conn: {detail}")
            }
            PacketSendError::Write(detail) => {
                format!("failed to send system integration packet: {detail}")
            }
        };
        return Err(SelfcheckError::internal(detail));
    }
    if !probe.wait_system_integration_packet(config.integration_wait) {
        return Err(SelfcheckError::issue(
            SystemIssueKind::SystemIntegration,
            format!(
                "self-check #1: system integration check failed: did not receive test packet after {}",
                format_wait(config.integration_wait)
            ),
        ));
    }

    // Step 2: a DNS query for a random subdomain must arrive at the nameserver.
    let random_bytes = probe.random_bytes(16).map_err(|error| {
        SelfcheckError::internal(format!(
            "self-check #2: failed to get random bytes for subdomain check: {error}"
        ))
    })?;
    let random_subdomain = format!("a{}b", hex::encode(&random_bytes));

    let b = probe.random_number(255).map_err(|error| {
        SelfcheckError::internal(format!(
            "self-check #2: failed to get random number for subdomain check response: {error}"
        ))
    })?;
    let c = probe.random_number(255).map_err(|error| {
        SelfcheckError::internal(format!(
            "self-check #2: failed to get random number for subdomain check response: {error}"
        ))
    })?;
    let d = probe.random_number(255).map_err(|error| {
        SelfcheckError::internal(format!(
            "self-check #2: failed to get random number for subdomain check response: {error}"
        ))
    })?;
    let random_answer = [127, b as u8, c as u8, d as u8];
    probe.set_dns_answer(random_answer);

    probe.start_dns_lookup(format!("{random_subdomain}{DNS_CHECK_INTERNAL_DOMAIN_SCOPE}"));

    match probe.wait_dns_query(config.dns_wait) {
        Some(received) if received == random_subdomain => {}
        Some(received) => {
            return Err(SelfcheckError::issue(
                issue_for_dns_step(config),
                format!(
                    "self-check #2: dns integration check failed: received unmatching subdomain {received:?}"
                ),
            ));
        }
        None => {
            return Err(SelfcheckError::issue(
                issue_for_dns_step(config),
                format!(
                    "self-check #2: dns integration check failed: did not receive test query after {}",
                    format_wait(config.dns_wait)
                ),
            ));
        }
    }

    // Step 3: when the resolver is disabled its response cannot be checked.
    if config.resolver_disabled {
        return Ok(());
    }

    match probe.wait_dns_result(config.dns_wait) {
        Some(DnsResult::LookupError(error)) => {
            return Err(SelfcheckError::issue(
                SystemIssueKind::SystemCompatibility,
                format!(
                    "self-check #3: dns integration check failed: failed to receive test response: {error}"
                ),
            ));
        }
        None => {
            return Err(SelfcheckError::issue(
                SystemIssueKind::SystemCompatibility,
                format!(
                    "self-check #3: dns integration check failed: did not receive test response after {}",
                    format_wait(config.dns_wait)
                ),
            ));
        }
        Some(DnsResult::Answer(Some(ip))) if ip == random_answer => {}
        Some(DnsResult::Answer(other)) => {
            return Err(SelfcheckError::issue(
                SystemIssueKind::SystemCompatibility,
                format!(
                    "self-check #3: dns integration check failed: received unmatching response \"{}\"",
                    format_ip(other)
                ),
            ));
        }
    }

    Ok(())
}

/// Production [`Probe`]. The senders are called by the firewall hooks; the
/// receivers are consumed by the self-check.
pub struct RealProbe {
    packet_tx: mpsc::Sender<()>,
    packet_rx: Mutex<mpsc::Receiver<()>>,
    domain_tx: mpsc::Sender<String>,
    domain_rx: Mutex<mpsc::Receiver<String>>,
    dns_result_tx: mpsc::Sender<DnsResult>,
    dns_result_rx: Mutex<mpsc::Receiver<DnsResult>>,
    dns_answer: Arc<Mutex<Option<[u8; 4]>>>,
}

impl Default for RealProbe {
    fn default() -> Self {
        Self::new()
    }
}

impl RealProbe {
    pub fn new() -> RealProbe {
        let (packet_tx, packet_rx) = mpsc::channel();
        let (domain_tx, domain_rx) = mpsc::channel();
        let (dns_result_tx, dns_result_rx) = mpsc::channel();
        RealProbe {
            packet_tx,
            packet_rx: Mutex::new(packet_rx),
            domain_tx,
            domain_rx: Mutex::new(domain_rx),
            dns_result_tx,
            dns_result_rx: Mutex::new(dns_result_rx),
            dns_answer: Arc::new(Mutex::new(None)),
        }
    }

    /// Go `compat.SubmitSystemIntegrationCheckPacket`.
    pub fn submit_system_integration_check_packet(&self) {
        let _ = self.packet_tx.send(());
    }

    /// Go `compat.SubmitDNSCheckDomain`; returns the configured answer.
    pub fn submit_dns_check_domain(&self, subdomain: &str) -> Option<[u8; 4]> {
        let _ = self.domain_tx.send(subdomain.to_string());
        *self.dns_answer.lock().expect("lock poisoned")
    }

    fn drain<T>(rx: &Mutex<mpsc::Receiver<T>>) {
        let rx = rx.lock().expect("lock poisoned");
        while rx.try_recv().is_ok() {}
    }
}

impl Probe for RealProbe {
    fn online(&self) -> bool {
        pm_netenv::online()
    }

    fn reset(&self) {
        Self::drain(&self.packet_rx);
        Self::drain(&self.domain_rx);
        Self::drain(&self.dns_result_rx);
    }

    fn send_system_integration_packet(&self) -> Result<(), PacketSendError> {
        #[cfg(target_os = "linux")]
        {
            use socket2::{Domain, Protocol, SockAddr, Socket, Type};
            let socket = Socket::new(
                Domain::IPV4,
                Type::RAW,
                Some(Protocol::from(SYSTEM_INTEGRATION_CHECK_PROTOCOL as i32)),
            )
            .map_err(|error| PacketSendError::Create(error.to_string()))?;
            let destination = SockAddr::from(SocketAddr::from((
                SYSTEM_INTEGRATION_CHECK_DST_IP,
                0u16,
            )));
            socket
                .send_to(SELF_CHECK_PAYLOAD, &destination)
                .map_err(|error| PacketSendError::Write(error.to_string()))?;
            Ok(())
        }
        #[cfg(not(target_os = "linux"))]
        {
            Err(PacketSendError::Create(
                "raw IP sockets are only available on Linux".to_string(),
            ))
        }
    }

    fn wait_system_integration_packet(&self, timeout: Duration) -> bool {
        self.packet_rx
            .lock()
            .expect("lock poisoned")
            .recv_timeout(timeout)
            .is_ok()
    }

    fn random_bytes(&self, len: usize) -> Result<Vec<u8>, String> {
        let mut bytes = vec![0u8; len];
        getrandom::fill(&mut bytes).map_err(|error| error.to_string())?;
        Ok(bytes)
    }

    fn random_number(&self, max: u64) -> Result<u64, String> {
        let mut bytes = [0u8; 8];
        getrandom::fill(&mut bytes).map_err(|error| error.to_string())?;
        Ok(u64::from_le_bytes(bytes) % (max + 1))
    }

    fn set_dns_answer(&self, ip: [u8; 4]) {
        *self.dns_answer.lock().expect("lock poisoned") = Some(ip);
    }

    fn start_dns_lookup(&self, domain: String) {
        let sender = self.dns_result_tx.clone();
        std::thread::spawn(move || {
            let result = match (domain.as_str(), 0u16).to_socket_addrs() {
                Ok(mut addresses) => {
                    let answer = addresses.find_map(|address| match address.ip() {
                        IpAddr::V4(ip) => Some(ip.octets()),
                        IpAddr::V6(_) => None,
                    });
                    DnsResult::Answer(answer)
                }
                Err(error) => DnsResult::LookupError(error.to_string()),
            };
            let _ = sender.send(result);
        });
    }

    fn wait_dns_query(&self, timeout: Duration) -> Option<String> {
        self.domain_rx
            .lock()
            .expect("lock poisoned")
            .recv_timeout(timeout)
            .ok()
    }

    fn wait_dns_result(&self, timeout: Duration) -> Option<DnsResult> {
        self.dns_result_rx
            .lock()
            .expect("lock poisoned")
            .recv_timeout(timeout)
            .ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::notify::system_issue_notification;
    use std::sync::atomic::{AtomicUsize, Ordering};

    /// A scripted probe: each canned response is consumed in order.
    struct Scripted {
        online: bool,
        packet_seen: bool,
        send_error: Option<PacketSendError>,
        random_bytes: Vec<u8>,
        numbers: Vec<u64>,
        number_index: AtomicUsize,
        query: Option<String>,
        result: Option<DnsResult>,
        domain: Mutex<String>,
        answer: Mutex<Option<[u8; 4]>>,
    }

    impl Scripted {
        fn success() -> Scripted {
            let bytes = vec![0xAA; 16];
            let subdomain = format!("a{}b", hex::encode(&bytes));
            Scripted {
                online: true,
                packet_seen: true,
                send_error: None,
                random_bytes: bytes,
                numbers: vec![1, 2, 3],
                number_index: AtomicUsize::new(0),
                query: Some(subdomain),
                result: Some(DnsResult::Answer(Some([127, 1, 2, 3]))),
                domain: Mutex::new(String::new()),
                answer: Mutex::new(None),
            }
        }
    }

    impl Probe for Scripted {
        fn online(&self) -> bool {
            self.online
        }
        fn send_system_integration_packet(&self) -> Result<(), PacketSendError> {
            match &self.send_error {
                Some(error) => Err(error.clone()),
                None => Ok(()),
            }
        }
        fn wait_system_integration_packet(&self, _timeout: Duration) -> bool {
            self.packet_seen
        }
        fn random_bytes(&self, _len: usize) -> Result<Vec<u8>, String> {
            Ok(self.random_bytes.clone())
        }
        fn random_number(&self, _max: u64) -> Result<u64, String> {
            let index = self.number_index.fetch_add(1, Ordering::SeqCst);
            Ok(self.numbers[index])
        }
        fn set_dns_answer(&self, ip: [u8; 4]) {
            *self.answer.lock().unwrap() = Some(ip);
        }
        fn start_dns_lookup(&self, domain: String) {
            *self.domain.lock().unwrap() = domain;
        }
        fn wait_dns_query(&self, _timeout: Duration) -> Option<String> {
            self.query.clone()
        }
        fn wait_dns_result(&self, _timeout: Duration) -> Option<DnsResult> {
            self.result.clone()
        }
    }

    #[test]
    fn offline_is_skipped() {
        let mut probe = Scripted::success();
        probe.online = false;
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.kind, SelfcheckErrorKind::Skipped);
        assert_eq!(
            error.message,
            "self-check skipped: device is offline or in limited network"
        );
        assert!(error.issue.is_none());
    }

    #[test]
    fn create_failure_is_an_internal_error() {
        let mut probe = Scripted::success();
        probe.send_error = Some(PacketSendError::Create("permission denied".into()));
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.kind, SelfcheckErrorKind::Internal);
        assert_eq!(
            error.message,
            "failed to create system integration conn: permission denied"
        );
    }

    #[test]
    fn write_failure_is_an_internal_error() {
        let mut probe = Scripted::success();
        probe.send_error = Some(PacketSendError::Write("broken pipe".into()));
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.message, "failed to send system integration packet: broken pipe");
    }

    #[test]
    fn missing_packet_is_a_system_integration_issue() {
        let mut probe = Scripted::success();
        probe.packet_seen = false;
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.kind, SelfcheckErrorKind::Issue);
        assert_eq!(error.issue, Some(SystemIssueKind::SystemIntegration));
        assert_eq!(
            error.message,
            "self-check #1: system integration check failed: did not receive test packet after 45s"
        );
    }

    #[test]
    fn successful_check_uses_random_subdomain_and_answer() {
        let probe = Scripted::success();
        selfcheck(&probe, &SelfcheckConfig::default()).unwrap();
        let expected = format!(
            "a{}b{DNS_CHECK_INTERNAL_DOMAIN_SCOPE}",
            hex::encode(vec![0xAA; 16])
        );
        assert_eq!(*probe.domain.lock().unwrap(), expected);
        assert_eq!(*probe.answer.lock().unwrap(), Some([127, 1, 2, 3]));
    }

    #[test]
    fn mismatching_query_picks_compatibility_or_manual_dns_issue() {
        let mut probe = Scripted::success();
        probe.query = Some("wrong".to_string());
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.issue, Some(SystemIssueKind::SystemCompatibility));
        assert_eq!(
            error.message,
            "self-check #2: dns integration check failed: received unmatching subdomain \"wrong\""
        );

        let mut probe = Scripted::success();
        probe.query = Some("wrong".to_string());
        let config = SelfcheckConfig {
            dns_query_interception: false,
            ..SelfcheckConfig::default()
        };
        let error = selfcheck(&probe, &config).unwrap_err();
        assert_eq!(error.issue, Some(SystemIssueKind::ManualDnsSetup));
    }

    #[test]
    fn missing_query_timeout_message() {
        let mut probe = Scripted::success();
        probe.query = None;
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(
            error.message,
            "self-check #2: dns integration check failed: did not receive test query after 45s"
        );
    }

    #[test]
    fn resolver_disabled_stops_after_step_two() {
        let mut probe = Scripted::success();
        probe.result = None; // would time out in step 3
        let config = SelfcheckConfig {
            resolver_disabled: true,
            ..SelfcheckConfig::default()
        };
        selfcheck(&probe, &config).unwrap();
    }

    #[test]
    fn step_three_failures_are_compatibility_issues() {
        let mut probe = Scripted::success();
        probe.result = Some(DnsResult::LookupError("no such host".into()));
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(error.issue, Some(SystemIssueKind::SystemCompatibility));
        assert_eq!(
            error.message,
            "self-check #3: dns integration check failed: failed to receive test response: no such host"
        );

        let mut probe = Scripted::success();
        probe.result = None;
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(
            error.message,
            "self-check #3: dns integration check failed: did not receive test response after 45s"
        );

        let mut probe = Scripted::success();
        probe.result = Some(DnsResult::Answer(Some([127, 9, 9, 9])));
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        assert_eq!(
            error.message,
            "self-check #3: dns integration check failed: received unmatching response \"127.9.9.9\""
        );
    }

    #[test]
    fn system_issue_notification_ids_match_failure_issue() {
        let mut probe = Scripted::success();
        probe.packet_seen = false;
        let error = selfcheck(&probe, &SelfcheckConfig::default()).unwrap_err();
        let notification = system_issue_notification(error.issue.unwrap(), "127.0.0.1");
        assert_eq!(notification.event_id, "compat:system-integration-issue");
    }
}
