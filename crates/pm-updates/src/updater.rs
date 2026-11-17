//! The updates module: workers, notifications and the update flow, port of Go
//! `service/updates/module.go`.
//!
//! Go runs the updater through a module manager with two worker managers. Here
//! the caller drives [`Updater::update_check`], [`Updater::upgrade`] and
//! [`Updater::force_update`]; the resource-update event and the state manager
//! are exposed for the core to wire into its own module lifecycle.
//!
//! # Compatibility notes
//!
//! * The Go instance interface (`Restart`, `Notifications`, `UI`) is replaced
//!   by optional notifications/restart handles and a callback for restarting.
//! * The UI upgrade lock is not taken; the UI archive owner does that when it
//!   wires the updater.
//! * The "updating failed" message fixes Go's "udpating failed" typo.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pm_mgr::{EventMgr, State, StateMgr, STATE_TYPE_ERROR};
use pm_notifications::{Action, ActionTypeWebhookPayload, Notification, Notifications};
use serde_json::json;

use crate::{
    downloader::HttpClient, generate_index_from_dir, Error as UpdatesError, Index, IndexScanConfig,
    IndexVerifier, Upgrader, UpdaterConfig,
};

/// How often the update check repeats.
pub const UPDATE_TASK_REPEAT_DURATION: Duration = Duration::from_secs(60 * 60);

/// Notification ID of the "is up to date" info.
pub const NO_NEW_UPDATE_NOTIFICATION_ID: &str = "updates:no-new-update";
/// Notification ID of "update available/ready".
pub const UPDATE_AVAILABLE_NOTIFICATION_ID: &str = "updates:update-available";
/// Notification ID of "restart required".
pub const RESTART_REQUIRED_NOTIFICATION_ID: &str = "updates:restart-required";
/// Notification ID of a failed update.
pub const UPDATE_FAILED_NOTIFICATION_ID: &str = "updates:update-failed";
/// State ID of a corrupted installation.
pub const CORRUPT_INSTALLATION_NOTIFICATION_ID: &str = "updates:corrupt-installation";
/// Event emitted after a successful resource update.
pub const RESOURCE_UPDATE_EVENT: &str = "resource update";

/// Errors of the updater.
#[derive(Debug, thiserror::Error)]
pub enum UpdateError {
    #[error("an updater task is already running, please try again later")]
    Running,
    #[error("no update available")]
    NoUpdateAvailable,
    #[error("no update available: {0}")]
    NoUpdateAvailableDetail(String),
    #[error("action required")]
    ActionRequired,
    #[error("file not found")]
    NotFound,
    #[error("config is invalid: {0}")]
    Config(#[from] crate::ConfigError),
    #[error("create update target directory: {0}")]
    CreateTargetDirectory(String),
    #[error("update index file: {0}")]
    UpdateIndex(String),
    #[error("load previously downloaded index file: {0}")]
    LoadIndex(String),
    #[error("downloading failed: {0}")]
    Downloading(String),
    #[error("upgrade failed: {0}")]
    Upgrade(String),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("{0}")]
    Message(String),
}

/// Result type of the updater.
pub type Result<T> = std::result::Result<T, UpdateError>;

/// Outcome of [`Updater::configure_with_check`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ConfigureResult {
    /// Whether any configuration field actually changed.
    pub changed: bool,
    /// The new automatic-check setting.
    pub auto_check: bool,
    /// Whether an immediate update check should be triggered now.
    ///
    /// This is `true` when something changed and automatic checking is now
    /// enabled. Go re-arms its repeating check worker whenever `Configure`
    /// changes the config while the module is running; Rust leaves scheduling
    /// to the caller, who should run [`Updater::update_check`] when this is
    /// `true` (off the async runtime, as the periodic workers do).
    pub should_check: bool,
}

/// The update module.
pub struct Updater {
    config: Mutex<UpdaterConfig>,
    index: Mutex<Option<Index>>,
    corrupted_installation: Mutex<Option<String>>,
    running: AtomicBool,
    started: AtomicBool,
    states: Arc<StateMgr>,
    notifications: Option<Arc<Notifications>>,
    restart: Option<Arc<dyn Fn() + Send + Sync>>,
    events: EventMgr<()>,
    client: Arc<dyn HttpClient>,
}

impl Updater {
    /// Creates the updater, loads the index and falls back to scanning the
    /// directory, like Go `New`.
    ///
    /// `current_version` is used for the index scan fallback, like Go
    /// `info.VersionNumber()`.
    pub fn new(
        mut config: UpdaterConfig,
        client: Arc<dyn HttpClient>,
        current_version: &str,
        notifications: Option<Arc<Notifications>>,
        restart: Option<Arc<dyn Fn() + Send + Sync>>,
    ) -> Result<Updater> {
        config.check()?;
        ensure_directory(Path::new(&config.directory), 0o755).map_err(|error| {
            // Go wraps the download directory in this error, which looks like a
            // mistake; keep the message shape but report the real error.
            UpdateError::CreateTargetDirectory(format!(
                "{}: {error}",
                config.directory
            ))
        })?;
        let states = Arc::new(StateMgr::new(&config.name));
        let mut corrupted_installation = None;

        let index = match load_index(&config) {
            Ok(index) => {
                if let Err(error) = index.verify_artifacts(&config.directory) {
                    let message = format!("invalid artifact: {error}");
                    tracing::error!("updates/{}: {message}", config.name);
                    corrupted_installation = Some(message);
                }
                Some(index)
            }
            Err(error) => {
                if !is_not_found(&error) {
                    let message = format!("invalid index: {error}");
                    tracing::error!("updates/{}: {message}", config.name);
                    corrupted_installation = Some(message);
                }
                scan_index(&config, current_version)
            }
        };

        Ok(Updater {
            config: Mutex::new(config),
            index: Mutex::new(index),
            corrupted_installation: Mutex::new(corrupted_installation),
            running: AtomicBool::new(false),
            started: AtomicBool::new(false),
            states,
            notifications,
            restart,
            events: EventMgr::new(RESOURCE_UPDATE_EVENT),
            client,
        })
    }

    /// Returns the current configuration.
    pub fn config(&self) -> UpdaterConfig {
        self.config
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Returns the state manager.
    pub fn states(&self) -> &Arc<StateMgr> {
        &self.states
    }

    /// Subscribes to resource update events.
    pub fn subscribe_resource_updates(&self) -> tokio::sync::mpsc::Receiver<()> {
        self.events.subscribe("subscriber", 8)
    }

    /// Returns the main directory.
    pub fn main_dir(&self) -> String {
        self.config().directory
    }

    /// Returns whether the corrupted installation is flagged.
    pub fn corrupted_installation(&self) -> Option<String> {
        self.corrupted_installation
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Adds the corruption state, like Go `Start`.
    pub fn start(&self) {
        let config = self.config();
        let corrupted = self.corrupted_installation();
        if let Some(message) = corrupted {
            if config.notify && self.notifications.is_some() {
                self.states.add(
                    State::new(
                        CORRUPT_INSTALLATION_NOTIFICATION_ID,
                        "Install Corruption",
                        STATE_TYPE_ERROR,
                    )
                    .with_message(&format!(
                        "Portmaster has detected that one or more of its own files have been corrupted. Please re-install the software. Error: {message}"
                    )),
                );
            }
        }
        self.started.store(true, Ordering::SeqCst);
    }

    /// Marks the module as stopped, like Go `Stop`.
    pub fn stop(&self) {
        self.started.store(false, Ordering::SeqCst);
    }

    /// Applies slight configuration changes and returns whether they changed.
    ///
    /// Kept for callers that only need the "did anything change" signal; use
    /// [`Updater::configure_with_check`] to also learn whether an immediate
    /// update check should be triggered.
    pub fn configure(&self, auto_check: bool, index_urls: Vec<String>) -> bool {
        self.apply_config(auto_check, index_urls)
    }

    /// Applies slight configuration changes and reports whether an immediate
    /// update check should be triggered, like Go `Configure`.
    ///
    /// The caller owns the update-check scheduling (the periodic workers live
    /// in pm-core), so this method does not start a task itself. When
    /// [`ConfigureResult::should_check`] is `true`, the caller should run
    /// [`Updater::update_check`] once, e.g. via `spawn_blocking`.
    pub fn configure_with_check(
        &self,
        auto_check: bool,
        index_urls: Vec<String>,
    ) -> ConfigureResult {
        let changed = self.apply_config(auto_check, index_urls);
        ConfigureResult {
            changed,
            auto_check,
            should_check: changed && auto_check,
        }
    }

    fn apply_config(&self, auto_check: bool, index_urls: Vec<String>) -> bool {
        let mut config = self
            .config
            .lock()
            .unwrap_or_else(|error| error.into_inner());
        let mut changed = false;
        if config.auto_check != auto_check {
            config.auto_check = auto_check;
            changed = true;
        }
        if config.index_urls != index_urls {
            config.index_urls = index_urls;
            changed = true;
        }
        changed
    }

    /// Returns a copy of the current index, like Go `GetIndex`.
    pub fn get_index(&self) -> Result<Index> {
        let mut index = self
            .index
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
            .ok_or(UpdateError::NotFound)?;
        index.artifacts = self.exported_files()?;
        Ok(index)
    }

    /// Returns the artifacts with their local paths, like Go `GetFiles`.
    pub fn get_files(&self) -> Result<Vec<crate::Artifact>> {
        self.exported_files()
    }

    /// Returns the artifact with the given file name, like Go `GetFile`.
    pub fn get_file(&self, name: &str) -> Result<crate::Artifact> {
        let config = self.config();
        let index = self.index.lock().unwrap_or_else(|error| error.into_inner());
        let index = index.as_ref().ok_or(UpdateError::NotFound)?;
        index
            .artifacts
            .iter()
            .find(|artifact| {
                artifact.filename == name
                    && (artifact.platform.is_empty() || artifact.platform == config.platform)
            })
            .map(|artifact| {
                artifact.export(&config.directory, index.version_num.as_ref())
            })
            .ok_or(UpdateError::NotFound)
    }

    fn exported_files(&self) -> Result<Vec<crate::Artifact>> {
        let config = self.config();
        let index = self.index.lock().unwrap_or_else(|error| error.into_inner());
        let index = index.as_ref().ok_or(UpdateError::NotFound)?;
        let export: Vec<crate::Artifact> = index
            .artifacts
            .iter()
            .filter(|artifact| {
                artifact.platform.is_empty() || artifact.platform == config.platform
            })
            .map(|artifact| artifact.export(&config.directory, index.version_num.as_ref()))
            .collect();
        if export.is_empty() {
            return Err(UpdateError::NotFound);
        }
        Ok(export)
    }

    /// Runs the update check, like Go `updateCheckWorker`.
    pub fn update_check(&self) -> Result<()> {
        let urls = self.config().index_urls;
        match self.update_and_upgrade(&urls, false, false) {
            Ok(()) => Ok(()),
            Err(UpdateError::NoUpdateAvailable | UpdateError::NoUpdateAvailableDetail(_)) => Ok(()),
            Err(UpdateError::ActionRequired) if !self.config().notify => Err(UpdateError::Message(
                "user action required, but notifying user is disabled: action required".to_string(),
            )),
            Err(error) => Err(UpdateError::Message(format!(
                "updating failed: {error}"
            ))),
        }
    }

    /// Runs the upgrade worker, like Go `upgradeWorker`.
    pub fn upgrade(&self) -> Result<()> {
        let urls = self.config().index_urls;
        match self.update_and_upgrade(&urls, false, true) {
            Ok(()) => Ok(()),
            Err(UpdateError::NoUpdateAvailable | UpdateError::NoUpdateAvailableDetail(_)) => Ok(()),
            Err(UpdateError::ActionRequired) if !self.config().notify => Err(UpdateError::Message(
                "user action required, but notifying user is disabled: action required".to_string(),
            )),
            Err(error) => Err(UpdateError::Message(format!(
                "updating failed: {error}"
            ))),
        }
    }

    /// Runs a forced update and upgrade, like Go `ForceUpdate`.
    pub fn force_update(&self) -> Result<()> {
        let urls = self.config().index_urls;
        self.update_and_upgrade(&urls, true, true)
    }

    /// Downloads and applies an update, like Go `updateAndUpgrade`.
    fn update_and_upgrade(
        &self,
        index_urls: &[String],
        mut ignore_version: bool,
        force_apply: bool,
    ) -> Result<()> {
        if self.running.swap(true, Ordering::SeqCst) {
            return Err(UpdateError::Running);
        }
        let _guard = RunningGuard(&self.running);

        let config = self.config();
        let repair_corrupted_resources = self.corrupted_installation().is_some()
            && config.auto_apply
            && !config.needs_restart;
        if repair_corrupted_resources {
            ignore_version = true;
            tracing::warn!("updates/{}: repairing corrupted resources", config.name);
        }

        let mut downloader = crate::Downloader::new(index_urls.to_vec());
        let downloaded = if index_urls.is_empty() {
            // Go `updateAndUpgrade` loads the staged index from the download
            // directory when no index URLs are configured, so that
            // `ShouldUpgradeTo` compares the installed index against the
            // staged one. Loading from the main directory here would compare
            // the index against itself and never see the staged update.
            load_downloaded_index(&config).map_err(|error| {
                UpdateError::LoadIndex(error.to_string())
            })?
        } else {
            let verifier = config
                .verify
                .as_ref()
                .map(pm_jess::index_verifier);
            downloader
                .update_index(
                    self.client.as_ref(),
                    Path::new(&config.download_directory),
                    &config.index_file,
                    &config.platform,
                    verifier.as_ref().map(|verifier| verifier as IndexVerifier<'_>),
                )
                .map_err(|error| UpdateError::UpdateIndex(error.to_string()))?
        };

        let current = self
            .index
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone();
        if !ignore_version {
            if let Some(current) = &current {
                if let Err(error) = current.should_upgrade_to(&downloaded, now()) {
                    if matches!(error, UpdatesError::SameIndex) {
                        tracing::info!("updates/{}: no new update", config.name);
                        self.notify(
                            Notification::info(
                                NO_NEW_UPDATE_NOTIFICATION_ID,
                                "Portmaster Is Up-To-Date",
                                &format!(
                                    "Portmaster v{} is the newest version.",
                                    current.version
                                ),
                            )
                            .with_action(Action::new("ack", "OK"))
                            .with_expiry(now() + 60),
                        );
                    } else {
                        tracing::warn!("updates/{}: cannot update: {error}", config.name);
                        self.notify(
                            Notification::info(
                                NO_NEW_UPDATE_NOTIFICATION_ID,
                                "Portmaster Is Up-To-Date*",
                                &format!(
                                    "While Portmaster v{} is the newest version, there is an internal issue with checking for updates: {error}",
                                    current.version
                                ),
                            )
                            .with_action(Action::new("ack", "OK"))
                            .with_expiry(now() + 60),
                        );
                    }
                    return Err(UpdateError::NoUpdateAvailableDetail(error.to_string()));
                }
            }
        }

        if !config.auto_download && !force_apply {
            tracing::info!(
                "updates/{}: new update to v{} available, action required to download and upgrade",
                config.name,
                downloaded.version
            );
            self.notify(
                Notification::info(
                    UPDATE_AVAILABLE_NOTIFICATION_ID,
                    "New Update Available",
                    &format!(
                        "Portmaster v{} is available. Click Upgrade to download and upgrade now.",
                        downloaded.version
                    ),
                )
                .with_action(Action::new("ack", "OK"))
                .with_action(upgrade_action())
                .with_expiry(now() + 60),
            );
            return Err(UpdateError::ActionRequired);
        }

        // Look for existing files before downloading.
        for dir in [
            config.directory.as_str(),
            config.download_directory.as_str(),
            config.purge_directory.as_str(),
        ] {
            if let Err(error) = downloader.gather_existing_files(Path::new(dir)) {
                tracing::debug!("updates/{}: gathering existing files: {error}", config.name);
            }
        }

        tracing::info!(
            "updates/{}: downloading new version: {} {}",
            config.name,
            downloaded.name,
            downloaded.version
        );
        if let Err(error) = downloader.download_artifacts(
            self.client.as_ref(),
            Path::new(&config.download_directory),
            &downloaded,
        ) {
            tracing::error!("updates/{}: failed to download update: {error}", config.name);
            let upgrader = Upgrader::new(config.clone());
            if let Err(cleanup_error) =
                upgrader.delete_unfinished_files(Path::new(&config.download_directory))
            {
                tracing::debug!(
                    "updates/{}: failed to delete unfinished files in download directory: {cleanup_error}",
                    config.name
                );
            }
            return Err(UpdateError::Downloading(error.to_string()));
        }

        if !config.auto_apply && !force_apply {
            tracing::info!(
                "updates/{}: new update to v{} available, action required to upgrade",
                config.name,
                downloaded.version
            );
            self.notify(
                Notification::info(
                    UPDATE_AVAILABLE_NOTIFICATION_ID,
                    "New Update Ready",
                    &format!(
                        "Portmaster v{} is available. Click Upgrade to upgrade now.",
                        downloaded.version
                    ),
                )
                .with_action(Action::new("ack", "OK"))
                .with_action(upgrade_action())
                .with_expiry(now() + 60),
            );
            return Err(UpdateError::ActionRequired);
        }

        let upgrader = Upgrader::new(config.clone());
        upgrader.set_index(current);
        if let Err(error) = upgrader.upgrade(&downloaded, ignore_version) {
            if let Err(cleanup_error) =
                upgrader.delete_unfinished_files(Path::new(&config.purge_directory))
            {
                tracing::debug!(
                    "updates/{}: failed to delete unfinished files in purge directory: {cleanup_error}",
                    config.name
                );
            }
            return Err(UpdateError::Upgrade(error.to_string()));
        }

        // Install is complete: clean up and notify the modules.
        if let Err(error) = upgrader.cleanup_after_upgrade() {
            tracing::debug!("updates/{}: failed to clean up after upgrade: {error}", config.name);
        }
        *self
            .index
            .lock()
            .unwrap_or_else(|error| error.into_inner()) = upgrader.index();
        if repair_corrupted_resources {
            *self
                .corrupted_installation
                .lock()
                .unwrap_or_else(|error| error.into_inner()) = None;
            self.states.remove(CORRUPT_INSTALLATION_NOTIFICATION_ID);
        }
        self.events.submit(());

        if !config.needs_restart {
            return Ok(());
        }

        if config.notify {
            self.notify(
                Notification::info(
                    RESTART_REQUIRED_NOTIFICATION_ID,
                    "Restart Required",
                    &format!(
                        "Portmaster v{} is installed. Restart to use new version.",
                        downloaded.version
                    ),
                )
                .with_action(Action::new("ack", "Later"))
                .with_action(restart_action())
                .with_expiry(now() + 60),
            );
            return Err(UpdateError::ActionRequired);
        }

        if let Some(restart) = &self.restart {
            restart();
        }
        Ok(())
    }

    /// Sends a notification, if notifications are enabled and available.
    fn notify(&self, notification: Notification) {
        let config = self.config();
        if !config.notify {
            return;
        }
        if let Some(notifications) = &self.notifications {
            notifications.notify(notification);
        }
    }
}

/// Resets the running flag when the update flow ends.
struct RunningGuard<'a>(&'a AtomicBool);

impl Drop for RunningGuard<'_> {
    fn drop(&mut self) {
        self.0.store(false, Ordering::SeqCst);
    }
}

/// Loads the installed index from the main directory, like Go `New`.
fn load_index(config: &UpdaterConfig) -> std::result::Result<Index, UpdatesError> {
    let verifier = config
        .verify
        .as_ref()
        .map(pm_jess::index_verifier);
    Index::load(
        Path::new(&config.directory).join(&config.index_file),
        &config.platform,
        verifier.as_ref().map(|verifier| verifier as IndexVerifier<'_>),
    )
}

/// Loads the staged index from the download directory, like the offline
/// branch of Go `updateAndUpgrade`.
fn load_downloaded_index(config: &UpdaterConfig) -> std::result::Result<Index, UpdatesError> {
    let verifier = config
        .verify
        .as_ref()
        .map(pm_jess::index_verifier);
    Index::load(
        Path::new(&config.download_directory).join(&config.index_file),
        &config.platform,
        verifier.as_ref().map(|verifier| verifier as IndexVerifier<'_>),
    )
}

/// Scans the directory for an index, like the Go fallback in `New`.
fn scan_index(config: &UpdaterConfig, current_version: &str) -> Option<Index> {
    match generate_index_from_dir(
        &config.directory,
        &IndexScanConfig {
            name: config.name.clone(),
            version: current_version.to_string(),
            ..IndexScanConfig::default()
        },
    ) {
        Ok(mut index) => {
            if let Err(error) = index.init(&config.platform) {
                tracing::error!("updates/{}: invalid scanned index: {error}", config.name);
                return None;
            }
            Some(index)
        }
        Err(error) => {
            tracing::error!("updates/{}: scanning directory failed: {error}", config.name);
            None
        }
    }
}

/// Returns whether the error means that the index file does not exist.
fn is_not_found(error: &UpdatesError) -> bool {
    matches!(error, UpdatesError::ReadIndex(error) if error.kind() == std::io::ErrorKind::NotFound)
}

/// The "Upgrade Now" action, like Go.
fn upgrade_action() -> Action {
    Action::new("upgrade", "Upgrade Now").with_type(
        pm_notifications::ACTION_TYPE_WEBHOOK,
        serde_json::to_value(ActionTypeWebhookPayload {
            method: "POST".to_string(),
            url: "updates/apply".to_string(),
            payload: json!({}),
            result_action: String::new(),
        })
        .unwrap_or_default(),
    )
}

/// The "Restart Now" action, like Go.
fn restart_action() -> Action {
    Action::new("restart", "Restart Now").with_type(
        pm_notifications::ACTION_TYPE_WEBHOOK,
        serde_json::to_value(ActionTypeWebhookPayload {
            method: "POST".to_string(),
            url: "core/restart?source=upgrade".to_string(),
            payload: json!({}),
            result_action: String::new(),
        })
        .unwrap_or_default(),
    )
}

/// Creates a directory with the given permissions.
fn ensure_directory(dir: &Path, mode: u32) -> std::io::Result<()> {
    std::fs::create_dir_all(dir)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(dir, std::fs::Permissions::from_mode(mode));
    }
    #[cfg(not(unix))]
    {
        let _ = mode;
    }
    Ok(())
}

/// Returns the current time in Unix seconds.
fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

/// Returns the index file path of the configuration.
pub fn index_file_path(config: &UpdaterConfig) -> PathBuf {
    Path::new(&config.directory).join(&config.index_file)
}
