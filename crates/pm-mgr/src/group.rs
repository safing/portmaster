use std::sync::atomic::{AtomicU8, Ordering};
use std::sync::Arc;
use std::time::Duration;

use anyhow::Context;

use crate::manager::Manager;
use crate::module::{Module, ModuleState};

struct GroupItem {
    module: Arc<dyn Module>,
    mgr: Manager,
}

/// Manages an ordered set of modules through their lifecycle.
pub struct Group {
    modules: Vec<GroupItem>,
    state: AtomicU8,
}

impl Group {
    /// Creates a new group from an ordered list of modules.
    pub fn new(modules: Vec<Arc<dyn Module>>) -> Self {
        let items = modules
            .into_iter()
            .map(|module| {
                let mgr = Manager::new(module.name());
                GroupItem { module, mgr }
            })
            .collect();

        Self {
            modules: items,
            state: AtomicU8::new(ModuleState::Off as u8),
        }
    }

    /// Returns the current state of the group.
    pub fn state(&self) -> ModuleState {
        match self.state.load(Ordering::SeqCst) {
            0 => ModuleState::Off,
            1 => ModuleState::Starting,
            2 => ModuleState::Running,
            3 => ModuleState::Stopping,
            _ => ModuleState::Invalid,
        }
    }

    fn set_state(&self, state: ModuleState) {
        self.state.store(state as u8, Ordering::SeqCst);
    }

    /// Returns true if all modules in the group are running.
    pub fn is_running(&self) -> bool {
        self.state() == ModuleState::Running
    }

    /// Returns whether the group is stopped.
    ///
    /// Returns an error if the group is in an `Invalid` state.
    pub fn is_stopped(&self) -> anyhow::Result<bool> {
        let state = self.state();
        if state == ModuleState::Invalid {
            anyhow::bail!("invalid group state: cannot recover");
        }
        Ok(state == ModuleState::Off)
    }

    /// Returns copies of the module references in the group.
    pub fn modules(&self) -> Vec<Arc<dyn Module>> {
        self.modules.iter().map(|item| Arc::clone(&item.module)).collect()
    }

    /// Returns the manager associated with a module by name, if found.
    pub fn manager(&self, name: &str) -> Option<Manager> {
        self.modules
            .iter()
            .find(|item| item.module.name() == name)
            .map(|item| item.mgr.clone())
    }

    /// Starts all modules sequentially in defined order.
    ///
    /// If a module fails to start, itself and all previously started modules
    /// are stopped in reverse order, returning the original error.
    pub async fn start(&self) -> anyhow::Result<()> {
        match self.state() {
            ModuleState::Running => return Ok(()),
            ModuleState::Invalid => {
                anyhow::bail!("invalid group state: cannot recover");
            }
            ModuleState::Off => {
                if self
                    .state
                    .compare_exchange(
                        ModuleState::Off as u8,
                        ModuleState::Starting as u8,
                        Ordering::SeqCst,
                        Ordering::SeqCst,
                    )
                    .is_err()
                {
                    anyhow::bail!("unsuitable group state: {}", self.state());
                }
            }
            other => {
                anyhow::bail!("unsuitable group state: {}", other);
            }
        }

        for (i, item) in self.modules.iter().enumerate() {
            item.mgr.reset();
            tracing::debug!(module = item.module.name(), "starting module");

            if let Err(err) = item.module.start(&item.mgr).await {
                tracing::error!(
                    module = item.module.name(),
                    error = %err,
                    "failed to start module"
                );

                // Stop the failed module and all previous modules in reverse order
                let rollback_ok = self.stop_from(i).await;
                if rollback_ok {
                    self.set_state(ModuleState::Off);
                } else {
                    self.set_state(ModuleState::Invalid);
                }

                return Err(err).context(format!("failed to start {}", item.module.name()));
            }

            tracing::info!(module = item.module.name(), "module started");
        }

        self.set_state(ModuleState::Running);
        Ok(())
    }

    /// Stops all modules in the group in reverse order.
    pub async fn stop(&self) -> anyhow::Result<()> {
        match self.state() {
            ModuleState::Off => return Ok(()),
            ModuleState::Invalid => {
                anyhow::bail!("invalid group state: cannot recover");
            }
            ModuleState::Running => {
                if self
                    .state
                    .compare_exchange(
                        ModuleState::Running as u8,
                        ModuleState::Stopping as u8,
                        Ordering::SeqCst,
                        Ordering::SeqCst,
                    )
                    .is_err()
                {
                    anyhow::bail!("unsuitable group state: {}", self.state());
                }
            }
            other => {
                anyhow::bail!("unsuitable group state: {}", other);
            }
        }

        let ok = if self.modules.is_empty() {
            true
        } else {
            self.stop_from(self.modules.len() - 1).await
        };

        if ok {
            self.set_state(ModuleState::Off);
            Ok(())
        } else {
            self.set_state(ModuleState::Invalid);
            anyhow::bail!("failed to stop group");
        }
    }

    async fn stop_from(&self, from_idx: usize) -> bool {
        if self.modules.is_empty() {
            return true;
        }

        let mut all_ok = true;
        for i in (0..=from_idx).rev() {
            let item = &self.modules[i];
            tracing::debug!(module = item.module.name(), "stopping module");

            // Cancel the manager context so workers see cancellation
            item.mgr.cancel();

            // Run module's stop method
            if let Err(err) = item.module.stop(&item.mgr).await {
                tracing::error!(
                    module = item.module.name(),
                    error = %err,
                    "failed to stop module"
                );
                all_ok = false;
            }

            // Wait for workers to finish
            item.mgr
                .wait_for_workers(Some(Duration::from_secs(5)))
                .await;

            tracing::info!(module = item.module.name(), "module stopped");
        }

        all_ok
    }
}
