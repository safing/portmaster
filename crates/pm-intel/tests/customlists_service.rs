//! Tests for the custom filter list service (Go `customlists/module.go`).

use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;

use pm_intel::customlists::{
    CustomListService, UpdateOutcome, PARSE_STATUS_NOTIFICATION_ID,
    PARSE_WARNING_NOTIFICATION_ID, ZERO_IP_NOTIFICATION_ID,
};
use pm_mgr::StateMgr;
use pm_notifications::Notifications;

static COUNTER: AtomicU32 = AtomicU32::new(0);

struct TempFile(PathBuf);

impl TempFile {
    fn with(content: &str) -> TempFile {
        let name = format!(
            "pm-intel-customlists-service-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        );
        let path = std::env::temp_dir().join(name);
        let mut file = std::fs::File::create(&path).expect("create temp file");
        file.write_all(content.as_bytes()).expect("write list");
        TempFile(path)
    }

    fn path_str(&self) -> &str {
        self.0.to_str().expect("utf-8 path")
    }
}

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

#[test]
fn not_configured() {
    let service = CustomListService::new();
    assert_eq!(
        service.check_and_update("").expect("check"),
        UpdateOutcome::NotConfigured
    );
    assert!(!service.lists().is_loaded());
}

#[test]
fn loads_and_reports_through_notifications_and_states() {
    let file = TempFile::with("example.com\n1.2.3.4\nUS\nAS1234\n");
    let notifications = Arc::new(Notifications::new());
    let states = Arc::new(StateMgr::new("CustomList"));
    let service =
        CustomListService::with_reporting(Some(Arc::clone(&notifications)), Some(Arc::clone(&states)));

    let report = match service.check_and_update(file.path_str()).expect("check") {
        UpdateOutcome::Loaded(report) => report,
        other => panic!("expected loaded, got {other:?}"),
    };
    assert_eq!(report.entries(), 4);
    assert!(service.lists().is_loaded());

    let notification = notifications
        .get(PARSE_STATUS_NOTIFICATION_ID)
        .expect("status notification");
    let notification = notification.snapshot();
    assert_eq!(
        notification.title,
        "Custom filter list loaded successfully."
    );
    assert_eq!(
        notification.message,
        format!(
            "Custom filter list loaded from file {}:  \n1 Domains  \n1 IPs  \n1 Autonomous Systems  \n1 Countries",
            file.path_str()
        )
    );
    assert!(states.export().states.is_empty(), "no warnings");
}

#[test]
fn unchanged_file_is_not_parsed_again() {
    let file = TempFile::with("example.com\n");
    let notifications = Arc::new(Notifications::new());
    let service = CustomListService::with_reporting(Some(Arc::clone(&notifications)), None);

    assert!(matches!(
        service.check_and_update(file.path_str()).expect("first"),
        UpdateOutcome::Loaded(_)
    ));
    let first = notifications
        .get(PARSE_STATUS_NOTIFICATION_ID)
        .expect("notification")
        .snapshot();

    assert_eq!(
        service.check_and_update(file.path_str()).expect("second"),
        UpdateOutcome::Unchanged
    );
    let second = notifications
        .get(PARSE_STATUS_NOTIFICATION_ID)
        .expect("notification")
        .snapshot();
    assert_eq!(first.guid, second.guid, "notification was not re-created");
}

#[test]
fn failed_open_adds_a_state_and_a_success_removes_it() {
    let names = format!(
        "pm-intel-customlists-missing-{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, Ordering::Relaxed)
    );
    let path = std::env::temp_dir().join(names);
    let path_str = path.to_str().unwrap().to_string();

    let states = Arc::new(StateMgr::new("CustomList"));
    let service = CustomListService::with_reporting(None, Some(Arc::clone(&states)));

    let error = service.check_and_update(&path_str).expect_err("missing file");
    assert_eq!(error.kind(), std::io::ErrorKind::NotFound);
    let exported = states.export();
    assert_eq!(exported.states.len(), 1);
    assert_eq!(exported.states[0].id, PARSE_WARNING_NOTIFICATION_ID);
    assert_eq!(exported.states[0].name, "Failed to open custom filter list");
    assert_eq!(exported.states[0].message, error.to_string());

    // The file appears; the next check loads it and clears the warning.
    std::fs::write(&path, "example.com\n").expect("write list");
    assert!(matches!(
        service.check_and_update(&path_str).expect("check"),
        UpdateOutcome::Loaded(_)
    ));
    assert!(states.export().states.is_empty());
    let _ = std::fs::remove_file(&path);
}

#[test]
fn too_many_invalid_lines_add_warning_state() {
    let file = TempFile::with("0.0.0.0\n::\nnot a domain !!\nExample.com\n");
    let states = Arc::new(StateMgr::new("CustomList"));
    let service = CustomListService::with_reporting(None, Some(Arc::clone(&states)));

    service.check_and_update(file.path_str()).expect("check");

    let exported = states.export();
    assert_eq!(exported.states.len(), 1);
    let state = &exported.states[0];
    assert_eq!(state.id, ZERO_IP_NOTIFICATION_ID);
    assert_eq!(state.name, "Custom filter list has many invalid lines");
    // 3 of 4 lines are invalid: two zero IPs and the upper-case domain. "not"
    // alone is a valid domain for Go.
    assert_eq!(
        state.message,
        "3 out of 4 lines are invalid.\n\t\t\t Check if you are using the correct file format and if the path to the custom filter list is correct."
    );
}
