//! Filter lists module glue, port of Go
//! `service/intel/filterlists/module.go`.
//!
//! Go keeps the module state in package globals and reacts to update and
//! netenv events through callbacks. Here the module owns its state manager and
//! exposes methods the core calls from its event subscriptions.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use pm_mgr::{State, StateMgr, STATE_TYPE_WARNING};
use pm_netenv::OnlineStatus;

use crate::{cache_database_version_or_zero, FilterLists, Result};

/// State ID shown while the filter lists are initializing.
pub const FILTERLISTS_DISABLED_STATE: &str = "filterlists:disabled";
/// State ID shown when a list update failed.
pub const FILTERLISTS_UPDATE_FAILED_STATE: &str = "filterlists:update-failed";
/// State ID shown when stale data could not be removed.
pub const FILTERLISTS_STALE_DATA_STATE: &str = "filterlists:staledata";

/// The filter lists module.
pub struct FilterListsModule {
    lists: Arc<FilterLists>,
    states: Arc<StateMgr>,
    ignore_update_events: AtomicBool,
    ignore_netenv_events: AtomicBool,
}

impl FilterListsModule {
    /// Creates the module. Netenv events are ignored by default, like the Go
    /// `init` for tests.
    pub fn new(lists: Arc<FilterLists>, states: Arc<StateMgr>) -> FilterListsModule {
        FilterListsModule {
            lists,
            states,
            ignore_update_events: AtomicBool::new(false),
            ignore_netenv_events: AtomicBool::new(true),
        }
    }

    /// Returns the filter lists.
    pub fn lists(&self) -> &Arc<FilterLists> {
        &self.lists
    }

    /// Returns the state manager.
    pub fn states(&self) -> &Arc<StateMgr> {
        &self.states
    }

    /// Enables or disables handling of update events.
    pub fn set_ignore_update_events(&self, ignore: bool) {
        self.ignore_update_events.store(ignore, Ordering::SeqCst);
    }

    /// Enables or disables handling of netenv events.
    pub fn set_ignore_netenv_events(&self, ignore: bool) {
        self.ignore_netenv_events.store(ignore, Ordering::SeqCst);
    }

    /// Initializes the module, like Go `start`.
    ///
    /// Loads the bloom filters when the cache database has a current version.
    /// Otherwise the lists stay disabled until the first update.
    pub fn start(&self) -> Result<()> {
        let cache_version = cache_database_version_or_zero(self.lists.cache());
        if self
            .lists
            .cache_version()
            .ok()
            .flatten()
            .is_some()
        {
            tracing::debug!(
                "intel/filterlists: cache database has version {cache_version}"
            );
            match self.lists.load_bloom_cache() {
                Ok(()) => {
                    self.lists.set_loaded(true);
                    return Ok(());
                }
                Err(error) => {
                    tracing::debug!(
                        "intel/filterlists: failed to load bloom filters from cache: {error}"
                    );
                }
            }
        } else {
            tracing::debug!("intel/filterlists: blocklists disabled, waiting for update");
        }

        self.warn_about_disabled_filter_lists();
        Ok(())
    }

    /// Stops the module, like Go `stop`.
    pub fn stop(&self) {
        self.lists.set_loaded(false);
    }

    /// Adds the warning shown while the lists are initializing.
    pub fn warn_about_disabled_filter_lists(&self) {
        self.states.add(
            State::new(
                FILTERLISTS_DISABLED_STATE,
                "Filter Lists Are Initializing",
                STATE_TYPE_WARNING,
            )
            .with_message(
                "Filter lists are being downloaded and set up in the background. They will be activated as configured when finished.",
            ),
        );
    }

    /// Handles a resource update event, like the Go callback.
    pub fn handle_resources_updated(
        &self,
        update: impl FnOnce() -> std::result::Result<(), String>,
    ) -> Result<()> {
        if self.ignore_update_events.load(Ordering::SeqCst) {
            return Ok(());
        }
        self.try_update(update)
    }

    /// Handles an online status change, like the Go netenv callback.
    pub fn handle_online_status(
        &self,
        status: OnlineStatus,
        update: impl FnOnce() -> std::result::Result<(), String>,
    ) -> Result<()> {
        if self.ignore_netenv_events.load(Ordering::SeqCst) {
            return Ok(());
        }
        if status == OnlineStatus::Offline {
            return Ok(());
        }
        self.try_update(update)
    }

    /// Runs an update and manages the failure state, like Go `tryListUpdate`.
    pub fn try_update(
        &self,
        update: impl FnOnce() -> std::result::Result<(), String>,
    ) -> Result<()> {
        match update() {
            Ok(()) => {
                self.states.remove(FILTERLISTS_UPDATE_FAILED_STATE);
                Ok(())
            }
            Err(message) => {
                let has_warning = self
                    .states
                    .export()
                    .states
                    .iter()
                    .any(|state| state.type_ == STATE_TYPE_WARNING);
                if !has_warning {
                    self.states.add(
                        State::new(
                            FILTERLISTS_UPDATE_FAILED_STATE,
                            "Filter Lists Update Failed",
                            STATE_TYPE_WARNING,
                        )
                        .with_message(&format!(
                            "The Portmaster failed to process a filter lists update. Filtering capabilities are currently either impaired or not available at all. Error: {message}"
                        )),
                    );
                }
                Err(crate::Error::Message(message))
            }
        }
    }

    /// Adds the stale data warning, like the Go state after a failed cleanup.
    pub fn warn_about_stale_data(&self, error: &str) {
        self.states.add(
            State::new(
                FILTERLISTS_STALE_DATA_STATE,
                "Filter Lists May Overblock",
                STATE_TYPE_WARNING,
            )
            .with_message(&format!(
                "The Portmaster failed to delete outdated filter list data. Filtering capabilities are fully available, but overblocking may occur. Error: {error}"
            )),
        );
    }
}
