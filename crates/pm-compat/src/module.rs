//! The compatibility module: periodic self-check, notification state and the
//! hooks called by the firewall. Port of Go `service/compat/module.go`,
//! `notify.go` and `callbacks.go`.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use pm_mgr::{State, StateMgr, STATE_TYPE_ERROR};
use pm_netenv::NetworkChangeFlag;
use pm_notifications::Notifications;

use crate::notify::{
    app_issue_notification, clean_notify_thresholds, is_over_threshold, AppIssueKind,
    NotifyThreshold, SystemIssueKind,
};
use crate::selfcheck::{
    selfcheck, RealProbe, SelfcheckConfig, SelfcheckError, SelfcheckErrorKind,
};

/// Delay before the first and each retry self-check (Go `selfcheckTaskRetryAfter`).
pub const SELF_CHECK_RETRY_AFTER: Duration = Duration::from_secs(15);
/// Interval between self-checks (Go `Repeat(5 * time.Minute)`).
pub const SELF_CHECK_REPEAT: Duration = Duration::from_secs(5 * 60);
/// Interval for cleaning notification thresholds (Go `Repeat(1 * time.Hour)`).
pub const CLEAN_NOTIFY_THRESHOLD_REPEAT: Duration = Duration::from_secs(60 * 60);
/// How often the self-check must fail before a notification is sent.
pub const SELF_CHECK_FAIL_THRESHOLD: u32 = 10;

/// Profile ID of unidentified processes (`pm_profile::UNIDENTIFIED_PROFILE_ID`).
const UNIDENTIFIED_PROFILE_ID: &str = "_unidentified";
/// State ID published while a self-check issue is active.
pub const COMPAT_SELF_CHECK_STATE_ID: &str = "compat:self-check";

/// Callback that stores a warning on a local profile (Go `p.Warning = ...`).
pub type SetAppWarning = Arc<dyn Fn(&str, &str) + Send + Sync>;

/// Dependencies of [`Compat`], all injected by the core.
pub struct CompatDeps {
    pub notifications: Arc<Notifications>,
    pub probe: Arc<RealProbe>,
    pub states: Arc<StateMgr>,
    pub net_changed: NetworkChangeFlag,
    /// Initial value of `filter/dnsQueryInterception`.
    pub dns_query_interception: bool,
    /// IP the nameserver listens on, used in the manual-DNS notification.
    pub nameserver_listen_ip: String,
    /// Whether the resolver is disabled (step 3 of the self-check).
    pub resolver_disabled: bool,
    /// Optional profile-warning setter, used for app issues.
    pub set_app_warning: Option<SetAppWarning>,
}

struct CompatState {
    selfcheck_failing: bool,
    selfcheck_fails: u32,
    system_notification_id: Option<String>,
    notify_thresholds: HashMap<String, NotifyThreshold>,
    dns_query_interception: bool,
    nameserver_listen_ip: String,
    resolver_disabled: bool,
}

/// The compatibility module. Cheap to clone through `Arc`; all mutable state
/// is behind a mutex.
pub struct Compat {
    deps: CompatDeps,
    state: Mutex<CompatState>,
}

impl Compat {
    pub fn new(deps: CompatDeps) -> Arc<Compat> {
        let state = CompatState {
            selfcheck_failing: false,
            selfcheck_fails: 0,
            system_notification_id: None,
            notify_thresholds: HashMap::new(),
            dns_query_interception: deps.dns_query_interception,
            nameserver_listen_ip: deps.nameserver_listen_ip.clone(),
            resolver_disabled: deps.resolver_disabled,
        };
        Arc::new(Compat {
            deps,
            state: Mutex::new(state),
        })
    }

    /// The probe backing the firewall hooks.
    pub fn probe(&self) -> &Arc<RealProbe> {
        &self.deps.probe
    }

    /// The module state manager, exposed so the core can forward states to
    /// `pm_status` (Go `SyncWithState`).
    pub fn states(&self) -> &Arc<StateMgr> {
        &self.deps.states
    }

    pub fn net_changed_flag(&self) -> &NetworkChangeFlag {
        &self.deps.net_changed
    }

    /// Go `SelfCheckIsFailing`: true after the first failure, before the
    /// notification threshold is reached.
    pub fn self_check_is_failing(&self) -> bool {
        self.state
            .lock()
            .expect("lock poisoned")
            .selfcheck_failing
    }

    /// Refreshes options read from the config at runtime.
    pub fn refresh_options(&self, dns_query_interception: bool, resolver_disabled: bool) {
        let mut state = self.state.lock().expect("lock poisoned");
        state.dns_query_interception = dns_query_interception;
        state.resolver_disabled = resolver_disabled;
    }

    /// Go `SetNameserverListenIP`, used in the manual-DNS notification.
    pub fn set_nameserver_listen_ip(&self, ip: String) {
        self.state.lock().expect("lock poisoned").nameserver_listen_ip = ip;
    }

    fn config(&self) -> SelfcheckConfig {
        let state = self.state.lock().expect("lock poisoned");
        SelfcheckConfig {
            dns_query_interception: state.dns_query_interception,
            resolver_disabled: state.resolver_disabled,
            ..SelfcheckConfig::default()
        }
    }

    /// Runs one self-check, used by the API endpoint and the periodic worker.
    pub fn run_selfcheck(&self) -> Result<(), SelfcheckError> {
        selfcheck(self.deps.probe.as_ref(), &self.config())
    }

    /// Go `selfcheckTaskFunc`: runs a check and updates state/notifications.
    /// Safe to call without root: a privileged failure is an internal error and
    /// never produces a notification.
    pub fn periodic_selfcheck(&self) {
        let result = self.run_selfcheck();
        let network_changed = self.deps.net_changed.is_set();

        match result {
            Ok(()) => {
                tracing::debug!("compat: self-check successful");
                self.reset_selfcheck_state();
            }
            Err(error) => match error.kind {
                SelfcheckErrorKind::Skipped => {
                    tracing::debug!("compat: {}", error.message);
                    self.reset_selfcheck_state();
                }
                SelfcheckErrorKind::Internal => {
                    tracing::warn!("compat: {}", error.message);
                    self.reset_selfcheck_state();
                }
                SelfcheckErrorKind::Issue => {
                    if network_changed {
                        // The network changed, ignore the issue like Go.
                        self.reset_selfcheck_state();
                        return;
                    }

                    let issue = error
                        .issue
                        .expect("issue-kind self-check error always has an issue");
                    let fails = {
                        let mut state = self.state.lock().expect("lock poisoned");
                        state.selfcheck_failing = true;
                        state.selfcheck_fails += 1;
                        state.selfcheck_fails
                    };

                    tracing::error!("compat: {}", error.message);
                    self.set_compat_state(issue, &error.message);
                    if fails >= SELF_CHECK_FAIL_THRESHOLD {
                        self.notify_system_issue(issue);
                    }
                }
            },
        }
    }

    fn set_compat_state(&self, issue: SystemIssueKind, message: &str) {
        let state = State::new(COMPAT_SELF_CHECK_STATE_ID, issue.title(), STATE_TYPE_ERROR)
            .with_message(message);
        self.deps.states.add(state);
    }

    fn reset_selfcheck_state(&self) {
        self.deps.net_changed.refresh();
        {
            let mut state = self.state.lock().expect("lock poisoned");
            state.selfcheck_failing = false;
            state.selfcheck_fails = 0;
        }
        self.deps.states.clear();
        self.reset_system_issue();
    }

    /// Go `systemIssue.notify`: deduplicates by event ID and replaces an older
    /// system notification.
    fn notify_system_issue(&self, issue: SystemIssueKind) {
        let mut state = self.state.lock().expect("lock poisoned");
        if let Some(active) = &state.system_notification_id {
            if active == issue.id() {
                return;
            }
            self.deps.notifications.delete(active);
        }
        let notification =
            crate::notify::system_issue_notification(issue, &state.nameserver_listen_ip);
        self.deps.notifications.notify(notification);
        state.system_notification_id = Some(issue.id().to_string());
    }

    fn reset_system_issue(&self) {
        let mut state = self.state.lock().expect("lock poisoned");
        if let Some(active) = state.system_notification_id.take() {
            self.deps.notifications.delete(&active);
        }
    }

    /// Go `cleanNotifyThreshold`.
    pub fn clean_notify_thresholds(&self) {
        let mut state = self.state.lock().expect("lock poisoned");
        clean_notify_thresholds(&mut state.notify_thresholds, Instant::now());
    }

    /// Go `appIssue.notify`, minus the process/profile lookup which the core
    /// performs before calling this method.
    pub fn report_app_issue(
        &self,
        kind: AppIssueKind,
        profile_id: &str,
        app_name: &str,
        process_path: &str,
    ) {
        if profile_id == UNIDENTIFIED_PROFILE_ID {
            return;
        }

        tracing::warn!(
            "compat: detected {} issue with {}",
            kind.display_name(),
            process_path
        );

        let event_id = kind.event_id(profile_id);
        if self.deps.notifications.get(&event_id).is_some() {
            return;
        }
        {
            let mut state = self.state.lock().expect("lock poisoned");
            if !is_over_threshold(&mut state.notify_thresholds, &event_id, Instant::now()) {
                return;
            }
        }

        let message = kind.message(app_name);
        self.deps
            .notifications
            .notify(app_issue_notification(kind, profile_id, app_name));

        if let Some(set_warning) = &self.deps.set_app_warning {
            set_warning(profile_id, &message);
        }
    }

    // -- Hooks called by the firewall (Go callbacks.go) --------------------

    /// Go `compat.SubmitSystemIntegrationCheckPacket`.
    pub fn submit_system_integration_check_packet(&self) {
        self.deps.probe.submit_system_integration_check_packet();
    }

    /// Go `compat.SubmitDNSCheckDomain`.
    pub fn submit_dns_check_domain(&self, subdomain: &str) {
        let _ = self.deps.probe.submit_dns_check_domain(subdomain);
    }

    /// Go `compat.ReportSecureDNSBypassIssue`.
    pub fn report_secure_dns_bypass(&self, profile_id: &str, app_name: &str, path: &str) {
        self.report_app_issue(AppIssueKind::SecureDnsBypass, profile_id, app_name, path);
    }

    /// Go `compat.ReportMultiPeerUDPTunnelIssue`.
    pub fn report_multi_peer_udp_tunnel(&self, profile_id: &str, app_name: &str, path: &str) {
        self.report_app_issue(
            AppIssueKind::MultiPeerUdpTunnel,
            profile_id,
            app_name,
            path,
        );
    }

    /// Stops the module, like Go `stop`: clears the failure state and issues.
    pub fn stop(&self) {
        self.reset_selfcheck_state();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pm_netenv::NetworkChangeFlag;
    use std::sync::atomic::{AtomicUsize, Ordering};

    struct Fixture {
        compat: Arc<Compat>,
        notifications: Arc<Notifications>,
        warnings: Arc<AtomicUsize>,
    }

    fn fixture() -> Fixture {
        let notifications = Arc::new(Notifications::new());
        let warnings = Arc::new(AtomicUsize::new(0));
        let warnings_cb = Arc::clone(&warnings);
        let compat = Compat::new(CompatDeps {
            notifications: Arc::clone(&notifications),
            probe: Arc::new(RealProbe::new()),
            states: Arc::new(StateMgr::new("Compat")),
            net_changed: NetworkChangeFlag::standalone(),
            dns_query_interception: true,
            nameserver_listen_ip: "127.0.0.1".to_string(),
            resolver_disabled: false,
            set_app_warning: Some(Arc::new(move |_, _| {
                warnings_cb.fetch_add(1, Ordering::SeqCst);
            })),
        });
        Fixture {
            compat,
            notifications,
            warnings,
        }
    }

    #[test]
    fn self_check_is_failing_tracks_state() {
        let f = fixture();
        assert!(!f.compat.self_check_is_failing());
        {
            let mut state = f.compat.state.lock().unwrap();
            state.selfcheck_failing = true;
        }
        assert!(f.compat.self_check_is_failing());
        f.compat.stop();
        assert!(!f.compat.self_check_is_failing());
    }

    #[test]
    fn system_issue_notification_is_deduplicated_and_replaced() {
        let f = fixture();
        f.compat.notify_system_issue(SystemIssueKind::SystemIntegration);
        assert!(f
            .notifications
            .get("compat:system-integration-issue")
            .is_some());
        // Same issue again: no change, still one active notification.
        f.compat.notify_system_issue(SystemIssueKind::SystemIntegration);
        assert_eq!(f.notifications.all().len(), 1);

        // A different issue replaces the old one.
        f.compat
            .notify_system_issue(SystemIssueKind::SystemCompatibility);
        assert!(f.notifications.get("compat:compatibility-issue").is_some());
        assert!(f
            .notifications
            .get("compat:system-integration-issue")
            .is_none());
        assert_eq!(f.notifications.all().len(), 1);

        f.compat.reset_system_issue();
        assert!(f.notifications.all().is_empty());
    }

    #[test]
    fn app_issue_skips_unidentified_profiles() {
        let f = fixture();
        f.compat
            .report_app_issue(AppIssueKind::SecureDnsBypass, "_unidentified", "App", "/bin/app");
        assert!(f.notifications.all().is_empty());
    }

    #[test]
    fn app_issue_needs_ten_incidents_and_sets_a_warning() {
        let f = fixture();
        for _ in 0..crate::notify::NOTIFY_THRESHOLD_MIN_INCIDENTS {
            f.compat
                .report_app_issue(AppIssueKind::SecureDnsBypass, "local/app", "Firefox", "/bin/ff");
        }
        let handle = f
            .notifications
            .get("compat:secure-dns-bypass-local/app")
            .expect("notification sent after threshold");
        assert_eq!(handle.snapshot().title, "Blocked Bypass Attempt by Firefox");
        assert_eq!(f.warnings.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn refresh_options_and_nameserver_ip_are_applied() {
        let f = fixture();
        f.compat.refresh_options(false, true);
        f.compat
            .set_nameserver_listen_ip("127.0.0.99".to_string());
        let config = f.compat.config();
        assert!(!config.dns_query_interception);
        assert!(config.resolver_disabled);
        let ip = f.compat.state.lock().unwrap().nameserver_listen_ip.clone();
        assert_eq!(ip, "127.0.0.99");
    }
}
