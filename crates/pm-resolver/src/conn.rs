use std::str::FromStr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use hickory_proto::op::{Message, MessageType, OpCode, Query as HickoryQuery};
use hickory_proto::rr::{Name, RecordType};

use crate::error::ResolveError;

/// Describes a DNS query to be sent to a resolver.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Query {
    pub fqdn: String,
    pub qtype: RecordType,
    pub no_caching: bool,
    pub ignore_failing: bool,
    pub local_resolvers_only: bool,
}

/// Returns a random 16-bit integer for DNS message IDs.
pub fn random_u16() -> u16 {
    let mut b = [0u8; 2];
    let _ = getrandom::fill(&mut b);
    u16::from_ne_bytes(b)
}

impl Query {
    /// Creates a new query for an A record.
    pub fn a(fqdn: impl Into<String>) -> Self {
        Self::new(fqdn, RecordType::A)
    }

    /// Creates a new query for an AAAA record.
    pub fn aaaa(fqdn: impl Into<String>) -> Self {
        Self::new(fqdn, RecordType::AAAA)
    }

    /// Creates a new query for the given FQDN and RecordType.
    pub fn new(fqdn: impl Into<String>, qtype: RecordType) -> Self {
        let mut domain = fqdn.into();
        if !domain.ends_with('.') {
            domain.push('.');
        }
        Self {
            fqdn: domain,
            qtype,
            no_caching: false,
            ignore_failing: false,
            local_resolvers_only: false,
        }
    }

    /// Converts this query into a wire-ready `Message` with recursion desired.
    pub fn to_message(&self, id: u16) -> Result<Message, ResolveError> {
        let name = Name::from_str(&self.fqdn).map_err(|e| ResolveError::Protocol(e.to_string()))?;
        let mut msg = Message::new(id, MessageType::Query, OpCode::Query);
        msg.metadata.recursion_desired = true;
        let hickory_query = HickoryQuery::query(name, self.qtype);
        msg.add_query(hickory_query);
        Ok(msg)
    }
}

/// Common asynchronous trait for resolver connection backends.
/// Mirrors Go `ResolverConn`.
#[async_trait::async_trait]
pub trait ResolverConn: Send + Sync {
    /// Executes the given query against the upstream resolver.
    async fn query(&self, q: Query) -> Result<Message, ResolveError>;

    /// Reports that an error occurred with this resolver.
    fn report_failure(&self);

    /// Reports if this resolver is currently failing.
    fn is_failing(&self) -> bool;

    /// Resets the failure status.
    fn reset_failure(&self);

    /// Forces the resolver to re-establish the connection to the server.
    async fn force_reconnect(&self);
}

/// Implements failure tracking for resolver connections matching Go `BasicResolverConn`.
#[derive(Debug, Default)]
pub struct BasicResolverConn {
    failing: AtomicBool,
    fail_state: Mutex<FailState>,
}

#[derive(Debug, Default)]
struct FailState {
    fails: usize,
    failing_started: Option<Instant>,
}

impl BasicResolverConn {
    /// FailThreshold is the number of errors a resolver must experience to be marked as failed.
    pub const FAIL_THRESHOLD: usize = 5;

    /// FailObserveDuration is the duration in which failures are counted.
    pub const FAIL_OBSERVE_DURATION: Duration = Duration::from_secs(50);

    pub fn new() -> Self {
        Self::default()
    }

    pub fn is_failing(&self) -> bool {
        self.failing.load(Ordering::Relaxed)
    }

    pub fn report_failure(&self) {
        if self.is_failing() {
            return;
        }

        let mut state = self.fail_state.lock().unwrap();
        let now = Instant::now();

        if let Some(started) = state.failing_started {
            if now.duration_since(started) > Self::FAIL_OBSERVE_DURATION {
                state.fails = 1;
                state.failing_started = Some(now);
                return;
            }
        } else {
            state.fails = 1;
            state.failing_started = Some(now);
            return;
        }

        state.fails += 1;
        if state.fails > Self::FAIL_THRESHOLD {
            self.failing.store(true, Ordering::Relaxed);
        }
    }

    pub fn reset_failure(&self) {
        if self.failing.swap(false, Ordering::Relaxed) {
            let mut state = self.fail_state.lock().unwrap();
            state.fails = 0;
            state.failing_started = None;
        }
    }
}
