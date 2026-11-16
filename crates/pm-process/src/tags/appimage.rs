//! AppImage tag handler.
//!
//! Port of `service/process/tags/appimage_unix.go`. Two independent checks are
//! performed: the `APPIMAGE`/`APPDIR` environment pair identifies the AppImage
//! file itself, and a `/tmp/.mount_*` path is cross-referenced with
//! `/proc/mounts` to derive a mount ID.

use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use regex::Regex;

use crate::{Process, Tag, TagDescription, TagHandler};

/// Matches the AppImage mount directory prefix of an executable path.
static MOUNT_DIR_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^/tmp/.mount_[^/]+").unwrap());
/// Extracts the leading alphanumeric ID from a mount source name.
static MOUNT_NAME_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"^[A-Za-z0-9]+").unwrap());

/// Tags processes running from an AppImage.
pub struct AppImageHandler {
    mounts_path: PathBuf,
}

impl Default for AppImageHandler {
    fn default() -> Self {
        Self {
            mounts_path: PathBuf::from("/proc/mounts"),
        }
    }
}

impl AppImageHandler {
    /// Creates a handler that reads an alternate mounts table (for tests).
    pub fn with_mounts_path(path: impl Into<PathBuf>) -> Self {
        Self {
            mounts_path: path.into(),
        }
    }
}

impl TagHandler for AppImageHandler {
    fn name(&self) -> &str {
        "AppImage"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![
            TagDescription {
                id: "app-image-path".to_string(),
                name: "AppImage Path".to_string(),
                description: "Path to the app image file itself.".to_string(),
            },
            TagDescription {
                id: "app-image-mount-id".to_string(),
                name: "AppImage Mount ID".to_string(),
                description:
                    "Extracted ID from the AppImage mount name. Use AppImage Path instead, if available."
                        .to_string(),
            },
        ]
    }

    fn add_tags(&self, process: &mut Process) {
        // Detect the AppImage file via the APPIMAGE/APPDIR environment pair.
        if let (Some(location), Some(mount_dir)) =
            (process.env.get("APPIMAGE"), process.env.get("APPDIR"))
        {
            if !location.is_empty()
                && !mount_dir.is_empty()
                && process.path.starts_with(mount_dir)
            {
                process.matching_path = location.clone();
                process.tags.push(Tag {
                    key: "app-image-path".to_string(),
                    value: location.clone(),
                });
            }
        }

        // Detect the AppImage mount point from the executable path.
        let Some(mount_dir) = MOUNT_DIR_RE
            .find(&process.path)
            .map(|mat| mat.as_str().to_string())
        else {
            return;
        };
        let Some(mount_name) = get_app_image_mount_name(&self.mounts_path, &mount_dir) else {
            return;
        };
        if mount_name.is_empty() {
            return;
        }

        let mount_name = mount_name
            .strip_prefix("gearlever_")
            .unwrap_or(&mount_name);
        let id = MOUNT_NAME_RE
            .find(mount_name)
            .map(|mat| mat.as_str())
            .unwrap_or("");
        if id.is_empty() {
            return;
        }
        process.tags.push(Tag {
            key: "app-image-mount-id".to_string(),
            value: id.to_string(),
        });
    }
}

/// Returns the mount source for `mount_point` if it names an AppImage file,
/// mirroring Go's `getAppImageMountName`.
fn get_app_image_mount_name(mounts_path: &Path, mount_point: &str) -> Option<String> {
    let data = std::fs::read_to_string(mounts_path).ok()?;
    for line in data.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() >= 2
            && fields[1] == mount_point
            && fields[0].to_lowercase().ends_with(".appimage")
        {
            return Some(fields[0].to_string());
        }
    }
    None
}
