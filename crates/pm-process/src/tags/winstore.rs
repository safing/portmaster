//! Windows Store app tag handler.
//!
//! Port of `service/process/tags/winstore_windows.go`. The WindowsApps path
//! from `%ProgramFiles%` is added to the default path on first use.

use std::sync::OnceLock;

use pm_profile::binmeta::{parse_winstore_app_dir, winstore_app_dir};

use crate::{Process, Tag, TagDescription, TagHandler};

const WIN_STORE_NAME: &str = "Windows Store";
const WIN_STORE_APP_NAME_TAG_KEY: &str = "winstore-app-name";
const WIN_STORE_PUBLISHER_ID_TAG_KEY: &str = "winstore-publisher-id";
const DEFAULT_WIN_STORE_PATH: &str = r"C:\Program Files\WindowsApps\";

/// Tags processes running from the Windows Store's `WindowsApps` directory.
pub struct WinStoreHandler;

fn store_paths() -> &'static [String] {
    static PATHS: OnceLock<Vec<String>> = OnceLock::new();
    PATHS.get_or_init(|| {
        let mut paths = vec![DEFAULT_WIN_STORE_PATH.to_string()];
        if let Ok(custom) = std::env::var("ProgramFiles") {
            let custom = format!(r"{custom}\WindowsApps\");
            if !paths.iter().any(|path| path == &custom) {
                paths.push(custom);
            }
        }
        paths
    })
}

impl TagHandler for WinStoreHandler {
    fn name(&self) -> &str {
        WIN_STORE_NAME
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![
            TagDescription {
                id: WIN_STORE_APP_NAME_TAG_KEY.to_string(),
                name: "Windows Store App Name".to_string(),
                description: "Name of the Windows Store App, as found in the executable path."
                    .to_string(),
            },
            TagDescription {
                id: WIN_STORE_PUBLISHER_ID_TAG_KEY.to_string(),
                name: "Windows Store Publisher ID".to_string(),
                description: "Publisher ID of a Windows Store App.".to_string(),
            },
        ]
    }

    fn add_tags(&self, process: &mut Process) {
        let Some(app_dir) = winstore_app_dir(&process.path, store_paths()) else {
            return;
        };
        let Some((name, publisher_id)) = parse_winstore_app_dir(&app_dir) else {
            tracing::debug!(
                app_dir = %app_dir,
                "profile/tags: windows store app has incompatible app dir format"
            );
            return;
        };

        process.tags.push(Tag {
            key: WIN_STORE_APP_NAME_TAG_KEY.to_string(),
            value: name,
        });
        process.tags.push(Tag {
            key: WIN_STORE_PUBLISHER_ID_TAG_KEY.to_string(),
            value: publisher_id,
        });
    }
}
