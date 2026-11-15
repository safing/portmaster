//! Tests of the proc reader.

mod common;

use pm_netstate::ProcFs;
use pm_process::ProcReader;

#[test]
fn reads_process_details() {
    let root = common::temp_dir("info");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 1234, 1, 1234, 250, "my process", "/usr/bin/myproc");

    let passwd = common::write_passwd(&root);
    let reader = ProcReader::with_passwd(ProcFs::new(&root), &passwd);
    let info = reader.read(1234).expect("process exists");

    assert_eq!(info.pid, 1234);
    assert_eq!(info.ppid, 1);
    assert_eq!(info.pgrp, 1234);
    // (250 / 100 + 1700000000) * 1000, like gopsutil.
    assert_eq!(info.created_at, 1_700_000_002_000);
    assert_eq!(info.uid, 1000);
    assert_eq!(info.name, "my process");
    assert_eq!(info.exe, "/usr/bin/myproc");
    assert_eq!(info.cwd, "/tmp");
    assert_eq!(info.cmdline, "myproc --flag");
    assert_eq!(info.environ["HOME"], "/home/test");
    assert_eq!(info.environ["PATH"], "/usr/bin");
    assert_eq!(info.environ["QUOTED"], "value");
    assert_eq!(info.username, "testuser");

    // Without a passwd file the username stays empty.
    let reader = ProcReader::with_passwd(ProcFs::new(&root), root.join("missing-passwd"));
    assert_eq!(reader.read(1234).unwrap().username, "");
}

#[test]
fn deleted_executables_are_trimmed() {
    let root = common::temp_dir("deleted");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 1234, 1, 1234, 250, "gone", "/usr/bin/gone (deleted)");

    let reader = ProcReader::new(ProcFs::new(&root));
    assert_eq!(reader.read(1234).unwrap().exe, "/usr/bin/gone");
}

#[test]
fn missing_processes_are_reported() {
    let root = common::temp_dir("missing");
    common::set_boot_time(&root, 1_700_000_000);
    let reader = ProcReader::new(ProcFs::new(&root));

    match reader.read(4242) {
        Err(pm_process::ProcError::NoSuchProcess(pid)) => assert_eq!(pid, 4242),
        other => panic!("unexpected result: {other:?}"),
    }
}

#[test]
fn process_names_with_special_characters() {
    let root = common::temp_dir("names");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 77, 1, 77, 100, "weird (name) here", "/bin/weird");

    let reader = ProcReader::new(ProcFs::new(&root));
    assert_eq!(reader.read(77).unwrap().name, "weird (name) here");
}
