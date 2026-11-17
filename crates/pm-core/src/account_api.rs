//! Local/free account endpoints from the user's Go fork (`service/core/account.go`).
//!
//! The UI checks these endpoints to decide whether network history and
//! bandwidth views are available. This port reports the local plan with the
//! `history` and `bw-vis` features enabled, without any external account
//! service.

use std::sync::LazyLock;

use pm_api::{Api, Endpoint, Result, PERMIT_USER};
use serde::Serialize;
use serde_json::json;

/// Key of the local user profile record.
pub const USER_RECORD_KEY: &str = "core:spn/account/user";

/// Local user plan exposed to the UI.
#[derive(Debug, Clone, Serialize)]
pub struct UserPlan {
    #[serde(rename = "name")]
    pub name: String,
    #[serde(rename = "feature_ids")]
    pub feature_ids: Vec<String>,
}

/// Display flags of the account view.
#[derive(Debug, Clone, Serialize)]
pub struct UserProfileView {
    #[serde(rename = "Message")]
    pub message: String,
    #[serde(rename = "ShowAccountData")]
    pub show_account_data: bool,
    #[serde(rename = "ShowAccountButton")]
    pub show_account_button: bool,
    #[serde(rename = "ShowLoginButton")]
    pub show_login_button: bool,
    #[serde(rename = "ShowRefreshButton")]
    pub show_refresh_button: bool,
    #[serde(rename = "ShowLogoutButton")]
    pub show_logout_button: bool,
}

/// The local user profile, like Go `UserProfileRecord`.
#[derive(Debug, Clone, Serialize)]
pub struct UserProfile {
    #[serde(rename = "username")]
    pub username: String,
    #[serde(rename = "state")]
    pub state: String,
    #[serde(rename = "current_plan")]
    pub current_plan: UserPlan,
    #[serde(rename = "view")]
    pub view: UserProfileView,
}

/// Returns the local profile with history and bandwidth enabled.
pub fn local_user_profile() -> UserProfile {
    UserProfile {
        username: "local".to_string(),
        state: "approved".to_string(),
        current_plan: UserPlan {
            name: "Local".to_string(),
            feature_ids: vec![
                "bw-vis".to_string(),
                "history".to_string(),
                "spn".to_string(),
                "xray".to_string(),
                "splittun".to_string(),
            ],
        },
        view: UserProfileView {
            message: String::new(),
            show_account_data: false,
            show_account_button: false,
            show_login_button: false,
            show_refresh_button: false,
            show_logout_button: false,
        },
    }
}

/// Package badge of an account feature.
#[derive(Debug, Clone, Serialize)]
pub struct AccountPackage {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "HexColor")]
    pub hex_color: String,
}

/// An account feature exposed in the settings UI.
#[derive(Debug, Clone, Serialize)]
pub struct AccountFeature {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "RequiredFeatureID")]
    pub required_feature_id: String,
    #[serde(rename = "ConfigKey", skip_serializing_if = "String::is_empty")]
    pub config_key: String,
    #[serde(rename = "ConfigScope", skip_serializing_if = "String::is_empty")]
    pub config_scope: String,
    #[serde(rename = "InPackage", skip_serializing_if = "Option::is_none")]
    pub in_package: Option<AccountPackage>,
    #[serde(rename = "Comment", skip_serializing_if = "String::is_empty")]
    pub comment: String,
    #[serde(rename = "Beta", skip_serializing_if = "std::ops::Not::not")]
    pub beta: bool,
    #[serde(rename = "ComingSoon", skip_serializing_if = "std::ops::Not::not")]
    pub coming_soon: bool,
    /// SVG icon, not serialized in the feature list (served by its own endpoint).
    #[serde(skip)]
    pub icon: String,
}

fn free() -> Option<AccountPackage> {
    Some(AccountPackage {
        name: "Free".to_string(),
        hex_color: "#ffffff".to_string(),
    })
}

fn plus() -> Option<AccountPackage> {
    Some(AccountPackage {
        name: "Plus".to_string(),
        hex_color: "#2fcfae".to_string(),
    })
}

const ICON_DNS: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" /></svg>"#;
const ICON_FILTER: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" /></svg>"#;
const ICON_HISTORY: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25" /></svg>"#;
const ICON_BANDWIDTH: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25c-.621 0-1.125-.504-1.125-1.125V4.125z" /></svg>"#;
const ICON_SPLITTUN: &str = r#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" stroke="currentColor" fill="none"><g stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"><path d="M8.5 12h3" /><path d="M11 12c4-2 6-4 6.5-4.5" /><path d="M11 12c4 2 6 4 6.5 4.5" /><circle cx="19" cy="6" r="2" /><circle cx="19" cy="18" r="2" /><circle cx="4" cy="12" r="2" /></g></svg>"#;

static FEATURES: LazyLock<Vec<AccountFeature>> = LazyLock::new(|| {
    vec![
        AccountFeature {
            name: "Secure DNS".to_string(),
            id: "dns".to_string(),
            required_feature_id: String::new(),
            config_key: String::new(),
            config_scope: "dns/".to_string(),
            in_package: free(),
            comment: String::new(),
            beta: false,
            coming_soon: false,
            icon: ICON_DNS.to_string(),
        },
        AccountFeature {
            name: "Privacy Filter".to_string(),
            id: "filter".to_string(),
            required_feature_id: String::new(),
            config_key: String::new(),
            config_scope: "filter/".to_string(),
            in_package: free(),
            comment: String::new(),
            beta: false,
            coming_soon: false,
            icon: ICON_FILTER.to_string(),
        },
        AccountFeature {
            name: "Network History".to_string(),
            id: "history".to_string(),
            required_feature_id: "history".to_string(),
            config_key: "history/enable".to_string(),
            config_scope: "history/".to_string(),
            in_package: plus(),
            comment: String::new(),
            beta: false,
            coming_soon: false,
            icon: ICON_HISTORY.to_string(),
        },
        AccountFeature {
            name: "Bandwidth Visibility".to_string(),
            id: "bw-vis".to_string(),
            required_feature_id: "bw-vis".to_string(),
            config_key: String::new(),
            config_scope: String::new(),
            in_package: plus(),
            comment: String::new(),
            beta: true,
            coming_soon: false,
            icon: ICON_BANDWIDTH.to_string(),
        },
        AccountFeature {
            name: "Split Tunneling".to_string(),
            id: "splittun".to_string(),
            required_feature_id: String::new(),
            config_key: "splittun/enable".to_string(),
            config_scope: "splittun/".to_string(),
            in_package: free(),
            comment: String::new(),
            beta: false,
            coming_soon: false,
            icon: ICON_SPLITTUN.to_string(),
        },
    ]
});

/// Returns the registered features.
pub fn features() -> &'static [AccountFeature] {
    &FEATURES
}

/// Registers the account endpoints, like the user's Go fork does.
pub fn register_api(api: &Api) -> Result<()> {
    api.register_endpoint(
        Endpoint::new(
            "spn/account/user/profile",
            "SPN User Profile",
            "Get the user profile of the local account.",
        )
        .with_read(PERMIT_USER)
        .with_read_method("GET")
        .with_struct(|_| Ok(serde_json::to_value(local_user_profile())?)),
    )?;

    api.register_endpoint(
        Endpoint::new("account/features", "Get Account Features", "Returns all account features.")
            .with_read(PERMIT_USER)
            .with_read_method("GET")
            .with_struct(|_| Ok(json!({ "Features": serde_json::to_value(features())? }))),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "account/features/{id}/icon",
            "Returns the image of the feature",
            "",
        )
        .with_read(PERMIT_USER)
        .with_mime_type("image/svg+xml")
        .with_data(|request| {
            let id = request.param("id").unwrap_or_default();
            FEATURES
                .iter()
                .find(|feature| feature.id == id)
                .map(|feature| feature.icon.as_bytes().to_vec())
                .ok_or_else(|| pm_api::ApiError::Message("feature id not found".to_string()))
        }),
    )?;

    Ok(())
}

/// Registers the mock user profile in `core:spn/account/user` and a stub
/// `runtime:spn/status` record so UI/tray subscriptions receive a valid
/// "disabled" status instead of failing.
pub fn register_spn_and_user_profile(
    runtime: &pm_runtime::Runtime,
    databases: &std::sync::Arc<pm_database::Databases>,
) {
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0);

    let spn_status_record = pm_record::Record {
        key: pm_record::Key::new("runtime", "spn/status"),
        meta: pm_record::Meta {
            created: now,
            modified: now,
            ..pm_record::Meta::default()
        },
        format: pm_dsd::format::JSON,
        data: serde_json::to_vec(&serde_json::json!({
            "Status": "connected",
        }))
        .unwrap(),
    };

    let spn_status_provider = std::sync::Arc::new(pm_runtime::SimpleValueGetter::new(move |_key| {
        Ok(vec![spn_status_record.clone()])
    }));
    let _ = runtime.registry().register("spn/status", spn_status_provider);

    let user_profile_val = local_user_profile();
    let user_record = pm_record::Record {
        key: pm_record::Key::new("core", "spn/account/user"),
        meta: pm_record::Meta {
            created: now,
            modified: now,
            ..pm_record::Meta::default()
        },
        format: pm_dsd::format::JSON,
        data: serde_json::to_vec(&user_profile_val).unwrap(),
    };
    let _ = databases.internal_interface().put(user_record);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn local_plan_unlocks_history_and_bandwidth() {
        let profile = local_user_profile();
        assert_eq!(profile.state, "approved");
        assert!(profile.current_plan.feature_ids.iter().any(|id| id == "history"));
        assert!(profile.current_plan.feature_ids.iter().any(|id| id == "bw-vis"));
        assert!(!profile.view.show_login_button);
    }

    #[test]
    fn features_match_go_ids() {
        let ids: Vec<&str> = features().iter().map(|feature| feature.id.as_str()).collect();
        assert_eq!(ids, vec!["dns", "filter", "history", "bw-vis", "splittun"]);
        assert!(features()
            .iter()
            .any(|feature| feature.id == "history" && feature.config_key == "history/enable"));
        assert!(features()
            .iter()
            .any(|feature| feature.id == "bw-vis" && feature.beta));
    }

    #[test]
    fn json_field_names_match_go() {
        let value = serde_json::to_value(local_user_profile()).unwrap();
        assert_eq!(value["username"], "local");
        assert_eq!(value["state"], "approved");
        assert_eq!(value["current_plan"]["name"], "Local");
        assert_eq!(value["view"]["ShowLoginButton"], false);
        assert!(value["view"].as_object().unwrap().contains_key("Message"));
    }
}
