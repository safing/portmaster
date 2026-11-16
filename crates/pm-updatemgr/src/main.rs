//! Rust port of the `scan` command of Go `cmds/updatemgr`.
//!
//! Other commands (sign, mirror, convert, download) depend on tooling that is
//! not part of the core port; `scan` is fully covered by
//! `pm_updates::generate_index_from_dir`.


use pm_updates::{generate_index_from_dir, Index, IndexScanConfig};

/// Ignore patterns copied verbatim from Go `cmds/updatemgr/scan.go`.
const IGNORE_FILES: &[&str] = &[
    // Indexes, checksums, latest symlinks.
    "*.json",
    "sha256*.txt",
    "latest/**",
    // Signatures.
    "*.sig",
    "**/*.sig",
    // Related, but not required artifacts.
    "**/*.apk",
    "**/*install*",
    "**/spn-hub*",
    "**/jess*",
    "**/hubs*.json",
    "**/*mini*.mmdb.gz",
    // Unsupported platforms.
    "darwin_amd64/**",
    "darwin_arm64/**",
    // Deprecated artifacts.
    "**/portmaster-start*",
    "**/portmaster-app*",
    "**/portmaster-notifier*",
    "**/portmaster-wintoast*.dll",
    "**/portmaster-snoretoast*.exe",
    "**/portmaster-kext*.dll",
    "**/profilemgr*.zip",
    "**/settings*.zip",
    "**/monitor*.zip",
    "**/base*.zip",
    "**/console*.zip",
];

/// Scan configuration copied from Go.
fn scan_config() -> IndexScanConfig {
    let mut unpack_files = std::collections::BTreeMap::new();
    unpack_files.insert("gz".to_string(), "**/*.gz".to_string());
    unpack_files.insert(
        "zip".to_string(),
        "**/app2/**/portmaster-app*.zip".to_string(),
    );

    IndexScanConfig {
        name: pm_updates::DEFAULT_BINARY_INDEX_NAME.to_string(),
        primary_artifact: "linux_amd64/portmaster-core".to_string(),
        base_url: "https://updates.safing.io/".to_string(),
        ignore_files: IGNORE_FILES.iter().map(|pattern| pattern.to_string()).collect(),
        unpack_files,
        ..IndexScanConfig::default()
    }
}

/// Generates the index from a directory and returns its indented JSON.
fn scan(dir: &std::path::Path) -> Result<String, String> {
    let index: Index = generate_index_from_dir(dir, &scan_config())
        .map_err(|error| format!("scan {}: {error}", dir.display()))?;
    serde_json::to_string_pretty(&index).map_err(|error| format!("marshal index: {error}"))
}

fn main() {
    let mut args = std::env::args().skip(1);
    let command = args.next();
    let mut dir = None;

    match command.as_deref() {
        Some("scan") => {
            while let Some(arg) = args.next() {
                match arg.as_str() {
                    "--dir" | "-d" => dir = args.next(),
                    other if other.starts_with("--dir=") => {
                        dir = Some(other.trim_start_matches("--dir=").to_string())
                    }
                    other => {
                        eprintln!("unknown flag: {other}");
                        std::process::exit(1);
                    }
                }
            }
        }
        Some(other) => {
            eprintln!("unknown command: {other}");
            eprintln!("available commands: scan");
            std::process::exit(1);
        }
        None => {
            eprintln!("usage: updatemgr-rs scan --dir <directory>");
            std::process::exit(1);
        }
    }

    let Some(dir) = dir else {
        eprintln!("flag needs an argument: --dir");
        std::process::exit(1);
    };
    match scan(std::path::Path::new(&dir)) {
        Ok(json) => println!("{json}"),
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scan_creates_an_index() {
        let dir = std::env::temp_dir().join(format!(
            "pm-updatemgr-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .subsec_nanos()
        ));
        let platform = dir.join("linux_amd64");
        std::fs::create_dir_all(&platform).unwrap();
        std::fs::write(platform.join("portmaster-core"), b"binary").unwrap();
        std::fs::write(dir.join("index.json"), b"ignored").unwrap();

        let json = scan(&dir).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed["Name"], pm_updates::DEFAULT_BINARY_INDEX_NAME);
        assert_eq!(parsed["Artifacts"].as_array().unwrap().len(), 1);
        assert_eq!(
            parsed["Artifacts"][0]["Filename"],
            "portmaster-core"
        );
        assert_eq!(
            parsed["Artifacts"][0]["URLs"][0],
            "https://updates.safing.io/linux_amd64/portmaster-core"
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn config_keeps_go_patterns() {
        let config = scan_config();
        assert_eq!(config.ignore_files.len(), IGNORE_FILES.len());
        assert_eq!(config.unpack_files.get("gz").unwrap(), "**/*.gz");
        assert_eq!(config.primary_artifact, "linux_amd64/portmaster-core");
    }
}
