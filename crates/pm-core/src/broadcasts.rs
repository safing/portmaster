//! Core wiring for `service/broadcasts`.
//!
//! The Go module obtains versions from `service/core`, config values from
//! `base/config`, the device location from `service/netenv` and the YAML file
//! from the intel updater. Those services already exist in the Rust core, so
//! this file only adapts them to the traits exposed by `pm-broadcasts`.

use std::path::PathBuf;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use pm_broadcasts::{
    BroadcastFileSource, BuildVersion, CoordinatesData, LocationData, MatchingEnvironment,
    ModuleAttachment, ResourceVersion, SimpleVersions,
};
use pm_config::Config;
use pm_updates::Updater;

use crate::location::CoreDeviceLocations;
use crate::service::CFG_RELEASE_CHANNEL;

/// Adapts the core's modules to the broadcasts matching data sources.
pub struct CoreBroadcastEnvironment {
    config: Arc<Config>,
    binary_updater: Arc<Updater>,
    intel_updater: Arc<Updater>,
    device_locations: Arc<CoreDeviceLocations>,
    version: String,
}

impl CoreBroadcastEnvironment {
    pub fn new(
        config: Arc<Config>,
        binary_updater: Arc<Updater>,
        intel_updater: Arc<Updater>,
        device_locations: Arc<CoreDeviceLocations>,
    ) -> CoreBroadcastEnvironment {
        CoreBroadcastEnvironment {
            config,
            binary_updater,
            intel_updater,
            device_locations,
            version: env!("CARGO_PKG_VERSION").to_string(),
        }
    }
}

impl MatchingEnvironment for CoreBroadcastEnvironment {
    fn versions(&self) -> SimpleVersions {
        let mut resources = std::collections::BTreeMap::new();
        for artifact in self.binary_updater.get_files().unwrap_or_default() {
            resources.insert(
                artifact.filename,
                ResourceVersion {
                    version: artifact.version,
                },
            );
        }
        for artifact in self.intel_updater.get_files().unwrap_or_default() {
            resources.insert(
                artifact.filename,
                ResourceVersion {
                    version: artifact.version,
                },
            );
        }
        SimpleVersions {
            build: BuildVersion {
                version: self.version.clone(),
            },
            resources,
            channel: self.config.get_as_string(CFG_RELEASE_CHANNEL, "stable")(),
        }
    }

    fn config_values(&self) -> serde_json::Map<String, serde_json::Value> {
        self.config.active_config_values()
    }

    fn location(&self) -> Option<LocationData> {
        let best = self.device_locations.get_internet_location().best()?.clone();
        let location = best.location?;
        Some(LocationData {
            country: location.country.code,
            coordinates: CoordinatesData {
                accuracy_radius: location.coordinates.accuracy_radius,
                latitude: location.coordinates.latitude,
                longitude: location.coordinates.longitude,
            },
            asn: u64::from(location.asn),
            as_org: location.as_org,
            source: best.source.as_str().to_string(),
            source_accuracy: best.source_accuracy,
        })
    }

    fn now(&self) -> i64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|elapsed| elapsed.as_secs() as i64)
            .unwrap_or(0)
    }

    fn local_offset_seconds(&self) -> i32 {
        crate::location::timezone_offset_seconds()
    }
}

/// Resolves `notifications.yaml` from the intel updater, like Go `GetFile`.
pub struct IntelBroadcastFileSource {
    intel_updater: Arc<Updater>,
}

impl IntelBroadcastFileSource {
    pub fn new(intel_updater: Arc<Updater>) -> IntelBroadcastFileSource {
        IntelBroadcastFileSource { intel_updater }
    }
}

impl BroadcastFileSource for IntelBroadcastFileSource {
    fn get_file(&self, name: &str) -> Option<PathBuf> {
        self.intel_updater
            .get_file(name)
            .ok()
            .map(|artifact| PathBuf::from(artifact.path()))
    }
}

/// Minimal adapter for Go `Notification.SyncWithState(module.states)`.
///
/// `pm_status` already derives notifications from module states (the reverse
/// direction) and `pm_mgr::StateMgr` has no attach hook, so a broadcast marked
/// `attachToModule` cannot literally follow the module state's lifecycle in
/// Rust. The attachment is recorded by `pm-broadcasts` for observability and
/// logged here; no notification is silently discarded.
pub struct CoreBroadcastAttachment;

impl ModuleAttachment for CoreBroadcastAttachment {
    fn attach(&self, event_id: &str) {
        tracing::debug!(event_id, "broadcasts: notification attached to module state");
    }
}
