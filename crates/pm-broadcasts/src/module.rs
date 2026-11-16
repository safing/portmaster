//! Module glue, ported from Go `service/broadcasts/module.go`.

use std::sync::{Arc, Mutex};

use pm_database::{Error as DatabaseError, Interface};
use pm_notifications::Notifications;
use pm_record::accessor::JsonAccessor;

use crate::data::{BroadcastFileSource, MatchingEnvironment, ModuleAttachment};
use crate::handler::{handle_broadcast, HandleResult};
use crate::install_info::InstallInfo;
use crate::source::{parse_broadcast_source, BroadcastNotifications, BROADCASTS_RESOURCE_NAME};
use crate::state::{BroadcastStates, BROADCAST_STATES_DB_KEY};
use crate::{api, Error, Result};

// Keys from Go packages that are not part of the Rust port yet. They are kept
// as literals so `broadcasts/reset-state` clears the same records as Go.
const IVPN_SUPPRESSED_DB_KEY: &str = "core:notifications/interop/ivpn/suppressed";
const RESOLVER_STALE_CACHE_SUPPRESSED_DB_KEY: &str =
    "core:notifications/resolver/StaleCache/suppressed";

/// Wraps the core's attachment adapter so that requested attachments are also
/// observable from tests.
struct AttachmentRecorder {
    delegate: Arc<dyn ModuleAttachment>,
    recorded: Mutex<Vec<String>>,
}

impl ModuleAttachment for AttachmentRecorder {
    fn attach(&self, event_id: &str) {
        self.recorded
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .push(event_id.to_string());
        self.delegate.attach(event_id);
    }
}

/// The broadcasts module.
pub struct Broadcasts {
    pub(crate) db: Interface,
    pub(crate) notifications: Arc<Notifications>,
    pub(crate) env: Arc<dyn MatchingEnvironment>,
    pub(crate) files: Arc<dyn BroadcastFileSource>,
    pub(crate) attachment: Arc<AttachmentRecorder>,
    pub(crate) started_at: Mutex<i64>,
    /// Serializes acknowledgement state writes, like Go's
    /// `savingBroadcastStateLock`.
    state_lock: Mutex<()>,
}

impl Broadcasts {
    pub fn new(
        db: Interface,
        notifications: Arc<Notifications>,
        env: Arc<dyn MatchingEnvironment>,
        files: Arc<dyn BroadcastFileSource>,
        attachment: Arc<dyn ModuleAttachment>,
    ) -> Broadcasts {
        let started_at = env.now();
        Broadcasts {
            db,
            notifications,
            env,
            files,
            attachment: Arc::new(AttachmentRecorder {
                delegate: attachment,
                recorded: Mutex::new(Vec::new()),
            }),
            started_at: Mutex::new(started_at),
            state_lock: Mutex::new(()),
        }
    }

    /// Overrides the recorded start time; used by tests to control uptime.
    pub fn set_started_at(&self, seconds: i64) {
        *self
            .started_at
            .lock()
            .unwrap_or_else(|error| error.into_inner()) = seconds;
    }

    /// Registers the three broadcasts API endpoints.
    pub fn register_api(self: &Arc<Self>, api_handle: &pm_api::Api) -> Result<()> {
        api::register_api(self, api_handle)
    }

    /// Ensures the install info record exists and is complete, like Go
    /// `ensureInstallInfo`.
    pub fn ensure_install_info(&self) -> Result<()> {
        let now = self.env.now();
        let mut install_info = match InstallInfo::get(&self.db) {
            Ok(info) => info,
            Err(Error::Database(DatabaseError::NotFound)) => InstallInfo::default(),
            Err(error) => {
                tracing::warn!(%error, "updates: failed to load install info");
                InstallInfo::default()
            }
        };

        let build_version = self.env.versions().build.version;
        install_info.check_all(&self.db, &build_version, now);
        install_info.save(&self.db)
    }

    /// Runs one broadcast notification pass, like Go `broadcastNotify`.
    pub fn run_once(self: &Arc<Self>) -> Result<()> {
        let broadcasts = self.load_source()?;
        let matching = self.matching_accessor();
        let mut states = BroadcastStates::get_or_new(&self.db)?;
        let now = self.env.now();

        for broadcast in broadcasts.notifications.values() {
            let module = Arc::clone(self);
            let on_ack = move |broadcast_id: String, action_id: String| {
                match module.mark_broadcast_as_read(&broadcast_id, &action_id) {
                    Ok(()) => Ok(()),
                    Err(error) => {
                        tracing::warn!(%error, broadcast_id, "broadcasts: failed to mark as read");
                        Err(error.to_string())
                    }
                }
            };
            let result = handle_broadcast(
                broadcast,
                &matching,
                Some(&mut states),
                &self.notifications,
                self.attachment.as_ref(),
                on_ack,
                now,
            );
            match result {
                HandleResult::Displayed => {
                    tracing::info!(id = %broadcast.id, "broadcasts: displaying broadcast");
                }
                HandleResult::Skipped(reason) => {
                    tracing::trace!(id = %broadcast.id, %reason, "broadcasts: skipped broadcast");
                }
                HandleResult::Failed(error) => {
                    tracing::warn!(id = %broadcast.id, %error, "broadcasts: failed to handle broadcast");
                }
            }
        }
        Ok(())
    }

    /// Simulates a source file, like Go `handleSimulate`.
    pub fn simulate(self: &Arc<Self>, yaml: &[u8], use_state: bool) -> Result<String> {
        let broadcasts: BroadcastNotifications =
            parse_broadcast_source(yaml).map_err(|error| {
                Error::Message(format!(
                    "failed to parse broadcast notifications update: {error}"
                ))
            })?;
        let matching = self.matching_accessor();
        let mut states = if use_state {
            Some(BroadcastStates::get_or_new(&self.db)?)
        } else {
            None
        };
        let now = self.env.now();

        let mut results = Vec::new();
        for broadcast in broadcasts.notifications.values() {
            let module = Arc::clone(self);
            let on_ack = move |broadcast_id: String, action_id: String| {
                match module.mark_broadcast_as_read(&broadcast_id, &action_id) {
                    Ok(()) => Ok(()),
                    Err(error) => {
                        tracing::warn!(%error, broadcast_id, "broadcasts: failed to mark as read");
                        Err(error.to_string())
                    }
                }
            };
            let result = handle_broadcast(
                broadcast,
                &matching,
                states.as_mut(),
                &self.notifications,
                self.attachment.as_ref(),
                on_ack,
                now,
            );
            let line = match result {
                HandleResult::Displayed => format!("{:>30}: displayed", broadcast.id),
                HandleResult::Skipped(reason) => format!("{:>30}: {reason}", broadcast.id),
                HandleResult::Failed(error) => format!("FAILED {:>23}: {error}", broadcast.id),
            };
            results.push(line);
        }
        Ok(results.join("\n"))
    }

    /// Deletes broadcast and related notification states, like Go
    /// `handleResetState`.
    pub fn reset_state(&self) -> Result<String> {
        match self.db.delete(BROADCAST_STATES_DB_KEY) {
            Ok(()) => {}
            Err(Error::Database(DatabaseError::NotFound)) => {}
            Err(error) => return Err(error),
        }
        // The other two notifications are owned by packages that are not
        // ported yet; Go ignores any error from deleting them.
        let _ = self.db.delete(IVPN_SUPPRESSED_DB_KEY);
        let _ = self.db.delete(RESOLVER_STALE_CACHE_SUPPRESSED_DB_KEY);
        Ok("Reset complete. Some notifications require a restart to reappear.".to_string())
    }

    fn load_source(&self) -> Result<BroadcastNotifications> {
        let path = self.files.get_file(BROADCASTS_RESOURCE_NAME).ok_or_else(|| {
            Error::Message(
                "failed to get broadcast notifications update: resource not found".to_string(),
            )
        })?;
        let data = std::fs::read(&path).map_err(|error| {
            Error::Message(format!("failed to load broadcast notifications update: {error}"))
        })?;
        parse_broadcast_source(&data).map_err(|error| {
            Error::Message(format!("failed to parse broadcast notifications update: {error}"))
        })
    }

    fn matching_accessor(&self) -> JsonAccessor {
        JsonAccessor::new(self.collect_data())
    }

    fn mark_broadcast_as_read(&self, broadcast_id: &str, action_id: &str) -> Result<()> {
        let _guard = self.state_lock.lock().unwrap_or_else(|error| error.into_inner());

        match action_id {
            "ack" => {}
            "" => {
                return Err(Error::Message(format!(
                    "no action ID for {broadcast_id}"
                )))
            }
            other => {
                return Err(Error::Message(format!(
                    "unexpected action ID for {broadcast_id}: {other}"
                )))
            }
        }

        let mut states = BroadcastStates::get_or_new(&self.db)?;
        let state = states.states.entry(broadcast_id.to_string()).or_default();

        // Delete to allow for timely repeats.
        self.notifications.delete(&format!(
            "{}{}",
            crate::source::BROADCAST_NOTIFICATION_ID_PREFIX,
            broadcast_id
        ));

        tracing::info!(broadcast_id, "broadcasts: user acknowledged broadcast");
        state.read = self.env.now();
        states.save(&self.db)
    }

    /// Event IDs of notifications that requested module attachment.
    pub fn attached_notifications(&self) -> Vec<String> {
        self.attachment
            .recorded
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }
}
