//! Compatibility test for the JSON form of a process.

use pm_process::{Process, Tag};
use serde_json::Value;

#[test]
fn process_json_matches_go() {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/process.json");
    let fixture: Value = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
    let expected = fixture["process"].as_str().unwrap();

    // The same values as the Go fixture generator.
    let mut process = Process::empty("4242-1700000000000");
    process.name = "test-process".to_string();
    process.user_id = 1000;
    process.user_name = "testuser".to_string();
    process.user_home = "/home/test".to_string();
    process.pid = 4242;
    process.created_at = 1_700_000_000_000;
    process.parent_pid = 1000;
    process.parent_created_at = 1_600_000_000_000;
    process.leader_pid = 1000;
    process.path = "/tmp/.mount_App/AppRun".to_string();
    process.exec_name = "AppRun".to_string();
    process.cwd = "/home/test".to_string();
    process.cmd_line = "AppRun --flag".to_string();
    process.first_arg = "AppRun".to_string();
    process
        .env
        .insert("HOME".to_string(), "/home/test".to_string());
    process
        .env
        .insert("PATH".to_string(), "/usr/bin".to_string());
    process.tags = vec![
        Tag {
            key: "app-image-path".to_string(),
            value: "/tmp/.mount_App/AppRun".to_string(),
        },
        Tag {
            key: "custom".to_string(),
            value: "value".to_string(),
        },
    ];
    process.matching_path = "/tmp/.mount_App/AppRun".to_string();
    process.primary_profile_id = "_unidentified".to_string();
    process.first_seen = 1_700_000_100;
    process.last_seen = 1_700_000_200;
    process.error = "test error".to_string();
    process
        .exec_hashes
        .insert("sha256".to_string(), "abc123".to_string());

    let serialized = String::from_utf8(pm_dsd::gojson::to_vec(&process).unwrap()).unwrap();
    assert_eq!(serialized, expected);

    // The key is internal and must not leak into the record.
    assert_eq!(process.key(), "4242-1700000000000");
    assert!(!serialized.contains("processKey"));
}
