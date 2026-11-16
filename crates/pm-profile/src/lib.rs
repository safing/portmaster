//! Compatible implementation of the Go `service/profile` package (without SPN).
//!
//! The crate models application profiles: their fingerprints, derived IDs,
//! merged profiles, matching against processes and the profile database
//! records.
//!
//! # Compatibility notes
//!
//! * Endpoint rules are parsed with `pm-endpoints`; [`GlobalProfileConfig`]
//!   keeps the parsed global lists.
//! * [`LayeredProfile`] resolves every option as local value, else global
//!   value, and matches endpoints and filter lists local-first. SPN policies
//!   are not ported.
//! * The profile options are registered by [`options::register_options`].
//!   Profile configs are read through a `pm_config::Perspective`, which also
//!   migrates legacy security-level values like Go.
//! * Binaries (`binmeta` icons) are modelled as data only; fetching icons and
//!   metadata from executables is not ported.
//! * Profile lifecycle events, active-profile caches and database hooks are not
//!   ported; [`ProfileStore`] provides explicit reads and writes.

mod fingerprint;
mod global;
mod icon;
mod layered;
mod matching;
mod merge;
mod option_texts;
pub mod options;
mod profile;
mod proxy_url;
mod store;

pub use fingerprint::{
    derive_profile_id, key_and_value_in_tags, key_in_tags, match_fingerprints, parse_fingerprints,
    sort_and_compact_fingerprints, sort_fingerprints, Fingerprint, MatchingData,
    ParsedFingerprints, Tag, FINGERPRINT_OPERATION_EQUALS, FINGERPRINT_OPERATION_PREFIX,
    FINGERPRINT_OPERATION_REGEX, FINGERPRINT_TYPE_CMDLINE, FINGERPRINT_TYPE_ENV,
    FINGERPRINT_TYPE_PATH, FINGERPRINT_TYPE_TAG,
};
pub use global::{GlobalProfileConfig, ListResolver};
pub use icon::{
    sort_and_compact_icons, Icon, IconSource, IconType, ICON_SOURCE_CORE, ICON_SOURCE_IMPORT,
    ICON_SOURCE_UI, ICON_SOURCE_USER, ICON_TYPE_API, ICON_TYPE_DATABASE, ICON_TYPE_FILE,
};
pub use layered::LayeredProfile;
pub use matching::{find_matching_profile, ProfileMatch};
pub use merge::merge_profiles;
pub use profile::{
    ensure_profile, now, OutdatedFlag, Profile, CFG_OPTION_ENDPOINTS_KEY,
    CFG_OPTION_SERVICE_ENDPOINTS_KEY, DEFAULT_ACTION_ASK, DEFAULT_ACTION_BLOCK,
    DEFAULT_ACTION_NOT_SET, DEFAULT_ACTION_PERMIT, DEFAULT_ACTION_PERMIT_VALUE,
};
pub use proxy_url::validate_upstream_proxy_url;
pub use store::{Error as StoreError, ProfileStore};

/// Profile source: local, editable profiles.
pub const SOURCE_LOCAL: &str = "local";
/// Profile source: special, read-only profiles.
pub const SOURCE_SPECIAL: &str = "special";

/// Source of a profile, a plain string like in Go.
pub type ProfileSource = String;

/// Base database path of profiles (`core:profiles/`).
pub const PROFILES_DB_PATH: &str = "core:profiles/";

/// Profile ID of unidentified processes.
pub const UNIDENTIFIED_PROFILE_ID: &str = "_unidentified";
/// Name of the unidentified profile.
pub const UNIDENTIFIED_PROFILE_NAME: &str = "Other Connections";
/// Profile ID of unsolicited connections.
pub const UNSOLICITED_PROFILE_ID: &str = "_unsolicited";
/// Name of the unsolicited profile.
pub const UNSOLICITED_PROFILE_NAME: &str = "Network Noise";
/// Profile ID of the system/kernel.
pub const SYSTEM_PROFILE_ID: &str = "_system";
/// Name of the system profile.
pub const SYSTEM_PROFILE_NAME: &str = "Operating System";
/// Profile ID of the system resolver.
pub const SYSTEM_RESOLVER_PROFILE_ID: &str = "_system-resolver";
/// Name of the system resolver profile.
pub const SYSTEM_RESOLVER_PROFILE_NAME: &str = "System DNS Client";
/// Profile ID of the Portmaster core itself.
pub const PORTMASTER_PROFILE_ID: &str = "_portmaster";
/// Name of the Portmaster core profile.
pub const PORTMASTER_PROFILE_NAME: &str = "Portmaster Core Service";

/// Returns the scoped profile ID (`source/id`).
pub fn make_scoped_id(source: &str, id: &str) -> String {
    format!("{source}/{id}")
}

/// Returns the database key of a profile (`core:profiles/source/id`).
pub fn make_profile_key(source: &str, id: &str) -> String {
    format!("{PROFILES_DB_PATH}{}", make_scoped_id(source, id))
}

/// Returns whether the ID belongs to a special profile.
pub fn is_special_profile_id(id: &str) -> bool {
    matches!(
        id,
        UNIDENTIFIED_PROFILE_ID
            | UNSOLICITED_PROFILE_ID
            | SYSTEM_PROFILE_ID
            | SYSTEM_RESOLVER_PROFILE_ID
            | PORTMASTER_PROFILE_ID
    )
}

/// Returns the name of a special profile ID.
pub fn special_profile_name(id: &str) -> Option<&'static str> {
    match id {
        UNIDENTIFIED_PROFILE_ID => Some(UNIDENTIFIED_PROFILE_NAME),
        UNSOLICITED_PROFILE_ID => Some(UNSOLICITED_PROFILE_NAME),
        SYSTEM_PROFILE_ID => Some(SYSTEM_PROFILE_NAME),
        SYSTEM_RESOLVER_PROFILE_ID => Some(SYSTEM_RESOLVER_PROFILE_NAME),
        PORTMASTER_PROFILE_ID => Some(PORTMASTER_PROFILE_NAME),
        _ => None,
    }
}
