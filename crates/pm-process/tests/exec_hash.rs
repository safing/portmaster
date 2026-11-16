//! Tests for executable hashing (Go `service/process/executable.go`).

use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};

use pm_process::{HashError, Process};

static COUNTER: AtomicU32 = AtomicU32::new(0);

/// A file in the temp directory that removes itself on drop.
struct TempFile(PathBuf);

impl TempFile {
    fn with(content: &[u8]) -> TempFile {
        let name = format!(
            "pm-process-hash-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        );
        let path = std::env::temp_dir().join(name);
        let mut file = std::fs::File::create(&path).expect("create temp file");
        file.write_all(content).expect("write temp file");
        TempFile(path)
    }

    fn path_str(&self) -> &str {
        self.0.to_str().expect("utf-8 path")
    }

    fn process(&self) -> Process {
        let mut process = Process::empty("1-0");
        process.path = self.path_str().to_string();
        process
    }
}

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

#[test]
fn hashes_empty_file_like_coreutils() {
    let file = TempFile::with(b"");

    // Reference values from `md5sum`, `sha1sum` and `sha256sum` on an empty file.
    assert_eq!(
        pm_process::hash_file(file.path_str(), "md5").unwrap(),
        "d41d8cd98f00b204e9800998ecf8427e"
    );
    assert_eq!(
        pm_process::hash_file(file.path_str(), "sha1").unwrap(),
        "da39a3ee5e6b4b0d3255bfef95601890afd80709"
    );
    assert_eq!(
        pm_process::hash_file(file.path_str(), "sha256").unwrap(),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
}

#[test]
fn hashes_content_like_coreutils() {
    let file = TempFile::with(b"portmaster\n");

    // `printf 'portmaster\n' | md5sum | sha1sum | sha256sum`
    assert_eq!(
        pm_process::hash_file(file.path_str(), "md5").unwrap(),
        "ce245dcc802a57d39f91368f7871be36"
    );
    assert_eq!(
        pm_process::hash_file(file.path_str(), "sha1").unwrap(),
        "9d5bf020469743c84450065efbac0e2e33c25a66"
    );
    assert_eq!(
        pm_process::hash_file(file.path_str(), "sha256").unwrap(),
        "ddb2366adb3d7499e63b97931a4b3cbc7f31f2f3ac5707d01fc2f0cbfe047ac6"
    );
}

#[test]
fn process_caches_hashes() {
    let file = TempFile::with(b"exe");
    let mut process = file.process();

    let first = process.get_exec_hash("sha256").unwrap();
    assert_eq!(process.exec_hashes.get("sha256"), Some(&first));

    // The cached value is returned even after the file is gone, like Go.
    let path = file.path_str().to_string();
    std::fs::remove_file(&path).unwrap();
    assert_eq!(process.get_exec_hash("sha256").unwrap(), first);
}

#[test]
fn unknown_algorithm_is_an_error() {
    let file = TempFile::with(b"exe");
    let mut process = file.process();

    match process.get_exec_hash("crc32") {
        Err(HashError::UnsupportedAlgorithm(algorithm)) => assert_eq!(algorithm, "crc32"),
        other => panic!("expected unsupported algorithm error, got {other:?}"),
    }
    assert!(process.exec_hashes.is_empty());
}

#[test]
fn missing_file_is_an_error() {
    let mut process = Process::empty("1-0");
    process.path = "/nonexistent/portmaster-exec".to_string();

    match process.get_exec_hash("sha256") {
        Err(HashError::Io(error)) => {
            assert_eq!(error.kind(), std::io::ErrorKind::NotFound);
        }
        other => panic!("expected io error, got {other:?}"),
    }
}
