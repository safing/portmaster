//! Update loop of the custom filter list, port of the module part of Go
//! `service/intel/customlists/module.go`.
//!
//! Go keeps the list in package globals and reacts to config changes and a
//! one-minute worker; this type owns the state and exposes
//! [`CustomListService::check_and_update`], which both callers use.

use std::io;
use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use pm_mgr::{State, StateMgr, STATE_TYPE_WARNING};
use pm_notifications::Notifications;

use crate::customlists::{CustomLists, ParseReport};

/// Go `ErrNotConfigured`, returned when no file path is set.
pub const ERR_NOT_CONFIGURED: &str = "custom filter list not configured";

/// State ID for a filter list that could not be opened.
pub const PARSE_WARNING_NOTIFICATION_ID: &str = "customlists:parse-warning";
/// State ID for a filter list with too many invalid lines.
pub const ZERO_IP_NOTIFICATION_ID: &str = "customlists:too-many-zero-ips";
/// Notification ID for the last successful load.
pub const PARSE_STATUS_NOTIFICATION_ID: &str = "customlists:parse-status";

/// What one check did.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum UpdateOutcome {
    /// No file path is configured, like Go `ErrNotConfigured`.
    NotConfigured,
    /// The file did not change since the last successful parse.
    Unchanged,
    /// The file was parsed again.
    Loaded(ParseReport),
}

/// The custom filter list with its file watching state.
pub struct CustomListService {
    lists: CustomLists,
    notifications: Option<Arc<Notifications>>,
    states: Option<Arc<StateMgr>>,
    last_parse: Mutex<LastParse>,
    /// The configured file path, set by the core from the config option.
    configured_path: Mutex<String>,
    /// Serializes checks, like Go `filterListLock` in the writers.
    check_lock: Mutex<()>,
}

/// Path and modification time of the last successful parse.
#[derive(Debug, Default, Clone)]
struct LastParse {
    path: String,
    modified: Option<SystemTime>,
}

impl CustomListService {
    /// Creates a service without states and notifications.
    pub fn new() -> CustomListService {
        CustomListService::with_reporting(None, None)
    }

    /// Creates a service that reports states and notifications, like Go.
    pub fn with_reporting(
        notifications: Option<Arc<Notifications>>,
        states: Option<Arc<StateMgr>>,
    ) -> CustomListService {
        CustomListService {
            lists: CustomLists::new(),
            notifications,
            states,
            last_parse: Mutex::new(LastParse::default()),
            configured_path: Mutex::new(String::new()),
            check_lock: Mutex::new(()),
        }
    }

    /// Returns the parsed lists for lookups.
    pub fn lists(&self) -> &CustomLists {
        &self.lists
    }

    /// Sets the configured file path; the core calls this on config changes.
    pub fn set_file_path(&self, file_path: &str) {
        let mut path = self
            .configured_path
            .lock()
            .unwrap_or_else(|error| error.into_inner());
        *path = file_path.to_string();
    }

    /// Returns the configured file path.
    pub fn file_path(&self) -> String {
        self.configured_path
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Reloads the list from the configured file path.
    pub fn check_and_update_configured(&self) -> io::Result<UpdateOutcome> {
        let file_path = self.file_path();
        self.check_and_update(&file_path)
    }

    /// Reloads the list from `file_path` if it is configured and changed.
    ///
    /// The caller passes the currently configured path; reading the option is
    /// left to the caller because the config value is not `Sync` in `pm-config`.
    pub fn check_and_update(&self, file_path: &str) -> io::Result<UpdateOutcome> {
        let _guard = self
            .check_lock
            .lock()
            .unwrap_or_else(|error| error.into_inner());

        if file_path.is_empty() {
            return Ok(UpdateOutcome::NotConfigured);
        }

        // Go falls back to the current time when the file cannot be stat'ed,
        // which makes the next parse run; do the same with `force`.
        let metadata = std::fs::metadata(file_path);
        let modified = metadata
            .as_ref()
            .ok()
            .and_then(|metadata| metadata.modified().ok());
        let force = metadata.is_err();

        if !force {
            let last = self
                .last_parse
                .lock()
                .unwrap_or_else(|error| error.into_inner());
            if last.path == file_path && last.modified == modified {
                return Ok(UpdateOutcome::Unchanged);
            }
        }

        let report = match self.lists.parse_file(file_path) {
            Ok(Some(report)) => report,
            Ok(None) => return Ok(UpdateOutcome::NotConfigured),
            Err(error) => {
                tracing::warn!("intel/customlists: failed to parse file {error}");
                if let Some(states) = &self.states {
                    states.add(
                        State::new(
                            PARSE_WARNING_NOTIFICATION_ID,
                            "Failed to open custom filter list",
                            STATE_TYPE_WARNING,
                        )
                        .with_message(&error.to_string()),
                    );
                }
                return Err(error);
            }
        };

        if let Some(states) = &self.states {
            // The file was opened, so clear a previous open failure.
            states.remove(PARSE_WARNING_NOTIFICATION_ID);
        }

        if report.too_many_invalid_lines() {
            tracing::warn!("intel/customlists: Too many invalid lines");
            if let Some(states) = &self.states {
                states.add(
                    State::new(
                        ZERO_IP_NOTIFICATION_ID,
                        "Custom filter list has many invalid lines",
                        STATE_TYPE_WARNING,
                    )
                    .with_message(&format!(
                        "{} out of {} lines are invalid.\n\t\t\t Check if you are using the correct file format and if the path to the custom filter list is correct.",
                        report.invalid_lines, report.total_lines
                    )),
                );
            }
        } else if let Some(states) = &self.states {
            states.remove(ZERO_IP_NOTIFICATION_ID);
        }

        tracing::info!(
            "intel/customlists: loaded {} entries from {}",
            report.entries(),
            file_path
        );

        if let Some(notifications) = &self.notifications {
            notifications.notify_info(
                PARSE_STATUS_NOTIFICATION_ID,
                "Custom filter list loaded successfully.",
                &format!(
                    "Custom filter list loaded from file {}:  \n{} Domains  \n{} IPs  \n{} Autonomous Systems  \n{} Countries",
                    file_path, report.domains, report.ips, report.asns, report.countries
                ),
            );
        }

        {
            let mut last = self
                .last_parse
                .lock()
                .unwrap_or_else(|error| error.into_inner());
            last.path = file_path.to_string();
            last.modified = modified;
        }

        Ok(UpdateOutcome::Loaded(report))
    }
}

impl Default for CustomListService {
    fn default() -> Self {
        CustomListService::new()
    }
}
