//! Configuration of the updates module, port of the config part of Go
//! `service/updates/module.go`.

use std::path::{MAIN_SEPARATOR, Path};

/// A command that is run after an upgrade, like Go `UpdateCommandConfig`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct UpdateCommandConfig {
    /// Shell command to execute.
    pub command: String,
    /// Arguments for the command.
    pub args: Vec<String>,
    /// Run only if this artifact (full path) was upgraded; empty means always.
    pub trigger_artifact_fname: String,
    /// Whether a failing command fails the upgrade.
    pub fail_on_error: bool,
}

/// Configuration of an updater, like Go `Config`.
#[derive(Debug, Clone, Default)]
pub struct UpdaterConfig {
    /// Name of the updater.
    pub name: String,
    /// Directory that holds the artifacts in use.
    pub directory: String,
    /// Directory where new artifacts are prepared for the upgrade.
    pub download_directory: String,
    /// Directory where old artifacts are moved during the upgrade.
    pub purge_directory: String,
    /// File and directory names in the main directory to leave alone.
    pub ignore: Vec<String>,
    /// URLs of the index files.
    pub index_urls: Vec<String>,
    /// Name of the index file in the directories.
    pub index_file: String,
    /// Whether the index signature is verified; `None` disables the check,
    /// like a nil `jess.TrustStore` in Go.
    pub verify: Option<pm_jess::TrustStore>,
    /// Platform to download artifacts for; defaults to the current platform.
    pub platform: String,
    /// Whether indexes may be downloaded without an outside trigger.
    pub auto_check: bool,
    /// Whether updates may be downloaded without an outside trigger.
    pub auto_download: bool,
    /// Whether updates may be applied without an outside trigger.
    pub auto_apply: bool,
    /// Whether a restart is required after an upgrade.
    pub needs_restart: bool,
    /// Whether the user is informed about updates.
    pub notify: bool,
    /// Commands to run after an upgrade.
    pub post_upgrade_commands: Vec<UpdateCommandConfig>,
    /// Optional GitHub repository (`owner/repo`) to check for releases instead of index files.
    pub github_repo: Option<String>,
}

/// An obvious configuration error.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
#[error("{0}")]
pub struct ConfigError(pub String);

impl UpdaterConfig {
    /// Checks for obvious configuration errors and fills in the platform.
    ///
    /// Error messages are identical to Go `Config.Check`.
    pub fn check(&mut self) -> Result<(), ConfigError> {
        let missing = if self.name.is_empty() {
            Some("name must be set")
        } else if self.directory.is_empty() {
            Some("directory must be set")
        } else if self.download_directory.is_empty() {
            Some("download directory must be set")
        } else if self.purge_directory.is_empty() {
            Some("purge directory must be set")
        } else if self.index_file.is_empty() {
            Some("index file must be set")
        } else if self.auto_apply && !self.auto_download {
            Some("auto apply is set, but auto download is not")
        } else {
            None
        };
        if let Some(message) = missing {
            return Err(ConfigError(message.to_string()));
        }

        // Ignore entries must be names, not paths.
        for (index, entry) in self.ignore.iter().enumerate() {
            // Go checks `filepath.Separator`, which is `/` on Linux. A Windows
            // port must also reject `\\`.
            if entry.contains(MAIN_SEPARATOR) {
                return Err(ConfigError(format!(
                    "ignore entry #{} invalid: must be file or directory name, not path",
                    index + 1
                )));
            }
        }

        // Index URLs must use HTTPS.
        for (index, url) in self.index_urls.iter().enumerate() {
            if !url.starts_with("https://") {
                return Err(ConfigError(format!(
                    "index URL #{} invalid: is not a HTTPS url",
                    index + 1
                )));
            }
        }

        if self.platform.is_empty() {
            self.platform = crate::go_platform();
        }

        Ok(())
    }

    /// Returns the index file path inside `dir`.
    pub fn index_path(&self, dir: &str) -> std::path::PathBuf {
        Path::new(dir).join(&self.index_file)
    }
}
