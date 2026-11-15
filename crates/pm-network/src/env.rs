//! Injected dependencies of the network module.

use std::net::IpAddr;
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use pm_database::Controller;
use pm_intel::Entity;
use pm_process::{
    get_pid_of_connection, process_key, Process, ProcessCache, SpecialProcess, Tag,
    NETWORK_HOST_PROCESS_ID,
};
use pm_resolver::ResolverInfo;

use crate::connection::DnsRequestContext;
use crate::types::PacketInfo;
use pm_netstate::NetState;

/// Source of the current time in Unix seconds.
pub trait Clock: Send + Sync {
    fn now(&self) -> i64;
}

/// The system clock.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> i64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|elapsed| elapsed.as_secs() as i64)
            .unwrap_or(0)
    }
}

/// A clock that only moves when told to, for tests.
#[derive(Debug, Default)]
pub struct ManualClock(AtomicI64);

impl ManualClock {
    pub fn new(now: i64) -> ManualClock {
        ManualClock(AtomicI64::new(now))
    }

    pub fn set(&self, now: i64) {
        self.0.store(now, Ordering::SeqCst);
    }

    pub fn advance(&self, seconds: i64) {
        self.0.fetch_add(seconds, Ordering::SeqCst);
    }
}

impl Clock for ManualClock {
    fn now(&self) -> i64 {
        self.0.load(Ordering::SeqCst)
    }
}

/// Process attribution and socket state, the parts of Go `service/process`
/// and `service/network/state` that connections depend on.
pub trait ProcessResolver: Send + Sync {
    /// Returns the PID and direction of the connection, with the fallbacks of
    /// Go `GetPidOfConnection`.
    fn pid_of_connection(&self, info: &mut PacketInfo) -> (i32, bool);

    /// Returns the process of the PID, or the unidentified process.
    fn process(&self, pid: i32) -> Arc<Process>;

    /// Returns the pseudo process of a device on the network.
    fn network_host(&self, ip: IpAddr) -> Arc<Process>;

    /// Returns whether the socket of the connection still exists.
    fn exists(&self, info: &PacketInfo, now: SystemTime) -> bool;

    /// Returns a process from the process storage by its process key.
    fn process_from_storage(&self, key: &str) -> Option<Arc<Process>>;

    /// Returns all known processes.
    fn all_processes(&self) -> Vec<Arc<Process>>;

    /// Sets the database controller that receives process updates.
    fn set_db_controller(&self, _controller: Arc<Controller>) {}
}

/// [`ProcessResolver`] backed by the process cache and the socket tables.
pub struct SystemProcessResolver {
    cache: Arc<ProcessCache>,
    state: Arc<NetState>,
}

impl SystemProcessResolver {
    pub fn new(cache: Arc<ProcessCache>, state: Arc<NetState>) -> SystemProcessResolver {
        SystemProcessResolver { cache, state }
    }
}

impl ProcessResolver for SystemProcessResolver {
    fn pid_of_connection(&self, info: &mut PacketInfo) -> (i32, bool) {
        let mut ns_info = info.to_netstate();
        let (pid, inbound, _error) =
            get_pid_of_connection(&self.cache, &self.state, &mut ns_info, false);
        (pid, inbound)
    }

    fn process(&self, pid: i32) -> Arc<Process> {
        if !self.cache.enabled() {
            return self.cache.special(SpecialProcess::Unidentified);
        }
        match self.cache.get_or_find(pid) {
            Ok(process) => process,
            Err(error) => {
                tracing::debug!(pid, %error, "network: failed to get process");
                self.cache.special(SpecialProcess::Unidentified)
            }
        }
    }

    fn network_host(&self, ip: IpAddr) -> Arc<Process> {
        let now = SystemClock.now();
        let mut process = Process::empty(&process_key(NETWORK_HOST_PROCESS_ID, 0));
        process.name = format!("Device at {ip}");
        process.user_name = "N/A".to_string();
        process.user_id = NETWORK_HOST_PROCESS_ID;
        process.pid = NETWORK_HOST_PROCESS_ID;
        process.parent_pid = NETWORK_HOST_PROCESS_ID;
        process.tags = vec![Tag {
            key: "ip".to_string(),
            value: ip.to_string(),
        }];
        process.first_seen = now;
        process.last_seen = now;
        Arc::new(process)
    }

    fn exists(&self, info: &PacketInfo, now: SystemTime) -> bool {
        self.state.exists(&info.to_netstate(), now)
    }

    fn process_from_storage(&self, key: &str) -> Option<Arc<Process>> {
        self.cache.get_from_storage(key)
    }

    fn all_processes(&self) -> Vec<Arc<Process>> {
        self.cache.all().into_values().collect()
    }

    fn set_db_controller(&self, controller: Arc<Controller>) {
        self.cache.set_db_controller(controller);
    }
}

/// The local profile of a process, as far as connections need it.
#[derive(Debug, Clone, Default)]
pub struct LocalProfileInfo {
    pub id: String,
    pub name: String,
    pub source: String,
    pub internal: bool,
    pub enable_history: bool,
    /// Revision counter of the layered profile.
    pub revision_cnt: u64,
}

/// Profile access for connections. Profiles are not part of the process type
/// yet, so the firewall integration supplies them.
pub trait ProfileLookup: Send + Sync {
    /// Returns the local profile of the process.
    fn local_profile(&self, process: &Process) -> Option<LocalProfileInfo>;

    /// Returns the source (profile ID) of the setting with the given option
    /// key, or `None` if the process has no layered profile.
    fn profile_source(&self, process: &Process, option_key: &str) -> Option<String>;
}

/// A [`ProfileLookup`] without any profiles.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoProfiles;

impl ProfileLookup for NoProfiles {
    fn local_profile(&self, _process: &Process) -> Option<LocalProfileInfo> {
        None
    }

    fn profile_source(&self, _process: &Process, _option_key: &str) -> Option<String> {
        None
    }
}

/// A domain that was resolved to an IP, as remembered by the resolver.
#[derive(Debug, Clone, Default)]
pub struct ResolvedDomain {
    pub domain: String,
    pub cnames: Vec<String>,
    pub dns_context: Option<DnsRequestContext>,
    pub resolver: Option<ResolverInfo>,
}

/// Profile scope for IP info that was resolved through the system resolver.
pub const IP_INFO_PROFILE_SCOPE_GLOBAL: &str = "global";

/// No IP info exists for the IP in the profile scope (Go `GetIPInfo` error).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct IpInfoNotFound;

/// Lookup of the most recent domain that resolved to an IP.
pub trait DomainLookup: Send + Sync {
    /// Returns `Err` if no IP info exists in the scope, and `Ok(None)` if info
    /// exists but has no domain. Only the error triggers the global fallback.
    fn most_recent_domain(
        &self,
        profile_scope: &str,
        ip: IpAddr,
    ) -> Result<Option<ResolvedDomain>, IpInfoNotFound>;
}

/// A [`DomainLookup`] that never finds anything.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoDomains;

impl DomainLookup for NoDomains {
    fn most_recent_domain(
        &self,
        _profile_scope: &str,
        _ip: IpAddr,
    ) -> Result<Option<ResolvedDomain>, IpInfoNotFound> {
        Err(IpInfoNotFound)
    }
}

/// The captive portal domain and IP.
#[derive(Debug, Clone)]
pub struct CaptivePortal {
    pub ip: IpAddr,
    pub domain: String,
}

/// Creates a fresh remote entity.
pub type EntityFactory = Arc<dyn Fn() -> Entity + Send + Sync>;
/// Returns the captive portal, if one is known.
pub type CaptivePortalGetter = Arc<dyn Fn() -> Option<CaptivePortal> + Send + Sync>;

/// The dependencies of the network module.
#[derive(Clone)]
pub struct Environment {
    pub clock: Arc<dyn Clock>,
    pub processes: Arc<dyn ProcessResolver>,
    pub profiles: Arc<dyn ProfileLookup>,
    pub domains: Arc<dyn DomainLookup>,
    pub new_entity: EntityFactory,
    pub captive_portal: CaptivePortalGetter,
}

impl Environment {
    /// Creates an environment with the system clock and no profiles, domains
    /// or captive portal.
    pub fn new(processes: Arc<dyn ProcessResolver>, new_entity: EntityFactory) -> Environment {
        Environment {
            clock: Arc::new(SystemClock),
            processes,
            profiles: Arc::new(NoProfiles),
            domains: Arc::new(NoDomains),
            new_entity,
            captive_portal: Arc::new(|| None),
        }
    }

    #[must_use]
    pub fn with_clock(mut self, clock: Arc<dyn Clock>) -> Environment {
        self.clock = clock;
        self
    }

    #[must_use]
    pub fn with_profiles(mut self, profiles: Arc<dyn ProfileLookup>) -> Environment {
        self.profiles = profiles;
        self
    }

    #[must_use]
    pub fn with_domains(mut self, domains: Arc<dyn DomainLookup>) -> Environment {
        self.domains = domains;
        self
    }

    #[must_use]
    pub fn with_captive_portal(mut self, getter: CaptivePortalGetter) -> Environment {
        self.captive_portal = getter;
        self
    }
}
