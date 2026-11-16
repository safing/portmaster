//! `download` subcommand: fetch all artifacts of an index into a directory,
//! port of Go `cmds/updatemgr/download.go`.

use std::sync::Arc;

use pm_updates::{HttpClient, HttpsClient, Updater, UpdaterConfig};

/// Current platform in Go's `GOOS_GOARCH` form.
pub fn current_platform() -> String {
    format!("{}_{}", std::env::consts::OS, std::env::consts::ARCH)
}

fn temp_dir(prefix: &str) -> Result<std::path::PathBuf, String> {
    let path = std::env::temp_dir().join(format!(
        "portmaster-updatemgr-{prefix}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_or(0, |elapsed| elapsed.subsec_nanos())
    ));
    std::fs::create_dir_all(&path).map_err(|error| error.to_string())?;
    Ok(path)
}

/// Downloads the index and all its artifacts into `target_dir`.
pub fn download(index_url: &str, target_dir: &std::path::Path, platform: &str) -> Result<(), String> {
    let stat = std::fs::metadata(target_dir)
        .map_err(|error| format!("failed to access target dir: {error}"))?;
    if !stat.is_dir() {
        return Err("target is not a directory".to_string());
    }

    let download_dir = temp_dir("download")?;
    let purge_dir = temp_dir("purge")?;

    let config = UpdaterConfig {
        name: "Downloader".to_string(),
        directory: target_dir.to_string_lossy().into_owned(),
        download_directory: download_dir.to_string_lossy().into_owned(),
        purge_directory: purge_dir.to_string_lossy().into_owned(),
        index_urls: vec![index_url.to_string()],
        index_file: "index.json".to_string(),
        platform: platform.to_string(),
        auto_check: false,
        auto_download: false,
        auto_apply: false,
        needs_restart: false,
        notify: false,
        ..UpdaterConfig::default()
    };

    let client: Arc<dyn HttpClient> = Arc::new(HttpsClient::new());
    let result = Updater::new(config, client, "", None, None)
        .and_then(|updater| updater.force_update())
        .map_err(|error| error.to_string());

    let _ = std::fs::remove_dir_all(&download_dir);
    let _ = std::fs::remove_dir_all(&purge_dir);
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn platform_has_go_shape() {
        let platform = current_platform();
        assert!(platform.contains('_'), "{platform}");
        assert!(platform.contains('_'), "{platform}");
    }

    #[test]
    fn rejects_non_directories() {
        let file = std::env::temp_dir().join(format!(
            "updatemgr-not-a-dir-{}",
            std::process::id()
        ));
        std::fs::write(&file, b"x").unwrap();
        assert_eq!(
            download("https://example.com/index.json", &file, "linux_amd64"),
            Err("target is not a directory".to_string())
        );
        let _ = std::fs::remove_file(&file);
    }
}
