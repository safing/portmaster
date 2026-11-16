//! Applying an upgrade, port of Go `service/updates/upgrade.go`.

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use crate::{copy_and_check_sha256_sum, Index, UpdaterConfig};

/// Permissions of public files and directories.
const PUBLIC_READ_PERMISSION: u32 = 0o644;
/// Permissions of public directories.
const PUBLIC_READ_EXEC_PERMISSION: u32 = 0o755;

/// An error while applying an upgrade.
#[derive(Debug, thiserror::Error)]
pub enum UpgradeError {
    #[error("cannot upgrade: no update available")]
    NoUpdateAvailable,
    #[error("failed to create purge directory: {0}")]
    CreatePurgeDirectory(String),
    #[error("read current directory: {0}")]
    ReadCurrentDirectory(String),
    #[error("create current directory: {0}")]
    CreateCurrentDirectory(String),
    #[error("failed to move current file {0} to purge dir: {1}")]
    MoveCurrentFile(String, String),
    #[error("failed to move index file to {0}: {1}")]
    MoveIndexFile(String, String),
    #[error("failed to move file {0}: {1}")]
    MoveFile(String, String),
    #[error("move failed, copy+delete fallback failed: {0}")]
    MoveFallback(String),
    #[error("upgrade failed, but recovery was successful: {0}")]
    Recovered(String),
    #[error("upgrade (including recovery) failed: {0}")]
    UpgradeFailed(String),
    #[error("delete purge dir: {0}")]
    DeletePurgeDir(String),
    #[error("delete download dir: {0}")]
    DeleteDownloadDir(String),
    #[error("post-upgrade command '{0}' failed: {1}, output: {2}")]
    PostUpgradeCommand(String, String, String),
}

/// Applies upgrades to an artifact directory.
///
/// Go's `Updater` also runs workers, events and notifications; this type only
/// owns the index and the file operations of `upgrade.go`. The UI upgrade lock
/// is not part of it and must be taken by the caller.
pub struct Upgrader {
    config: UpdaterConfig,
    index: Mutex<Option<Index>>,
}

impl Upgrader {
    /// Creates an upgrader for the given configuration.
    pub fn new(config: UpdaterConfig) -> Upgrader {
        Upgrader {
            config,
            index: Mutex::new(None),
        }
    }

    /// Returns the configuration.
    pub fn config(&self) -> &UpdaterConfig {
        &self.config
    }

    /// Returns the currently installed index.
    pub fn index(&self) -> Option<Index> {
        self.index
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Sets the currently installed index.
    pub fn set_index(&self, index: Option<Index>) {
        let mut current = self
            .index
            .lock()
            .unwrap_or_else(|error| error.into_inner());
        *current = index;
    }

    /// Applies the downloaded index, moving the new files into place.
    pub fn upgrade(&self, downloaded: &Index, ignore_version: bool) -> Result<(), UpgradeError> {
        let mut current = self
            .index
            .lock()
            .unwrap_or_else(|error| error.into_inner());

        if !ignore_version {
            if let Some(installed) = current.as_ref() {
                if installed.should_upgrade_to(downloaded, now()).is_err() {
                    // Go wraps `ErrNoUpdateAvailable` here and loses the actual
                    // reason; keep the same message.
                    return Err(UpgradeError::NoUpdateAvailable);
                }
            }
        }

        let upgrade_error = match self.upgrade_move_files(downloaded) {
            Ok(()) => {
                // Go installs the new index before running the post-upgrade
                // commands and keeps it even if they fail.
                *current = Some(downloaded.clone());
                self.apply_post_upgrade_commands(downloaded)
            }
            Err(error) => Err(error),
        };

        let Err(upgrade_error) = upgrade_error else {
            return Ok(());
        };

        match self.recover_from_failed_upgrade() {
            Ok(()) => Err(UpgradeError::Recovered(upgrade_error.to_string())),
            Err(_) => Err(UpgradeError::UpgradeFailed(upgrade_error.to_string())),
        }
    }

    /// Moves the current files into the purge directory and the new files in.
    fn upgrade_move_files(&self, downloaded: &Index) -> Result<(), UpgradeError> {
        // Reset the purge directory, so a clean rollback is possible.
        let _ = std::fs::remove_dir_all(&self.config.purge_directory);
        ensure_directory(Path::new(&self.config.purge_directory), PUBLIC_READ_EXEC_PERMISSION)
            .map_err(|error| UpgradeError::CreatePurgeDirectory(error.to_string()))?;

        let directory = Path::new(&self.config.directory);
        match std::fs::read_dir(directory) {
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                // Go re-creates the purge directory here, which is a bug: the
                // current directory is the missing one.
                ensure_directory(directory, PUBLIC_READ_EXEC_PERMISSION)
                    .map_err(|error| UpgradeError::CreateCurrentDirectory(error.to_string()))?;
            }
            Err(error) => return Err(UpgradeError::ReadCurrentDirectory(error.to_string())),
            Ok(entries) => {
                for entry in entries {
                    let entry = entry
                        .map_err(|error| UpgradeError::ReadCurrentDirectory(error.to_string()))?;
                    let name = entry.file_name().to_string_lossy().into_owned();

                    // Leave ignored entries and the purge directory alone.
                    if self.config.ignore.iter().any(|ignored| ignored == &name) {
                        continue;
                    }
                    let source = directory.join(&name);
                    let purge = Path::new(&self.config.purge_directory);
                    if self.config.purge_directory.eq_ignore_ascii_case(
                        &directory.join(&name).to_string_lossy(),
                    ) || source == purge
                    {
                        continue;
                    }

                    let destination = purge.join(&name);
                    self.move_file(&source, &destination, "", PUBLIC_READ_PERMISSION)
                        .map_err(|error| UpgradeError::MoveCurrentFile(name.clone(), error))?;
                }
            }
        }

        // Move the new index file into the main directory.
        let source = Path::new(&self.config.download_directory).join(&self.config.index_file);
        let destination = directory.join(&self.config.index_file);
        self.move_file(&source, &destination, "", PUBLIC_READ_PERMISSION)
            .map_err(|error| UpgradeError::MoveIndexFile(destination.display().to_string(), error))?;

        // Move the downloaded artifacts.
        for artifact in &downloaded.artifacts {
            let source = Path::new(&self.config.download_directory).join(&artifact.filename);
            let destination = directory.join(&artifact.filename);
            self.move_file(
                &source,
                &destination,
                &artifact.sha256,
                artifact.get_file_mode(),
            )
            .map_err(|error| UpgradeError::MoveFile(artifact.filename.clone(), error))?;
        }

        Ok(())
    }

    /// Moves a file and falls back to copying and verifying it.
    fn move_file(
        &self,
        current_path: &Path,
        new_path: &Path,
        sha256sum: &str,
        file_permission: u32,
    ) -> Result<(), String> {
        if std::fs::rename(current_path, new_path).is_ok() {
            set_file_mode(new_path, file_permission);
            return Ok(());
        }

        copy_and_check_sha256_sum(current_path, new_path, sha256sum, file_permission)
            .map_err(|error| error.to_string())
    }

    /// Moves the purged files back, like Go `recoverFromFailedUpgrade`.
    pub fn recover_from_failed_upgrade(&self) -> Result<(), UpgradeError> {
        let purge = Path::new(&self.config.purge_directory);
        let entries =
            std::fs::read_dir(purge).map_err(|error| UpgradeError::ReadCurrentDirectory(
                error.to_string(),
            ))?;

        for entry in entries {
            let entry = entry
                .map_err(|error| UpgradeError::ReadCurrentDirectory(error.to_string()))?;
            let name = entry.file_name();
            let purged_file = purge.join(&name);
            let active_file = Path::new(&self.config.directory).join(&name);
            if let Err(error) =
                self.move_file(&purged_file, &active_file, "", PUBLIC_READ_PERMISSION)
            {
                // Recover as many files as possible.
                tracing::warn!(
                    "updates/{}: failed to roll back file {}: {error}",
                    self.config.name,
                    name.to_string_lossy()
                );
            }
        }

        Ok(())
    }

    /// Removes the purge and download directories.
    pub fn cleanup_after_upgrade(&self) -> Result<(), UpgradeError> {
        std::fs::remove_dir_all(&self.config.purge_directory)
            .map_err(|error| UpgradeError::DeletePurgeDir(error.to_string()))?;
        std::fs::remove_dir_all(&self.config.download_directory)
            .map_err(|error| UpgradeError::DeleteDownloadDir(error.to_string()))?;
        Ok(())
    }

    /// Deletes leftover `.download` and `.copy` files.
    pub fn delete_unfinished_files(&self, dir: &Path) -> std::io::Result<()> {
        for entry in std::fs::read_dir(dir)? {
            let entry = entry?;
            if entry.file_type()?.is_dir() {
                continue;
            }

            let name = entry.file_name().to_string_lossy().into_owned();
            if name.ends_with(".download") || name.ends_with(".copy") {
                let path = dir.join(&name);
                tracing::warn!(
                    "updates/{}: deleting unfinished file: {}",
                    self.config.name,
                    path.display()
                );
                if let Err(error) = std::fs::remove_file(&path) {
                    tracing::error!(
                        "updates/{}: failed to delete unfinished file {}: {error}",
                        self.config.name,
                        path.display()
                    );
                }
            }
        }

        Ok(())
    }

    /// Runs the post-upgrade commands, honoring their artifact triggers.
    fn apply_post_upgrade_commands(&self, installed: &Index) -> Result<(), UpgradeError> {
        if self.config.post_upgrade_commands.is_empty() {
            return Ok(());
        }

        // Full paths of the files that were just upgraded.
        let upgraded_files: BTreeSet<PathBuf> = installed
            .artifacts
            .iter()
            .map(|artifact| Path::new(&self.config.directory).join(&artifact.filename))
            .collect();

        for command in &self.config.post_upgrade_commands {
            if !command.trigger_artifact_fname.is_empty()
                && !upgraded_files.contains(Path::new(&command.trigger_artifact_fname))
            {
                continue;
            }

            let output = std::process::Command::new(&command.command)
                .args(&command.args)
                .output();
            match output {
                Ok(output) if output.status.success() => {}
                Ok(output) => {
                    let combined = String::from_utf8_lossy(&output.stdout).into_owned()
                        + &String::from_utf8_lossy(&output.stderr);
                    if command.fail_on_error {
                        return Err(UpgradeError::PostUpgradeCommand(
                            format!("{} {}", command.command, command.args.join(" ")),
                            exit_status_text(&output.status),
                            combined,
                        ));
                    }
                    tracing::warn!(
                        "updates/{}: post-upgrade command '{} {}' failed, but ignored. Error: {}",
                        self.config.name,
                        command.command,
                        command.args.join(" "),
                        output.status
                    );
                }
                Err(error) => {
                    if command.fail_on_error {
                        return Err(UpgradeError::PostUpgradeCommand(
                            format!("{} {}", command.command, command.args.join(" ")),
                            error.to_string(),
                            String::new(),
                        ));
                    }
                    tracing::warn!(
                        "updates/{}: post-upgrade command '{} {}' failed, but ignored. Error: {error}",
                        self.config.name,
                        command.command,
                        command.args.join(" ")
                    );
                }
            }
        }

        Ok(())
    }
}

/// Formats a process exit like Go's `os/exec` error.
fn exit_status_text(status: &std::process::ExitStatus) -> String {
    match status.code() {
        Some(code) => format!("exit status {code}"),
        None => "signal: killed".to_string(),
    }
}

/// Creates a directory with the given permissions.
fn ensure_directory(dir: &Path, mode: u32) -> std::io::Result<()> {
    std::fs::create_dir_all(dir)?;
    set_file_mode(dir, mode);
    Ok(())
}

/// Sets the file permissions on Unix.
fn set_file_mode(path: &Path, mode: u32) {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode));
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
}

/// Returns the current time in Unix seconds.
fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}
