//! Tests for connection prompts (Go `firewall/prompt.go`).

use std::net::IpAddr;
use std::sync::{Arc, Mutex};

use pm_firewall::{
    create_prompt, endpoint_rule, prompt, PromptEnv, PromptOutcome, PromptProfile,
    PromptTarget, ALLOW_DOMAIN_ALL, ALLOW_IP, ALLOW_SERVING_IP, BLOCK_DOMAIN_DISTINCT, BLOCK_IP,
    BLOCK_SERVING_IP, CANCEL_PROMPT, PROMPT_ID_PREFIX,
};
use pm_intel::{Entity, FilterLists, StaticLocations};
use pm_network::{protocol, Connection, Verdict};
use pm_notifications::{Notifications, STATE_ACTIVE};
use tokio_util::sync::CancellationToken;

fn lists() -> Arc<FilterLists> {
    let databases = Arc::new(pm_database::Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register");
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .expect("inject");
    Arc::new(FilterLists::with_capacity(
        pm_intel::cache_interface(&databases),
        10,
        10,
        1,
        1,
        1,
    ))
}

fn connection(inbound: bool, domain: &str, ip: &str) -> Connection {
    let mut entity = Entity::new(lists(), Arc::new(StaticLocations::new()));
    entity.ip = Some(ip.parse().unwrap());
    entity.domain = domain.to_string();
    entity.protocol = protocol::TCP;
    entity.port = 443;
    entity.init(443);

    Connection {
        inbound,
        ip_protocol: protocol::TCP,
        pid: 100,
        entity: Some(entity),
        process: Some(Arc::new(pm_process::Process::empty("100-0"))),
        ..Connection::default()
    }
}

type SavedRule = (String, String, Option<IpAddr>, String);

struct FakeEnv {
    notifications: Arc<Notifications>,
    profile: Option<PromptProfile>,
    timeout: i64,
    system_notifications: bool,
    saved: Mutex<Vec<SavedRule>>,
}

impl FakeEnv {
    fn new(profile: Option<PromptProfile>) -> Arc<FakeEnv> {
        Arc::new(FakeEnv {
            notifications: Arc::new(Notifications::new()),
            profile,
            timeout: 60,
            system_notifications: true,
            saved: Mutex::new(Vec::new()),
        })
    }
}

impl PromptEnv for FakeEnv {
    fn ask_timeout(&self) -> i64 {
        self.timeout
    }

    fn ask_with_system_notifications(&self) -> bool {
        self.system_notifications
    }

    fn notifications(&self) -> Arc<Notifications> {
        Arc::clone(&self.notifications)
    }

    fn local_profile(&self, _conn: &Connection) -> Option<PromptProfile> {
        self.profile.clone()
    }

    fn save_response(
        &self,
        profile_id: &str,
        domain: &str,
        ip: Option<IpAddr>,
        action: &str,
    ) -> Result<(), String> {
        self.saved.lock().expect("lock").push((
            profile_id.to_string(),
            domain.to_string(),
            ip,
            action.to_string(),
        ));
        Ok(())
    }
}

fn as_env(env: &Arc<FakeEnv>) -> Arc<dyn PromptEnv> {
    let env: Arc<dyn PromptEnv> = env.clone();
    env
}

fn profile() -> PromptProfile {
    PromptProfile {
        source: "local".to_string(),
        id: "profile-1".to_string(),
        name: "Firefox".to_string(),
        linked_path: "/usr/bin/firefox".to_string(),
    }
}

#[test]
fn endpoint_rules_match_go() {
    let domain = "example.com";
    let ip = Some("1.2.3.4".parse::<IpAddr>().unwrap());

    assert_eq!(
        endpoint_rule(domain, ip, ALLOW_DOMAIN_ALL).unwrap(),
        ("+ .example.com".to_string(), false)
    );
    assert_eq!(
        endpoint_rule(domain, ip, "allow-domain-distinct").unwrap(),
        ("+ example.com".to_string(), false)
    );
    assert_eq!(
        endpoint_rule(domain, ip, BLOCK_DOMAIN_DISTINCT).unwrap(),
        ("- example.com".to_string(), false)
    );
    assert_eq!(
        endpoint_rule(domain, ip, ALLOW_IP).unwrap(),
        ("+ 1.2.3.4".to_string(), false)
    );
    assert_eq!(
        endpoint_rule(domain, ip, ALLOW_SERVING_IP).unwrap(),
        ("+ 1.2.3.4".to_string(), true)
    );
    assert_eq!(
        endpoint_rule(domain, ip, BLOCK_SERVING_IP).unwrap(),
        ("- 1.2.3.4".to_string(), true)
    );
    assert_eq!(
        endpoint_rule(domain, ip, BLOCK_IP).unwrap(),
        ("- 1.2.3.4".to_string(), false)
    );
    assert_eq!(
        endpoint_rule(domain, ip, "nope").unwrap_err(),
        "unknown prompt response: nope"
    );
}

#[test]
fn creates_a_domain_prompt() {
    let env = FakeEnv::new(Some(profile()));
    let mut conn = connection(false, "example.com", "1.1.1.1");

    let handle = create_prompt(&mut conn, as_env(&env)).expect("prompt");
    let notification = handle.snapshot();

    assert_eq!(
        notification.event_id,
        format!("{PROMPT_ID_PREFIX}-profile-1-example.com")
    );
    // Go `notifications.Prompt`.
    assert_eq!(notification.type_, pm_notifications::TYPE_PROMPT);
    assert_eq!(notification.title, "Connection Prompt");
    assert_eq!(notification.category, "Privacy Filter");
    assert!(notification.show_on_system);
    assert_eq!(
        notification.message,
        "Firefox wants to connect to example.com"
    );
    assert_eq!(
        notification
            .available_actions
            .iter()
            .map(|action| (action.id.as_str(), action.text.as_str()))
            .collect::<Vec<_>>(),
        vec![(ALLOW_DOMAIN_ALL, "Allow"), ("block-domain-all", "Block")]
    );

    // Event data carries the entity and the profile.
    assert_eq!(notification.event_data["Entity"]["IP"], "1.1.1.1");
    assert_eq!(notification.event_data["Entity"]["Domain"], "example.com");
    assert_eq!(notification.event_data["Profile"]["Source"], "local");
    assert_eq!(notification.event_data["Profile"]["ID"], "profile-1");
    assert_eq!(
        notification.event_data["Profile"]["LinkedPath"],
        "/usr/bin/firefox"
    );
}

#[test]
fn creates_ip_and_inbound_prompts() {
    let env = FakeEnv::new(Some(profile()));

    let mut direct = connection(false, "", "1.1.1.1");
    let handle = create_prompt(&mut direct, as_env(&env)).expect("direct");
    let notification = handle.snapshot();
    assert_eq!(
        notification.event_id,
        format!("{PROMPT_ID_PREFIX}-profile-1-false-1.1.1.1")
    );
    assert_eq!(
        notification.message,
        "Firefox wants to connect to 1.1.1.1 (6/443)"
    );
    assert_eq!(notification.available_actions[0].id, ALLOW_IP);

    let mut inbound = connection(true, "example.com", "1.1.1.1");
    let handle = create_prompt(&mut inbound, as_env(&env)).expect("inbound");
    let notification = handle.snapshot();
    assert_eq!(
        notification.message,
        "Firefox wants to accept connections from 1.1.1.1 (6/443)"
    );
    assert_eq!(notification.available_actions[0].id, ALLOW_SERVING_IP);
}

#[test]
fn active_prompt_is_reused_and_expiry_extended() {
    let env = FakeEnv::new(Some(profile()));
    let mut first = connection(false, "example.com", "1.1.1.1");
    let handle = create_prompt(&mut first, as_env(&env)).expect("first");
    let first_guid = handle.snapshot().guid;

    let mut second = connection(false, "example.com", "1.1.1.1");
    let reused = create_prompt(&mut second, as_env(&env)).expect("second");
    assert_eq!(reused.snapshot().guid, first_guid);
    assert_eq!(
        reused.snapshot().state,
        STATE_ACTIVE,
        "still active after the update"
    );
}

#[test]
fn answered_prompt_applies_the_decision_directly() {
    let env = FakeEnv::new(Some(profile()));
    let mut first = connection(false, "example.com", "1.1.1.1");
    create_prompt(&mut first, as_env(&env)).expect("first");
    let event_id = format!("{PROMPT_ID_PREFIX}-profile-1-example.com");
    assert!(env.notifications.select_action(&event_id, "block-domain-all"));

    let mut second = connection(false, "example.com", "1.1.1.1");
    assert!(create_prompt(&mut second, as_env(&env)).is_none());
    assert_eq!(second.verdict, Verdict::Block);
    assert!(second.prompt.is_none());
}

#[test]
fn connection_without_profile_has_no_prompt() {
    let env = FakeEnv::new(None);
    let mut conn = connection(false, "example.com", "1.1.1.1");
    assert!(create_prompt(&mut conn, env).is_none());
}

#[test]
fn missing_entity_has_no_target() {
    assert!(PromptTarget::from_connection(&Connection::default()).is_none());
}

#[tokio::test]
async fn answered_prompt_allows_the_connection() {
    let env = FakeEnv::new(Some(profile()));
    let mut conn = connection(false, "example.com", "1.1.1.1");
    let handle = create_prompt(&mut conn, as_env(&env)).expect("prompt");
    let event_id = handle.snapshot().event_id;

    // Answer while the prompt waits.
    let notifications = Arc::clone(&env.notifications);
    let responder = tokio::spawn(async move {
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;
        notifications.select_action(&event_id, ALLOW_DOMAIN_ALL)
    });

    let outcome = prompt(&mut conn, as_env(&env), &CancellationToken::new()).await;
    assert!(responder.await.expect("responder"));
    assert_eq!(outcome, PromptOutcome::Allowed);
    assert_eq!(conn.verdict, Verdict::Accept);
    assert_eq!(conn.reason.msg, "allowed via prompt");

    // The action handler saved the rule.
    let saved = env.saved.lock().expect("lock").clone();
    assert_eq!(
        saved,
        vec![(
            "profile-1".to_string(),
            "example.com".to_string(),
            Some("1.1.1.1".parse().unwrap()),
            ALLOW_DOMAIN_ALL.to_string()
        )]
    );
}

#[tokio::test]
async fn dismissed_prompt_waits_for_a_new_decision() {
    let env = FakeEnv::new(Some(profile()));
    let mut conn = connection(false, "example.com", "1.1.1.1");
    let handle = create_prompt(&mut conn, as_env(&env)).expect("prompt");
    let event_id = handle.snapshot().event_id;

    let notifications = Arc::clone(&env.notifications);
    tokio::spawn(async move {
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;
        notifications.select_action(&event_id, "");
    });

    let outcome = prompt(&mut conn, as_env(&env), &CancellationToken::new()).await;
    assert_eq!(outcome, PromptOutcome::Dismissed);
    assert_eq!(conn.verdict, Verdict::Block);
    assert_eq!(
        conn.reason.msg,
        "prompting canceled, waiting for new decision"
    );
}

#[tokio::test]
async fn timeout_denies_and_keeps_the_prompt_open() {
    let mut env = FakeEnv::new(Some(profile()));
    // The decision timeout is capped by the ask timeout.
    Arc::get_mut(&mut env).expect("unique env").timeout = 1;
    let mut conn = connection(false, "example.com", "1.1.1.1");

    let outcome = prompt(&mut conn, as_env(&env), &CancellationToken::new()).await;
    assert_eq!(outcome, PromptOutcome::Timeout);
    assert_eq!(conn.verdict, Verdict::Block);
    assert_eq!(
        conn.reason.msg,
        "prompting in progress, please respond to prompt"
    );

    let notification = env
        .notifications
        .get(&format!("{PROMPT_ID_PREFIX}-profile-1-example.com"))
        .expect("notification")
        .snapshot();
    assert_eq!(notification.state, STATE_ACTIVE);
}

#[tokio::test]
async fn shutdown_drops_the_connection() {
    let env = FakeEnv::new(Some(profile()));
    let mut conn = connection(false, "example.com", "1.1.1.1");
    let token = CancellationToken::new();
    token.cancel();

    let outcome = prompt(&mut conn, as_env(&env), &token).await;
    assert_eq!(outcome, PromptOutcome::Shutdown);
    assert_eq!(conn.verdict, Verdict::Drop);
    assert_eq!(conn.reason.msg, "shutting down");
}

#[test]
fn cancel_is_not_a_decision() {
    let mut conn = connection(false, "example.com", "1.1.1.1");
    let env = FakeEnv::new(Some(profile()));
    let handle = create_prompt(&mut conn, as_env(&env)).expect("prompt");
    let event_id = handle.snapshot().event_id;

    assert!(env.notifications.select_action(&event_id, CANCEL_PROMPT));
    // The handler ignores the cancel action.
    assert!(env.saved.lock().expect("lock").is_empty());
}
