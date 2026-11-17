//! `mirror` subcommand: mirror artifacts of an index into a directory,
//! port of Go `cmds/updatemgr/mirror.go`.

use std::sync::Arc;

use pm_updates::{check_sha256_sum, check_sha256_sum_file, decompress, HttpClient, HttpsClient, Index};

use crate::convert::{convert_v1, convert_v2, now};

/// User agent of the tool, like Go.
pub fn user_agent() -> String {
    format!("Portmaster Update Mgr ({}_{})", std::env::consts::OS, std::env::consts::ARCH)
}

/// Fetches data from a URL with the tool's user agent.
fn fetch(client: &dyn HttpClient, url: &str) -> Result<Vec<u8>, String> {
    println!("    fetching from {url}");
    client.get(url)
}

/// Computes the SHA-256 of a byte slice, like Go's `sha256.Sum256` + hex.
fn sha256_hex(data: &[u8]) -> String {
    use sha2::Digest;
    hex::encode(sha2::Sha256::digest(data))
}

/// Mirror parameters copied from the Go flags.
#[derive(Debug, Clone, Default)]
pub struct MirrorOptions {
    /// Check local artifacts instead of downloading them.
    pub check: bool,
    /// Also download `.sig` files.
    pub sigs: bool,
}

/// Mirrors all artifacts of the index at `index_url` into `target_dir`.
pub fn mirror(
    index_url: &str,
    target_dir: &std::path::Path,
    options: &MirrorOptions,
    config: &pm_updates::IndexScanConfig,
) -> Result<(), String> {
    let stat = std::fs::metadata(target_dir)
        .map_err(|error| format!("failed to access target dir: {error}"))?;
    if !stat.is_dir() {
        return Err("target is not a directory".to_string());
    }

    let parsed_url = url::Url::parse(index_url).map_err(|error| format!("invalid index URL: {error}"))?;
    let index_path = parsed_url.path().to_string();
    let mut base = parsed_url.clone();
    base.set_query(None);
    base.set_fragment(None);
    base.set_path("");
    let base_url = format!("{base}/");

    let agent = HttpsClient::new().with_user_agent(&user_agent());
    let client: Arc<dyn HttpClient> = Arc::new(agent);

    println!("downloading index...");
    let index_data = fetch(client.as_ref(), index_url)
        .map_err(|error| format!("download index: {error}"))?;

    let (mut index, new_index_name) = if index_path.ends_with(".v3.json") {
        let index: Index = serde_json::from_slice(&index_data)
            .map_err(|error| format!("parse v3 index: {error}"))?;
        let name = index_path.rsplit('/').next().unwrap_or("index.v3.json").to_string();
        (index, name)
    } else if index_path.ends_with(".v2.json") {
        let index = convert_v2(&index_data, &base_url, config)
            .map_err(|error| format!("convert v2 index: {error}"))?;
        let name = index_path
            .rsplit('/')
            .next()
            .unwrap_or("index.v2.json")
            .trim_end_matches(".v2.json")
            .to_string()
            + ".v3.json";
        (index, name)
    } else if index_path.ends_with(".json") {
        let index = convert_v1(&index_data, &base_url, now(), config)
            .map_err(|error| format!("convert v1 index: {error}"))?;
        let name = index_path
            .rsplit('/')
            .next()
            .unwrap_or("index.json")
            .trim_end_matches(".json")
            .to_string()
            + ".v3.json";
        (index, name)
    } else {
        return Err("invalid index file extension".to_string());
    };

    if options.check {
        return check_mirror(&index, target_dir);
    }

    for artifact in &mut index.artifacts {
        let Some(url) = artifact.urls.first() else {
            return Err(format!("get artifact {}: no URLS defined", artifact.filename));
        };
        let artifact_url = url::Url::parse(url)
            .map_err(|error| format!("get artifact {}: invalid URL: {error}", artifact.filename))?;
        let location = artifact_url.path().trim_start_matches('/');
        println!("getting {location}...");

        let destination = target_dir.join(location);
        if !artifact.sha256.is_empty() && destination.exists() {
            match check_sha256_sum_file(&destination, &artifact.sha256) {
                Ok(()) => {
                    println!(
                        "    skipping download, verified SHA256 checksum: {}",
                        artifact.sha256
                    );
                    continue;
                }
                Err(error) => println!("    existing file did not match: {error}"),
            }
        }

        let data = fetch(client.as_ref(), url)
            .map_err(|error| format!("get artifact {location}: GET artifact: {error}"))?;
        let final_data = if artifact.unpack.is_empty() {
            data.clone()
        } else {
            decompress(&artifact.unpack, &data).map_err(|error| format!("decompress: {error}"))?
        };
        if artifact.sha256.is_empty() {
            artifact.sha256 = sha256_hex(&final_data);
        } else {
            check_sha256_sum(&final_data, &artifact.sha256).map_err(|error| error.to_string())?;
        }

        if let Some(parent) = destination.parent() {
            std::fs::create_dir_all(parent)
                .map_err(|error| format!("create artifact dir {}: {error}", parent.display()))?;
        }
        std::fs::write(&destination, &data)
            .map_err(|error| format!("save artifact {location}: {error}"))?;

        if options.sigs {
            let signature = fetch(client.as_ref(), &format!("{url}.sig")).ok();
            if let Some(signature) = signature {
                let path = destination.with_extension(format!(
                    "{}.sig",
                    destination.extension().map_or("", |ext| ext.to_str().unwrap_or(""))
                ));
                if let Err(error) = std::fs::write(&path, &signature) {
                    println!("    failed to write sig: {error}");
                } else {
                    println!("    sig written to {}", path.display());
                }
            }
        }
    }

    let index_json =
        serde_json::to_string_pretty(&index).map_err(|error| format!("marshal index: {error}"))?;
    let index_path = target_dir.join(new_index_name);
    std::fs::write(&index_path, index_json)
        .map_err(|error| format!("write index to {}: {error}", index_path.display()))?;
    Ok(())
}

/// Verifies all artifacts of an index against local files, like Go `checkMirror`.
pub fn check_mirror(index: &Index, target_dir: &std::path::Path) -> Result<(), String> {
    for artifact in &index.artifacts {
        let Some(url) = artifact.urls.first() else {
            return Err(format!(
                "check artifact {}: no URLS defined (required for path)",
                artifact.filename
            ));
        };
        let parsed = url::Url::parse(url).map_err(|error| {
            format!(
                "check artifact {}: invalid URL (required for path): {error}",
                artifact.filename
            )
        })?;
        let location = parsed.path().trim_start_matches('/');
        if artifact.sha256.is_empty() {
            return Err(format!(
                "check artifact {location}: no checksum defined, please be sure you are using v3 index"
            ));
        }
        let destination = target_dir.join(location);
        // Index checksums describe the unpacked bytes, just like the normal
        // download path; mirrored files stay compressed, so verify the
        // decompressed stream instead of the raw file.
        match pm_updates::check_sha256_sum_file_unpacked(
            &destination,
            &artifact.unpack,
            &artifact.sha256,
        ) {
            Ok(()) => println!("verified: {location}"),
            Err(error) => println!("failed to verify {location}: {error}"),
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use pm_updates::Artifact;

    #[test]
    fn user_agent_matches_go_shape() {
        let agent = user_agent();
        assert!(agent.starts_with("Portmaster Update Mgr ("), "{agent}");
        assert!(agent.contains(std::env::consts::OS));
    }

    #[test]
    fn check_mirror_requires_checksums() {
        let index = Index {
            artifacts: vec![Artifact {
                filename: "tool".to_string(),
                urls: vec!["https://example.com/linux_amd64/tool".to_string()],
                ..Artifact::default()
            }],
            ..Index::default()
        };
        assert!(check_mirror(&index, std::path::Path::new("."))
            .unwrap_err()
            .contains("no checksum defined"));
    }

    #[test]
    fn check_mirror_verifies_files() {
        let data = b"artifact";
        let dir = std::env::temp_dir().join(format!(
            "updatemgr-mirror-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .subsec_nanos()
        ));
        let path = dir.join("linux_amd64/tool");
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, data).unwrap();
        let index = Index {
            artifacts: vec![Artifact {
                filename: "tool".to_string(),
                urls: vec!["https://example.com/linux_amd64/tool".to_string()],
                sha256: sha256_hex(data),
                ..Artifact::default()
            }],
            ..Index::default()
        };
        assert!(check_mirror(&index, &dir).is_ok());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
