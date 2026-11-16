//! Snap tag handler.
//!
//! Port of `service/process/tags/snap_unix.go`. Snap metadata is read from the
//! environment first and falls back to parsing the `/snap/<name>/...` path.

use crate::{Process, Tag, TagDescription, TagHandler};

const SNAP_BASE_DIR: &str = "/snap/";

/// Tags processes running from a Snap package.
pub struct SnapHandler;

impl TagHandler for SnapHandler {
    fn name(&self) -> &str {
        "Snap"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![
            TagDescription {
                id: "snap-name".to_string(),
                name: "Snap Name".to_string(),
                description: "Name of snap package.".to_string(),
            },
            TagDescription {
                id: "snap-version".to_string(),
                name: "Snap Version".to_string(),
                description: "Version and revision of the snap package.".to_string(),
            },
        ]
    }

    fn add_tags(&self, process: &mut Process) {
        if let Some(snap_pkg_base_dir) = process.env.get("SNAP") {
            if process.path.starts_with(snap_pkg_base_dir) && add_tags_from_env(process) {
                return;
            }
        }
        add_tags_from_path(process);
    }
}

/// Adds name and version from the environment; returns whether they were found.
fn add_tags_from_env(process: &mut Process) -> bool {
    let (Some(name), Some(version)) = (
        process.env.get("SNAP_NAME"),
        process.env.get("SNAP_VERSION"),
    ) else {
        return false;
    };
    process.tags.push(Tag {
        key: "snap-name".to_string(),
        value: name.clone(),
    });
    process.tags.push(Tag {
        key: "snap-version".to_string(),
        value: version.clone(),
    });
    true
}

/// Adds the package name parsed from a `/snap/<name>/...` path.
fn add_tags_from_path(process: &mut Process) {
    if !process.path.starts_with(SNAP_BASE_DIR) {
        return;
    }
    let rest = &process.path[SNAP_BASE_DIR.len()..];
    let Some((name, _tail)) = rest.split_once('/') else {
        return;
    };
    if name.is_empty() {
        return;
    }
    process.tags.push(Tag {
        key: "snap-name".to_string(),
        value: name.to_string(),
    });
}
