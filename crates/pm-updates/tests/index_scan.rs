//! Tests for the index scan (Go `service/updates/index_scan.go`).
//!
//! Expected values were produced by running the Go implementation on the same
//! directory layout.

use std::collections::BTreeMap;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU32, Ordering};

use flate2::write::GzEncoder;
use flate2::Compression;
use pm_updates::{generate_index_from_dir, Artifact, IndexScanConfig};

static COUNTER: AtomicU32 = AtomicU32::new(0);

struct TempDir(PathBuf);

impl TempDir {
    fn new(prefix: &str) -> TempDir {
        let name = format!(
            "pm-updates-scan-{prefix}-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        );
        let path = std::env::temp_dir().join(name);
        std::fs::create_dir_all(&path).expect("create temp dir");
        TempDir(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }

    fn write(&self, relative: &str, content: &[u8]) {
        let path = self.0.join(relative);
        std::fs::create_dir_all(path.parent().unwrap()).expect("create parent dir");
        std::fs::write(path, content).expect("write file");
    }

    fn write_gzip(&self, relative: &str, content: &[u8]) {
        let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
        encoder.write_all(content).expect("gzip");
        self.write(relative, &encoder.finish().expect("finish gzip"));
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

fn artifacts(index: &pm_updates::Index) -> Vec<(String, String, String, String, String)> {
    index
        .artifacts
        .iter()
        .map(|artifact| {
            (
                artifact.filename.clone(),
                artifact.sha256.clone(),
                artifact.platform.clone(),
                artifact.unpack.clone(),
                artifact.version.clone(),
            )
        })
        .collect()
}

#[test]
fn index_matches_go_reference() {
    let dir = TempDir::new("reference");
    dir.write("linux_amd64/portmaster-core_v1-2-3", b"core-1.2.3");
    dir.write("linux_amd64/portmaster-core_v1-2-4", b"core-1.2.4");
    dir.write("windows_amd64/portmaster-core_v1-2-3.exe", b"core-win");
    dir.write("README", b"readme");
    dir.write("index.json", b"{}");

    let index = generate_index_from_dir(
        dir.path(),
        &IndexScanConfig {
            name: "test-index".to_string(),
            primary_artifact: "linux_amd64/portmaster-core".to_string(),
            base_url: "https://example.com/updates/".to_string(),
            ignore_files: vec!["*.json".to_string()],
            ..IndexScanConfig::default()
        },
    )
    .expect("generate index");

    // The index version comes from the primary artifact, the older file is
    // dropped and the matching artifact version is omitted.
    assert_eq!(index.name, "test-index");
    assert_eq!(index.version, "1.2.4");
    assert!(index.is_locally_generated);

    assert_eq!(
        artifacts(&index),
        vec![
            (
                "README".to_string(),
                "711a6108ba2ce6ca93dd47d6817f2361db10d8ab6eec89460b2dfc2c325efabe".to_string(),
                String::new(),
                String::new(),
                String::new(),
            ),
            (
                "portmaster-core".to_string(),
                "554c5dda1a7dc8f2d69d0da583f20041ad0d498299474eaf3a2b5aa56c66199c".to_string(),
                "linux_amd64".to_string(),
                String::new(),
                String::new(),
            ),
            (
                "portmaster-core.exe".to_string(),
                "76c2d8f2e3c4f8cd7a2e2fc012c8cffc83479f88be49b0b8e2a6c4a8fa3e6525".to_string(),
                "windows_amd64".to_string(),
                String::new(),
                "1.2.3".to_string(),
            ),
        ]
    );

    assert_eq!(
        index.artifacts[1].urls,
        vec!["https://example.com/updates/linux_amd64/portmaster-core_v1-2-4".to_string()]
    );
}

#[test]
fn unpacks_before_hashing_and_strips_suffix() {
    let dir = TempDir::new("unpack");
    dir.write_gzip("linux_amd64/intel/geoipv4.mmdb.gz", b"geoip-data");

    let index = generate_index_from_dir(
        dir.path(),
        &IndexScanConfig {
            name: "unpack".to_string(),
            base_url: "https://e/".to_string(),
            unpack_files: BTreeMap::from([("gz".to_string(), "**/*.gz".to_string())]),
            ..IndexScanConfig::default()
        },
    )
    .expect("generate index");

    assert_eq!(artifacts(&index).len(), 1);
    let artifact = &index.artifacts[0];
    assert_eq!(artifact.filename, "geoipv4.mmdb");
    assert_eq!(artifact.unpack, "gz");
    assert_eq!(artifact.platform, "linux_amd64");
    // SHA-256 of "geoip-data", not of the gzip stream.
    assert_eq!(
        artifact.sha256,
        "71201211297736a37b18d9a5627ed2b6c403de89c78436cf8cd681ca7543be00"
    );
}

#[test]
fn unpack_glob_does_not_cross_separators() {
    let dir = TempDir::new("nested-glob");
    dir.write_gzip("linux_amd64/tool_v1-0-0.gz", b"payload");

    // `*.gz` does not match the nested file, so it stays packed and keeps the
    // version (the index has no version of its own).
    let index = generate_index_from_dir(
        dir.path(),
        &IndexScanConfig {
            unpack_files: BTreeMap::from([("gz".to_string(), "*.gz".to_string())]),
            ..IndexScanConfig::default()
        },
    )
    .expect("generate index");

    let artifact = &index.artifacts[0];
    // The version is still split off the name, so only ".gz" remains.
    assert_eq!(artifact.filename, "tool.gz");
    assert_eq!(artifact.unpack, "");
    assert_eq!(artifact.version, "1.0.0");
    assert_eq!(artifact.platform, "linux_amd64");
}

#[test]
fn mixed_versioned_and_unversioned_files_fail() {
    let dir = TempDir::new("mixed");
    dir.write("x/portmaster", b"x");
    dir.write("x/portmaster_v1-2-3", b"y");

    let error = generate_index_from_dir(dir.path(), &IndexScanConfig::default())
        .expect_err("mixed files must fail");
    assert!(
        error
            .to_string()
            .contains("both a versioned and non-versioned file for: x/portmaster"),
        "unexpected error: {error}"
    );
}

#[test]
fn duplicate_versions_fail() {
    let dir = TempDir::new("duplicate");
    dir.write("x/portmaster_v1-2-3", b"x");
    dir.write("x/nested/portmaster_v1-2-3", b"y");

    let error = generate_index_from_dir(dir.path(), &IndexScanConfig::default())
        .expect_err("duplicate versions must fail");
    assert!(
        error.to_string().contains("duplicate version for x/portmaster"),
        "unexpected error: {error}"
    );
}

#[test]
fn keeps_the_newest_version_and_prereleases() {
    let dir = TempDir::new("newest");
    dir.write("tool_v2-0-0-beta", b"beta");
    dir.write("tool_v2-0-0", b"stable");

    let index = generate_index_from_dir(dir.path(), &IndexScanConfig::default())
        .expect("generate index");

    assert_eq!(artifacts(&index).len(), 1);
    assert_eq!(index.artifacts[0].version, "2.0.0");
    assert_eq!(
        index.artifacts[0].sha256,
        "f379ccb92b9116442dc65bdc35648a85d3786b34779db7f704a901fa07b00cb6"
    );
}

#[test]
fn applies_templates_by_identifier() {
    let dir = TempDir::new("template");
    dir.write("linux_amd64/custom_v1-0-0", b"tmpl");

    let template = Artifact {
        filename: "renamed".to_string(),
        urls: vec!["https://example.com/custom".to_string()],
        ..Artifact::default()
    };

    let index = generate_index_from_dir(
        dir.path(),
        &IndexScanConfig {
            templates: BTreeMap::from([("custom".to_string(), template)]),
            base_url: "https://ignored/".to_string(),
            ..IndexScanConfig::default()
        },
    )
    .expect("generate index");

    let artifact = &index.artifacts[0];
    assert_eq!(artifact.filename, "renamed");
    assert_eq!(artifact.urls, vec!["https://example.com/custom".to_string()]);
    assert_eq!(artifact.version, "1.0.0");
    assert_eq!(artifact.platform, "linux_amd64");
}

#[test]
fn default_name_is_the_directory_name() {
    let dir = TempDir::new("name");
    dir.write("file", b"x");

    let index = generate_index_from_dir(dir.path(), &IndexScanConfig::default())
        .expect("generate index");

    let expected = dir
        .path()
        .file_name()
        .unwrap()
        .to_string_lossy()
        .to_string();
    assert_eq!(index.name, expected);
}

#[test]
fn invalid_glob_patterns_fail() {
    let dir = TempDir::new("bad-glob");
    dir.write("file", b"x");

    let error = generate_index_from_dir(
        dir.path(),
        &IndexScanConfig {
            ignore_files: vec!["{a,b}".to_string()],
            ..IndexScanConfig::default()
        },
    )
    .expect_err("unsupported pattern must fail");
    assert!(
        error.to_string().contains("invalid ignore files pattern"),
        "unexpected error: {error}"
    );
}

#[test]
fn sha256_of_packed_file() {
    let dir = TempDir::new("sha");
    dir.write_gzip("a.gz", b"payload");
    let path = dir.path().join("a.gz");

    let packed = pm_updates::sha256_of_file(path.to_str().unwrap(), "").expect("hash file");
    let unpacked = pm_updates::sha256_of_file(path.to_str().unwrap(), "gz").expect("hash gzip");
    assert_ne!(packed, unpacked);
    assert_eq!(
        unpacked,
        "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
    );
}
