//! Rust port of the Go `cmds/updatemgr` tool.
//!
//! Ported: `scan`, `convert`, `download` and `mirror`.
//! `sign` is registered but reports that jess signing is not available: the
//! Rust `pm-jess` crate only verifies signatures.

mod convert;
mod download;
mod mirror;

use std::path::PathBuf;

use pm_updates::IndexScanConfig;

use crate::convert::{convert_v1, convert_v2, now};
use crate::download::{current_platform, download};
use crate::mirror::{mirror, MirrorOptions};

const HELP: &str = "Usage: updatemgr-rs <command> [options]

Commands:
  scan     --dir <directory>                     Create an index from a directory
  convert  [--from v1|v2] [--base-url URL]       Convert a legacy index to v3
           [--published SECONDS] <index file>
  download [--platform GOOS_GOARCH]              Download all artifacts of an index
           <index URL> <download dir>
  mirror   [--check] [--sigs]                    Mirror artifacts of an index
           <index URL> <mirror dir>
  sign     [--envelope NAME] <index file>        Not available (jess signer not ported)
";

/// Builds the scan config shared by `scan` and `convert`, like Go's
/// `cmds/updatemgr/scan.go`.
pub fn scan_config() -> IndexScanConfig {
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
        ignore_files: convert::IGNORE_FILES
            .iter()
            .map(|pattern: &&str| pattern.to_string())
            .collect(),
        unpack_files,
        ..IndexScanConfig::default()
    }
}

fn take_value(args: &mut std::vec::IntoIter<String>, name: &str) -> Result<Option<String>, String> {
    match args.next() {
        Some(value) if value.starts_with("--") => Err(format!("flag needs an argument: {name}")),
        other => Ok(other),
    }
}

fn run(args: Vec<String>) -> Result<(), String> {
    let mut args = args.into_iter();
    let command = args.next().ok_or_else(|| HELP.to_string())?;

    match command.as_str() {
        "scan" => {
            let mut dir = None;
            while let Some(arg) = args.next() {
                match arg.as_str() {
                    "--dir" | "-d" => dir = take_value(&mut args, "--dir")?,
                    other if other.starts_with("--dir=") => {
                        dir = Some(other.trim_start_matches("--dir=").to_string())
                    }
                    other => return Err(format!("unknown flag: {other}")),
                }
            }
            let dir = dir.ok_or_else(|| "flag needs an argument: --dir".to_string())?;
            let index = pm_updates::generate_index_from_dir(&dir, &scan_config())
                .map_err(|error| format!("scan {dir}: {error}"))?;
            println!(
                "{}",
                serde_json::to_string_pretty(&index).map_err(|error| format!("marshal index: {error}"))?
            );
        }
        "convert" => {
            let mut format = String::new();
            let mut base_url = String::new();
            let mut published = None;
            let mut file = None;
            while let Some(arg) = args.next() {
                match arg.as_str() {
                    "--from" => format = take_value(&mut args, "--from")?.unwrap_or_default(),
                    "--base-url" => {
                        base_url = take_value(&mut args, "--base-url")?.unwrap_or_default()
                    }
                    "--published" => {
                        published = Some(
                            take_value(&mut args, "--published")?
                                .ok_or_else(|| "invalid --published".to_string())?
                                .parse::<i64>()
                                .map_err(|error| format!("invalid --published: {error}"))?,
                        )
                    }
                    other if other.starts_with('-') => return Err(format!("unknown flag: {other}")),
                    other => file = Some(other.to_string()),
                }
            }
            let file = file.ok_or_else(|| "missing index file".to_string())?;
            let data = std::fs::read(&file)
                .map_err(|error| format!("read index file: {error}"))?;
            let config = scan_config();
            let index = match format.as_str() {
                "v1" => convert_v1(&data, &base_url, published.unwrap_or_else(now), &config)?,
                "v2" => convert_v2(&data, &base_url, &config)?,
                other => return Err(format!("unsupported --from: {other} (use v1 or v2)")),
            };
            println!(
                "{}",
                serde_json::to_string_pretty(&index).map_err(|error| format!("marshal index: {error}"))?
            );
        }
        "download" => {
            let mut platform = current_platform();
            let mut positional = Vec::new();
            while let Some(arg) = args.next() {
                match arg.as_str() {
                    "--platform" | "-p" => {
                        platform = take_value(&mut args, "--platform")?.unwrap_or_default()
                    }
                    other if other.starts_with('-') => return Err(format!("unknown flag: {other}")),
                    other => positional.push(other.to_string()),
                }
            }
            if positional.len() != 2 {
                return Err("download expects <index URL> <download dir>".to_string());
            }
            download(&positional[0], &PathBuf::from(&positional[1]), &platform)?;
        }
        "mirror" => {
            let mut options = MirrorOptions::default();
            let mut positional = Vec::new();
            for arg in args.by_ref() {
                match arg.as_str() {
                    "--check" => options.check = true,
                    "--sigs" => options.sigs = true,
                    other if other.starts_with('-') => return Err(format!("unknown flag: {other}")),
                    other => positional.push(other.to_string()),
                }
            }
            if positional.len() != 2 {
                return Err("mirror expects <index URL> <mirror dir>".to_string());
            }
            mirror(
                &positional[0],
                &PathBuf::from(&positional[1]),
                &options,
                &scan_config(),
            )?;
        }
        "sign" => {
            return Err("sign is not available: the Rust pm-jess crate only verifies signatures (jess signer not ported)".to_string());
        }
        "help" | "--help" | "-h" => {
            print!("{HELP}");
        }
        other => return Err(format!("unknown command: {other}\n{HELP}")),
    }
    Ok(())
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if let Err(error) = run(args) {
        eprintln!("{error}");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn help_works() {
        assert!(run(vec!["--help".to_string()]).is_ok());
    }

    #[test]
    fn unknown_command_is_rejected() {
        assert!(run(vec!["nope".to_string()]).unwrap_err().contains("unknown command"));
    }

    #[test]
    fn sign_reports_the_missing_signer() {
        let error = run(vec!["sign".to_string(), "index.json".to_string()]).unwrap_err();
        assert!(error.contains("pm-jess"), "{error}");
    }

    #[test]
    fn config_keeps_go_patterns() {
        let config = scan_config();
        assert_eq!(config.ignore_files.len(), convert::IGNORE_FILES.len());
        assert_eq!(config.unpack_files.get("gz").unwrap(), "**/*.gz");
        assert_eq!(config.primary_artifact, "linux_amd64/portmaster-core");
    }
}
