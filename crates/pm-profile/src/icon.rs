//! Icons of profiles, modelled like Go `service/profile/binmeta`.

use serde::{Deserialize, Serialize};

/// Type of an icon.
pub type IconType = String;
/// Icon type: file path.
pub const ICON_TYPE_FILE: &str = "path";
/// Icon type: database key.
pub const ICON_TYPE_DATABASE: &str = "database";
/// Icon type: API resource.
pub const ICON_TYPE_API: &str = "api";

/// Source of an icon.
pub type IconSource = String;
/// Icon source: set by the user.
pub const ICON_SOURCE_USER: &str = "user";
/// Icon source: imported from elsewhere.
pub const ICON_SOURCE_IMPORT: &str = "import";
/// Icon source: provided by the UI.
pub const ICON_SOURCE_UI: &str = "ui";
/// Icon source: provided by the core.
pub const ICON_SOURCE_CORE: &str = "core";

/// An icon of a profile.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Icon {
    #[serde(rename = "Type")]
    pub type_: IconType,
    #[serde(rename = "Value")]
    pub value: String,
    #[serde(rename = "Source")]
    pub source: IconSource,
}

impl Icon {
    pub fn new(type_: &str, value: &str, source: &str) -> Icon {
        Icon {
            type_: type_.to_string(),
            value: value.to_string(),
            source: source.to_string(),
        }
    }

    fn sort_order(&self) -> u8 {
        match self.type_.as_str() {
            ICON_TYPE_API => 1,
            ICON_TYPE_DATABASE => 2,
            ICON_TYPE_FILE => 3,
            _ => 9,
        }
    }
}

/// Sorts and de-duplicates icons, like Go `SortAndCompactIcons`.
pub fn sort_and_compact_icons(icons: &mut Vec<Icon>) {
    icons.sort_by(|a, b| (a.sort_order(), &a.value).cmp(&(b.sort_order(), &b.value)));
    icons.dedup_by(|a, b| a.type_ == b.type_ && a.value == b.value);
}
