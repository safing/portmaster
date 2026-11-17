//! Creation of the built-in special profiles, port of Go
//! `service/profile/special.go` (`createSpecialProfile`).
//!
//! Special profiles keep a fixed ID, have no fingerprints and carry a default
//! config tailored to their role. They are stored with the local source, like
//! in Go, so they load together with the user profiles.

use serde_json::{json, Value};

use crate::options::{
    CFG_OPTION_BLOCK_INBOUND_KEY, CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY,
    CFG_OPTION_BLOCK_SCOPE_LAN_KEY, CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY, CFG_OPTION_BLOCK_P2P_KEY,
    CFG_OPTION_FILTER_LISTS_KEY, DEFAULT_ACTION_BLOCK_VALUE,
};
use crate::{
    Profile, CFG_OPTION_DEFAULT_ACTION_KEY, CFG_OPTION_ENDPOINTS_KEY,
    CFG_OPTION_SERVICE_ENDPOINTS_KEY, DEFAULT_ACTION_PERMIT_VALUE, SOURCE_LOCAL,
};

/// Creates a special profile with the given fixed ID and executable path.
/// Returns `None` for an unknown ID.
pub fn create_special_profile(profile_id: &str, path: &str) -> Option<Profile> {
    let name = crate::special_profile_name(profile_id)?;
    let mut config = serde_json::Map::new();

    let block_scopes = |config: &mut serde_json::Map<String, Value>| {
        config.insert(CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY.into(), json!(false));
        config.insert(CFG_OPTION_BLOCK_SCOPE_LAN_KEY.into(), json!(false));
        config.insert(CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY.into(), json!(false));
        config.insert(CFG_OPTION_BLOCK_P2P_KEY.into(), json!(false));
        config.insert(CFG_OPTION_BLOCK_INBOUND_KEY.into(), json!(false));
    };

    match profile_id {
        crate::UNIDENTIFIED_PROFILE_ID
        | crate::UNSOLICITED_PROFILE_ID
        | crate::SYSTEM_PROFILE_ID => {}
        crate::SYSTEM_RESOLVER_PROFILE_ID => {
            config.insert(
                CFG_OPTION_DEFAULT_ACTION_KEY.into(),
                json!(DEFAULT_ACTION_PERMIT_VALUE),
            );
            block_scopes(&mut config);
            config.insert(
                CFG_OPTION_SERVICE_ENDPOINTS_KEY.into(),
                json!([
                    "+ Localhost",
                    "+ LAN UDP/5353",
                    "+ LAN UDP/5355",
                    "+ LAN UDP/1900",
                    "- *"
                ]),
            );
            config.insert(CFG_OPTION_FILTER_LISTS_KEY.into(), json!([]));
        }
        crate::PORTMASTER_PROFILE_ID => {
            config.insert(
                CFG_OPTION_DEFAULT_ACTION_KEY.into(),
                json!(DEFAULT_ACTION_PERMIT_VALUE),
            );
            block_scopes(&mut config);
            config.insert(CFG_OPTION_ENDPOINTS_KEY.into(), json!(["+ *"]));
            config.insert(
                CFG_OPTION_SERVICE_ENDPOINTS_KEY.into(),
                json!(["+ Localhost", "+ LAN", "- *"]),
            );
        }
        crate::PORTMASTER_APP_PROFILE_ID => {
            config.insert(
                CFG_OPTION_DEFAULT_ACTION_KEY.into(),
                json!(DEFAULT_ACTION_BLOCK_VALUE),
            );
            config.insert(CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY.into(), json!(false));
            config.insert(CFG_OPTION_BLOCK_SCOPE_LAN_KEY.into(), json!(true));
            config.insert(CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY.into(), json!(true));
            config.insert(CFG_OPTION_BLOCK_P2P_KEY.into(), json!(true));
            config.insert(CFG_OPTION_BLOCK_INBOUND_KEY.into(), json!(true));
            config.insert(
                CFG_OPTION_ENDPOINTS_KEY.into(),
                json!(["+ .safing.io"]),
            );
        }
        crate::PORTMASTER_NOTIFIER_PROFILE_ID => {
            config.insert(
                CFG_OPTION_DEFAULT_ACTION_KEY.into(),
                json!(DEFAULT_ACTION_BLOCK_VALUE),
            );
            config.insert(CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY.into(), json!(false));
            config.insert(CFG_OPTION_BLOCK_SCOPE_LAN_KEY.into(), json!(false));
            config.insert(CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY.into(), json!(false));
            config.insert(CFG_OPTION_BLOCK_P2P_KEY.into(), json!(false));
            config.insert(CFG_OPTION_BLOCK_INBOUND_KEY.into(), json!(true));
            config.insert(
                CFG_OPTION_ENDPOINTS_KEY.into(),
                json!(["+ Localhost"]),
            );
        }
        _ => return None,
    };

    let internal = matches!(
        profile_id,
        crate::PORTMASTER_PROFILE_ID
            | crate::PORTMASTER_APP_PROFILE_ID
            | crate::PORTMASTER_NOTIFIER_PROFILE_ID
    );

    Some(Profile::new(Profile {
        id: profile_id.to_string(),
        source: SOURCE_LOCAL.to_string(),
        name: name.to_string(),
        presentation_path: path.to_string(),
        use_presentation_path: !path.is_empty(),
        config,
        internal,
        ..Profile::default()
    }))
}
