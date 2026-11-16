//! Export/import operations, ported from `service/sync`.

use std::path::PathBuf;
use std::sync::Arc;

use base64::engine::general_purpose::STANDARD as BASE64_STANDARD;
use base64::Engine;
use sha1::Digest;
use pm_config::Config;
use pm_database::Interface;
use pm_dsd::gotime::{format_rfc3339, parse_rfc3339};
use pm_profile::{
    derive_profile_id, ensure_profile, make_profile_key, parse_fingerprints, now, Icon,
    ProfileStore, Fingerprint, ICON_SOURCE_IMPORT, ICON_TYPE_API, ICON_TYPE_DATABASE,
    ICON_TYPE_FILE, PROFILES_DB_PATH,
};
use serde_json::{Map, Value};

use crate::{
    err_export_failed, err_import_failed, err_invalid_import_request, err_invalid_profile_data,
    err_invalid_setting_value, err_mismatch, err_not_settable_per_app, err_setting_not_found,
    err_target_not_found, err_unchanged, ImportResult, ProfileExport, ProfileFingerprint,
    ProfileImportRequest, ProfileImportResult, Result, SettingsExport, SettingsImportRequest,
    SingleSettingExport, SingleSettingImportRequest, SyncError, EXPORT_TARGET_GLOBAL,
    TYPE_PROFILE, TYPE_SETTINGS, TYPE_SINGLE_SETTING,
};

/// The sync module. It owns the dependencies used by the export/import code.
pub struct Sync {
    pub config: Arc<Config>,
    pub db: Interface,
    pub profiles: ProfileStore,
    /// Directory holding API profile icons (`data_dir/databases/icons` in Go).
    pub icon_dir: PathBuf,
}

impl Sync {
    pub fn new(config: Arc<Config>, databases: &Arc<pm_database::Databases>, icon_dir: PathBuf) -> Arc<Sync> {
        let options = pm_database::Options {
            local: true,
            internal: true,
            ..pm_database::Options::default()
        };
        Arc::new(Sync {
            config,
            db: databases.interface(options.clone()),
            profiles: ProfileStore::new(databases.interface(options)),
            icon_dir,
        })
    }

    /// Go module `Start()` is a no-op; kept explicit for the core integration.
    pub fn start(&self) {
        tracing::debug!(icon_dir = %self.icon_dir.display(), "sync: module started");
    }
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

fn load_profile(
    sync: &Sync,
    scoped_id: &str,
    not_found: &SyncError,
    load: &SyncError,
) -> Result<pm_profile::Profile> {
    let record = sync
        .db
        .get(&format!("{PROFILES_DB_PATH}{scoped_id}"))
        .map_err(|error| not_found.wrap(&format!("failed to find profile: {error}")))?;
    let profile: pm_profile::Profile = serde_json::from_slice(&record.data)
        .map_err(|error| load.wrap(&format!("failed to load profile: {error}")))?;
    ensure_profile(&profile).map_err(|error| load.wrap(&format!("failed to load profile: {error}")))?;
    Ok(profile)
}

fn settable_per_app(option: &pm_config::OptionState) -> bool {
    option.annotation(pm_config::SETTABLE_PER_APP_ANNOTATION) == Some(Value::Bool(true))
}

fn result_for_option(option: &pm_config::OptionState) -> ImportResult {
    ImportResult {
        restart_required: option.definition().requires_restart,
        replaces_existing: option.is_set_by_user(),
        contains_unknown: false,
    }
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

/// `ExportSettings`: exports changed global settings or a profile's settings.
pub fn export_settings(sync: &Sync, from: &str, keys: &[String]) -> Result<SettingsExport> {
    let mut settings = if from == EXPORT_TARGET_GLOBAL {
        sync.config.active_config_values()
    } else {
        let profile = load_profile(sync, from, &err_target_not_found(), &err_export_failed())?;
        pm_config::flatten(&profile.config)
    };

    if !keys.is_empty() {
        let mut selection = Map::new();
        for key in keys {
            if let Some(value) = settings.get(key) {
                selection.insert(key.clone(), value.clone());
            }
        }
        settings = selection;
    }

    if settings.is_empty() {
        return Err(err_unchanged());
    }

    Ok(SettingsExport {
        type_: TYPE_SETTINGS.to_string(),
        config: pm_config::expand(&settings),
    })
}

/// `ImportSettings`: imports settings into global config or a profile.
pub fn import_settings(sync: &Sync, request: &SettingsImportRequest) -> Result<ImportResult> {
    let export = request
        .export
        .as_ref()
        .ok_or_else(err_invalid_import_request)?;
    if export.type_ != TYPE_SETTINGS {
        return Err(err_mismatch());
    }

    let settings = pm_config::flatten(&export.config);
    let (mut result, global_only) = check_settings(sync, &settings)?;
    if result.contains_unknown && !request.allow_unknown && !request.request.validate_only {
        return Err(err_invalid_import_request().wrap("the export contains unknown settings"));
    }

    if request.request.target == EXPORT_TARGET_GLOBAL {
        if request.request.validate_only {
            return Ok(result);
        }

        let outcome = sync.config.replace_config(&settings);
        if !outcome.validation_errors.is_empty() {
            let messages: Vec<String> = outcome
                .validation_errors
                .iter()
                .map(ToString::to_string)
                .collect();
            return Err(err_import_failed()
                .wrap(&format!("the supplied configuration could not be applied:\n{}", messages.join("\n"))));
        }
        sync.config
            .save_config()
            .map_err(|error| SyncError::new(500, format!("failed to save config: {error}")))?;
        result.restart_required = outcome.requires_restart;
        return Ok(result);
    }

    if global_only {
        return Err(err_not_settable_per_app().wrap("export contains settings that cannot be set per app"));
    }

    let mut profile = load_profile(
        sync,
        &request.request.target,
        &err_target_not_found(),
        &err_import_failed(),
    )?;

    if request.request.validate_only {
        return Ok(result);
    }

    if request.reset {
        profile.config = pm_config::expand(&settings);
    } else {
        for (key, value) in &settings {
            pm_config::put_value_into_hierarchical_config(&mut profile.config, key, value.clone());
        }
    }
    profile.last_edited = now();
    sync.profiles
        .save(&profile)
        .map_err(|error| err_import_failed().wrap(&format!("failed to save profile: {error}")))?;

    Ok(result)
}

/// `checkSettings`: validates the flattened settings and gathers metadata.
fn check_settings(sync: &Sync, settings: &Map<String, Value>) -> Result<(ImportResult, bool)> {
    let mut result = ImportResult::default();

    // `validate_config` mirrors Go's ForEachOption + ValidateValue loop.
    let outcome = sync.config.validate_config(settings);
    if let Some(error) = outcome.validation_errors.first() {
        return Err(err_invalid_setting_value().wrap(&format!(
            "configuration value for {} is invalid: {error}",
            error.key
        )));
    }
    result.contains_unknown = outcome.contains_unknown;
    result.restart_required = outcome.requires_restart;

    // Annotations are available on the exported definitions.
    let mut global_only = false;
    for option in sync.config.export_options() {
        if !settings.contains_key(&option.key) {
            continue;
        }
        let settable = option
            .annotations
            .as_ref()
            .and_then(|annotations| annotations.get(pm_config::SETTABLE_PER_APP_ANNOTATION))
            == Some(&Value::Bool(true));
        if !settable {
            global_only = true;
        }
    }
    Ok((result, global_only))
}

// ---------------------------------------------------------------------------
// Single setting
// ---------------------------------------------------------------------------

/// `ExportSingleSetting`: exports one setting value.
pub fn export_single_setting(
    sync: &Sync,
    key: &str,
    from: &str,
) -> Result<SingleSettingExport> {
    let option = sync
        .config
        .option(key)
        .map_err(|error| err_setting_not_found().wrap(&format!("configuration {error}")))?;

    let value = if from == EXPORT_TARGET_GLOBAL {
        match option.user_value() {
            Some(value) => value.to_json(),
            None => return Err(err_unchanged()),
        }
    } else {
        if !settable_per_app(&option) {
            return Err(err_not_settable_per_app());
        }
        let profile = load_profile(sync, from, &err_target_not_found(), &err_export_failed())?;
        let flattened = pm_config::flatten(&profile.config);
        match flattened.get(key).filter(|value| !value.is_null()) {
            Some(value) => value.clone(),
            None => return Err(err_unchanged()),
        }
    };

    Ok(SingleSettingExport {
        type_: TYPE_SINGLE_SETTING.to_string(),
        id: key.to_string(),
        value,
    })
}

/// `ImportSingeSetting`: imports one setting value (Go spelling preserved).
pub fn import_single_setting(
    sync: &Sync,
    request: &SingleSettingImportRequest,
) -> Result<ImportResult> {
    let export = request
        .export
        .as_ref()
        .ok_or_else(err_invalid_import_request)?;
    if export.type_ != TYPE_SINGLE_SETTING {
        return Err(err_mismatch());
    }

    let option = sync
        .config
        .option(&export.id)
        .map_err(|error| err_setting_not_found().wrap(&format!("configuration {error}")))?;
    option
        .validate(&export.value)
        .map_err(|error| err_invalid_setting_value().wrap(&error.to_string()))?;

    if request.request.target == EXPORT_TARGET_GLOBAL {
        if request.request.validate_only {
            return Ok(result_for_option(&option));
        }
        sync.config
            .set_config_option(&export.id, export.value.clone())
            .map_err(|error| err_invalid_setting_value().wrap(&error.to_string()))?;
    } else {
        if !settable_per_app(&option) {
            return Err(err_not_settable_per_app());
        }
        let mut profile = load_profile(
            sync,
            &request.request.target,
            &err_target_not_found(),
            &err_import_failed(),
        )?;
        if request.request.validate_only {
            return Ok(result_for_option(&option));
        }
        pm_config::put_value_into_hierarchical_config(
            &mut profile.config,
            &export.id,
            export.value.clone(),
        );
        profile.last_edited = now();
        sync.profiles
            .save(&profile)
            .map_err(|error| err_import_failed().wrap(&format!("failed to save profile: {error}")))?;
    }

    Ok(result_for_option(&option))
}

// ---------------------------------------------------------------------------
// Profile
// ---------------------------------------------------------------------------

/// `ExportProfile`: exports fingerprints, settings and metadata.
pub fn export_profile(sync: &Sync, scoped_id: &str) -> Result<ProfileExport> {
    let profile = load_profile(sync, scoped_id, &err_target_not_found(), &err_export_failed())?;

    let fingerprints: Vec<ProfileFingerprint> = profile
        .fingerprints
        .iter()
        .map(|fp| ProfileFingerprint {
            type_: fp.type_.clone(),
            key: fp.key.clone(),
            operation: fp.operation.clone(),
            value: fp.value.clone(),
            merged_from: fp.merged_from.clone(),
        })
        .collect();

    let mut export = ProfileExport {
        type_: TYPE_PROFILE.to_string(),
        id: profile.id.clone(),
        source: profile.source.clone(),
        name: profile.name.clone(),
        description: profile.description.clone(),
        homepage: profile.homepage.clone(),
        presentation_path: profile.presentation_path.clone(),
        use_presentation_path: profile.use_presentation_path,
        fingerprints,
        config: profile.config.clone(),
        last_edited: (profile.last_edited > 0).then(|| format_rfc3339(profile.last_edited)),
        created: (profile.created > 0).then(|| format_rfc3339(profile.created)),
        internal: profile.internal,
        ..ProfileExport::default()
    };

    // Derive the ID from a copy: Go mutates the profile's fingerprints but the
    // export already copied them in their original order.
    let mut derivation_fingerprints = profile.fingerprints.clone();
    export.id = derive_profile_id(&mut derivation_fingerprints);

    // Add the first exportable icon.
    if !profile.icons.is_empty() {
        let mut last_error = None;
        for icon in &profile.icons {
            match icon_data_url(sync, icon) {
                Ok(data) => {
                    export.icon_data = data;
                    last_error = None;
                    break;
                }
                Err(error) => last_error = Some(error),
            }
        }
        if let Some(error) = last_error {
            return Err(err_export_failed().wrap(&format!("failed to export icon: {error}")));
        }
    }

    // NOTE: Go modifies `p` (not `export`) in the presentation-path cleanup
    // below, so the export keeps its original values. This quirk is preserved
    // for byte compatibility.
    // let mut p = profile; if export.name != "" && export.icon_data != "" { ... }

    Ok(export)
}

/// `ImportProfile`: imports a full application profile.
pub fn import_profile(
    sync: &Sync,
    request: &mut ProfileImportRequest,
    required_source: &str,
) -> Result<ProfileImportResult> {
    let export = request
        .export
        .as_ref()
        .ok_or_else(err_invalid_import_request)?
        .clone();

    if export.type_ != TYPE_PROFILE {
        return Err(err_mismatch());
    }
    if !export.source.is_empty() && export.source != required_source {
        return Err(err_mismatch());
    }

    let mut fingerprints: Vec<Fingerprint> = export
        .fingerprints
        .iter()
        .map(|fp| Fingerprint {
            type_: fp.type_.clone(),
            key: fp.key.clone(),
            operation: fp.operation.clone(),
            value: fp.value.clone(),
            merged_from: fp.merged_from.clone(),
        })
        .collect();
    if fingerprints.is_empty() {
        return Err(err_invalid_profile_data().wrap("the export contains no fingerprints"));
    }

    // DeriveProfileID sorts and compacts the slice in place; Go then uses that
    // same (mutated) slice for the saved profile.
    let profile_id = derive_profile_id(&mut fingerprints);
    if !export.id.is_empty() && export.id != profile_id {
        return Err(err_invalid_profile_data()
            .wrap("the export profile ID does not match the fingerprints, remove to ignore"));
    }

    let (_, parse_error) = parse_fingerprints(&fingerprints, "");
    if let Some(error) = parse_error {
        return Err(
            err_invalid_profile_data().wrap(&format!("the export contains invalid fingerprints: {error}")),
        );
    }

    let settings = pm_config::flatten(&export.config);
    let (result, global_only) = check_settings(sync, &settings)?;
    if result.contains_unknown && !request.allow_unknown && !request.request.validate_only {
        return Err(err_invalid_import_request().wrap("the export contains unknown settings"));
    }
    if global_only {
        return Err(err_not_settable_per_app().wrap("export contains settings that cannot be set per app"));
    }

    let mut import_result = ProfileImportResult {
        result,
        replaces_profiles: Vec::with_capacity(export.fingerprints.len()),
    };

    let key = make_profile_key(&export.source, &profile_id);
    let exists = sync
        .db
        .exists(&key)
        .map_err(|error| SyncError::new(500, format!("internal import error: {error}")))?;
    if exists {
        import_result.result.replaces_existing = true;
    }

    let required_source_prefix = format!("{}/", export.source);
    for fp in &export.fingerprints {
        if fp.merged_from.is_empty() {
            continue;
        }
        if !fp.merged_from.starts_with(&required_source_prefix) {
            return Err(err_invalid_import_request()
                .wrap("exported profile was merged from different profile source"));
        }
        let exists = sync
            .db
            .exists(&format!("{PROFILES_DB_PATH}{}", fp.merged_from))
            .map_err(|error| SyncError::new(500, format!("internal import error: {error}")))?;
        if exists {
            import_result.replaces_profiles.push(fp.merged_from.clone());
        }
    }

    if request.request.validate_only {
        return Ok(import_result);
    }
    if import_result.result.replaces_existing && !request.allow_replace {
        return Err(err_import_failed().wrap("import would replace existing profile"));
    }

    let mut profile = pm_profile::Profile {
        id: profile_id,
        source: required_source.to_string(),
        name: export.name.clone(),
        description: export.description.clone(),
        homepage: export.homepage.clone(),
        presentation_path: export.presentation_path.clone(),
        use_presentation_path: export.use_presentation_path,
        fingerprints,
        config: if export.config.is_empty() {
            Map::new()
        } else {
            export.config.clone()
        },
        last_edited: export
            .last_edited
            .as_deref()
            .and_then(parse_rfc3339)
            .unwrap_or(0),
        created: export
            .created
            .as_deref()
            .and_then(parse_rfc3339)
            .unwrap_or(0),
        internal: export.internal,
        ..pm_profile::Profile::default()
    };
    if profile.created == 0 {
        profile.created = now();
    }

    if !export.icon_data.is_empty() {
        let (data, subtype) = decode_data_url(&export.icon_data)?;
        let filename = update_profile_icon(&sync.icon_dir, &data, &subtype)
            .map_err(|error| err_import_failed().wrap(&format!("icon is invalid: {error}")))?;
        profile.icons = vec![Icon::new(ICON_TYPE_API, &filename, ICON_SOURCE_IMPORT)];
    }

    sync.profiles
        .save(&profile)
        .map_err(|error| err_import_failed().wrap(&format!("failed to save profile: {error}")))?;

    for profile_id in &import_result.replaces_profiles {
        if let Err(error) = sync.db.delete(&format!("{PROFILES_DB_PATH}{profile_id}")) {
            tracing::error!(%error, profile_id, "sync: failed to delete merged profile on import");
        }
    }

    Ok(import_result)
}

// ---------------------------------------------------------------------------
// Icon handling (Go binmeta, modelled as file + database access)
// ---------------------------------------------------------------------------

/// Go `binmeta.Icon.GetIconAsDataURL`, limited to the icon kinds the core can
/// resolve from its database and icon storage directory.
fn icon_data_url(sync: &Sync, icon: &Icon) -> Result<String> {
    match icon.type_.as_str() {
        ICON_TYPE_FILE => Err(SyncError::new(
            500,
            "getting icon from file is not supported".to_string(),
        )),
        ICON_TYPE_DATABASE => {
            if !icon.value.starts_with("cache:icons/") {
                return Err(SyncError::new(500, "invalid icon db key".to_string()));
            }
            let record = sync
                .db
                .get(&icon.value)
                .map_err(|error| SyncError::new(500, error.to_string()))?;
            let stored: Map<String, Value> = serde_json::from_slice(&record.data)
                .map_err(|error| SyncError::new(500, error.to_string()))?;
            match stored.get("iconData").and_then(Value::as_str) {
                Some(data) => Ok(data.to_string()),
                None => Err(SyncError::new(500, "invalid icon db key".to_string())),
            }
        }
        ICON_TYPE_API => {
            if ignored_icon(&icon.value) {
                return Err(SyncError::new(500, "icon is ignored".to_string()));
            }
            let path = sync.icon_dir.join(&icon.value);
            let data = std::fs::read(&path)
                .map_err(|error| SyncError::new(500, error.to_string()))?;
            Ok(encode_data_url(&data))
        }
        _ => Err(SyncError::new(500, "unknown icon type".to_string())),
    }
}

const IGNORED_ICONS: &[&str] = &[
    "a27898ddfa4e0481b62c69faa196919a738fcade",
    "5a3eea8bcd08b9336ce9c5083f26185164268ee9",
    "573393d6ad238d255b20dc1c1b303c95debe6965",
    "d459b2cb23c27cc31ccab5025533048d5d8301bf",
    "d35a0d91ebfda81df5286f68ec5ddb1d6ad6b850",
    "cc33187385498384f1b648e23be5ef1a2e9f5f71",
];

fn ignored_icon(name: &str) -> bool {
    let name = name.split('.').next().unwrap_or(name).to_ascii_lowercase();
    IGNORED_ICONS.contains(&name.as_str())
}

/// Go `http.DetectContentType` for the image types icons use, plus its
/// text/plain fallback. `dataurl.EncodeBytes` removes "; " from parameters.
fn detect_content_type(data: &[u8]) -> &'static str {
    if data.starts_with(b"\x89PNG\r\n\x1a\n") {
        "image/png"
    } else if data.starts_with(b"\xff\xd8\xff") {
        "image/jpeg"
    } else if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
        "image/gif"
    } else if data.starts_with(b"II*\x00") || data.starts_with(b"MM\x00*") {
        "image/tiff"
    } else if data.len() >= 12 && data.starts_with(b"RIFF") && &data[8..12] == b"WEBP" {
        "image/webp"
    } else if data.starts_with(b"<?xml") {
        "text/xml;charset=utf-8"
    } else if data.iter().any(|byte| *byte < 0x20 && *byte != b'\n' && *byte != b'\r' && *byte != b'\t') {
        "application/octet-stream"
    } else {
        "text/plain;charset=utf-8"
    }
}

fn encode_data_url(data: &[u8]) -> String {
    format!(
        "data:{};base64,{}",
        detect_content_type(data),
        BASE64_STANDARD.encode(data)
    )
}

/// Go `dataurl.DecodeString`: returns the bytes and the media type subtype.
fn decode_data_url(data_url: &str) -> Result<(Vec<u8>, String)> {
    let rest = data_url
        .strip_prefix("data:")
        .ok_or_else(|| SyncError::new(500, "invalid data URL".to_string()))?;
    let (metadata, encoded) = rest
        .split_once(',')
        .ok_or_else(|| SyncError::new(500, "invalid data URL".to_string()))?;
    let mut parts = metadata.split(';');
    let media_type = parts
        .next()
        .ok_or_else(|| SyncError::new(500, "invalid data URL".to_string()))?;
    let subtype = media_type
        .split_once('/')
        .map(|(_, subtype)| subtype.to_string())
        .unwrap_or_default();
    let data = if parts.any(|part| part.eq_ignore_ascii_case("base64")) {
        BASE64_STANDARD
            .decode(encoded.as_bytes())
            .map_err(|error| SyncError::new(500, format!("invalid base64 data: {error}")))?
    } else {
        percent_decode(encoded).into_bytes()
    };
    Ok((data, subtype))
}

fn percent_decode(text: &str) -> String {
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' && index + 2 < bytes.len() {
            if let Ok(byte) = u8::from_str_radix(&text[index + 1..index + 3], 16) {
                out.push(byte);
                index += 3;
                continue;
            }
        }
        out.push(bytes[index]);
        index += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}

/// Go `binmeta.UpdateProfileIcon`, reduced to validation and file storage.
fn update_profile_icon(icon_dir: &std::path::Path, data: &[u8], ext: &str) -> Result<String> {
    if data.len() > 1_000_000 {
        return Err(SyncError::new(500, "icon too big".to_string()));
    }
    let sum = hex::encode(sha1::Sha1::digest(data));
    if ignored_icon(&sum) {
        return Err(SyncError::new(500, "icon is ignored".to_string()));
    }
    let ext = match ext.to_ascii_lowercase().as_str() {
        "gif" => "gif",
        "jpeg" | "jpg" => "jpg",
        "png" => "png",
        "svg" => "svg",
        "tiff" => "tiff",
        "webp" => "webp",
        _ => return Err(SyncError::new(500, "unsupported icon format".to_string())),
    };
    let filename = format!("{sum}.{ext}");
    std::fs::create_dir_all(icon_dir).map_err(|error| SyncError::new(500, error.to_string()))?;
    let path = icon_dir.join(&filename);
    std::fs::write(&path, data).map_err(|error| SyncError::new(500, error.to_string()))?;
    Ok(filename)
}
