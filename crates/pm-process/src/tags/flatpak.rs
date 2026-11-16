//! Flatpak tag handler.
//!
//! Port of `service/process/tags/flatpak_unix.go`. Processes running from the
//! Flatpak `/app` space are tagged with their `FLATPAK_ID`.

use crate::{Process, Tag, TagDescription, TagHandler};

/// Tags processes running inside a Flatpak sandbox.
pub struct FlatpakHandler;

impl TagHandler for FlatpakHandler {
    fn name(&self) -> &str {
        "Flatpak"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![TagDescription {
            id: "flatpak-id".to_string(),
            name: "Flatpak ID".to_string(),
            description: "ID of the flatpak.".to_string(),
        }]
    }

    fn add_tags(&self, process: &mut Process) {
        if !process.path.starts_with("/app/") {
            return;
        }
        let Some(flatpak_id) = process.env.get("FLATPAK_ID") else {
            return;
        };
        if flatpak_id.is_empty() {
            return;
        }

        process.matching_path = process.path.clone();
        process.tags.push(Tag {
            key: "flatpak-id".to_string(),
            value: flatpak_id.clone(),
        });
    }
}
