//! API registration and request handlers, mirroring the six Go endpoints.

use std::sync::Arc;

use http::header::CONTENT_TYPE;
use http::HeaderValue;
use pm_api::{Api, Endpoint, Parameter, Request, PERMIT_ADMIN};
use serde::de::DeserializeOwned;

use crate::format;
use crate::sync::{
    export_profile, export_settings, export_single_setting, import_profile, import_settings,
    import_single_setting,
};
use crate::{
    err_export_failed, err_invalid_import_request, err_mismatch, Sync,
    SyncError, ExportRequest, ImportRequest, ProfileExportRequest, ProfileImportRequest,
    SettingsImportRequest, SingleSettingImportRequest, SOURCE_LOCAL,
};

impl Sync {
    /// Registers all sync endpoints on the given API.
    pub fn register_api(self: &Arc<Self>, api: &Api) -> pm_api::Result<()> {
        register_api(self, api)
    }
}

fn param(method: &str, field: &str, description: &str) -> Parameter {
    Parameter {
        method: method.to_string(),
        field: field.to_string(),
        value: String::new(),
        description: description.to_string(),
    }
}

/// Registers the same endpoints, paths and permissions as Go `prep()`.
pub fn register_api(sync: &Arc<Sync>, api: &Api) -> pm_api::Result<()> {
    api.register_endpoint(
        Endpoint::new(
            "sync/settings/export",
            "Export Settings",
            "Exports settings in a share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param("GET", "from", "Specify where to export from."))
        .with_parameter(param(
            "GET",
            "key",
            "Optionally select a single setting to export. Repeat to export selection.",
        ))
        .with_data({
            let sync = Arc::clone(sync);
            move |request| handle_export_settings(&sync, request)
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "sync/settings/import",
            "Import Settings",
            "Imports settings from the share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param("POST", "to", "Specify where to import to."))
        .with_parameter(param("POST", "validate", "Validate only."))
        .with_parameter(param("POST", "reset", "Replace all existing settings."))
        .with_parameter(param(
            "POST",
            "allowUnknown",
            "Allow importing of unknown values.",
        ))
        .with_struct({
            let sync = Arc::clone(sync);
            move |request| handle_import_settings(&sync, request)
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "sync/single-setting/export",
            "Export Single Setting",
            "Exports a single setting in a share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param("GET", "from", "Specify where to export from."))
        .with_parameter(param(
            "GET",
            "key",
            "Specify which settings key to export.",
        ))
        .with_data({
            let sync = Arc::clone(sync);
            move |request| handle_export_single_setting(&sync, request)
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "sync/single-setting/import",
            "Import Single Setting",
            "Imports a single setting from the share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param("POST", "to", "Specify where to import to."))
        .with_parameter(param(
            "POST",
            "key",
            "Specify which setting key to import.",
        ))
        .with_parameter(param("POST", "validate", "Validate only."))
        .with_struct({
            let sync = Arc::clone(sync);
            move |request| handle_import_single_setting(&sync, request)
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "sync/profile/export",
            "Export App Profile",
            "Exports app fingerprints, settings and metadata in a share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param(
            "GET",
            "id",
            "Specify scoped profile ID to export.",
        ))
        .with_data({
            let sync = Arc::clone(sync);
            move |request| handle_export_profile(&sync, request)
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "sync/profile/import",
            "Import App Profile",
            "Imports full app profiles, including fingerprints, setting and metadata from the share-able format.",
        )
        .with_read(PERMIT_ADMIN)
        .with_write(PERMIT_ADMIN)
        .with_parameter(param(
            "POST",
            "allowReplace",
            "Allow replacing existing profiles.",
        ))
        .with_parameter(param("POST", "validate", "Validate only."))
        .with_parameter(param("POST", "reset", "Replace all existing settings."))
        .with_parameter(param(
            "POST",
            "allowUnknown",
            "Allow importing of unknown values.",
        ))
        .with_struct({
            let sync = Arc::clone(sync);
            move |request| handle_import_profile(&sync, request)
        }),
    )?;

    Ok(())
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

fn accept(request: &Request) -> &str {
    request.header("accept").unwrap_or("")
}

fn content_type(request: &Request) -> String {
    request.header("content-type").unwrap_or("").to_string()
}

fn has_query(request: &Request) -> bool {
    !request.query.is_empty()
}

fn has_param(request: &Request, name: &str) -> bool {
    request.query.contains_key(name)
}

/// Like Go's `q["key"]`: returns every value of a repeated query parameter.
fn query_all(request: &Request, name: &str) -> Vec<String> {
    let mut values = Vec::new();
    for pair in request.query_string.split('&') {
        if pair.is_empty() {
            continue;
        }
        let (key, value) = pair.split_once('=').unwrap_or((pair, ""));
        if percent_decode(key) == name {
            values.push(percent_decode(value));
        }
    }
    values
}

fn percent_decode(text: &str) -> String {
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        match bytes[index] {
            b'+' => {
                out.push(b' ');
                index += 1;
            }
            b'%' if index + 2 < bytes.len() => {
                if let Ok(byte) = u8::from_str_radix(&text[index + 1..index + 3], 16) {
                    out.push(byte);
                    index += 3;
                    continue;
                }
                out.push(b'%');
                index += 1;
            }
            byte => {
                out.push(byte);
                index += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

fn set_content_type(request: &mut Request, mime: &str) {
    if let Ok(value) = HeaderValue::from_str(mime) {
        request.response_header.insert(CONTENT_TYPE, value);
    }
}

/// Parses a body as JSON (or the format requested via Accept) for the Go
/// `dsd.MimeLoad` call in the single-setting import handler.
fn mime_load<T: DeserializeOwned>(body: &[u8], accept: &str) -> Result<T, SyncError> {
    format::mime_load(body, accept)
        .map_err(|error| err_invalid_import_request().wrap(&format!("failed to parse import request: {error}")))
}

/// Handles the Go `parseExport` switch shared by the import endpoints.
fn prepare_export<E: DeserializeOwned>(
    request: &ImportRequest,
    export: &mut Option<E>,
) -> Result<(), SyncError> {
    match (export.is_some(), &request.raw_export) {
        (true, raw) if !raw.is_empty() => Err(err_invalid_import_request()
            .wrap("both Export and RawExport are defined")),
        (false, raw) if !raw.is_empty() => {
            let (parsed, _) = format::parse_export::<E>(raw.as_bytes(), &request.raw_mime)?;
            *export = Some(parsed);
            Ok(())
        }
        (false, _) => Err(err_invalid_import_request()),
        (true, _) => Ok(()),
    }
}

// ---------------------------------------------------------------------------
// Export handlers
// ---------------------------------------------------------------------------

fn handle_export_settings(sync: &Sync, request: &mut Request) -> pm_api::Result<Vec<u8>> {
    let parsed = if has_query(request) {
        ExportRequest {
            from: request.query.get("from").cloned().unwrap_or_default(),
            keys: query_all(request, "key"),
        }
    } else {
        serde_json::from_slice(&request.body).map_err(|error| {
            err_export_failed()
                .wrap(&format!("failed to parse export request: {error}"))
                .api()
        })?
    };
    if parsed.from.is_empty() {
        return Err(pm_api::ApiError::Message("missing parameters".to_string()));
    }
    let export = export_settings(sync, &parsed.from, &parsed.keys).map_err(SyncError::api)?;
    let (data, mime) = format::serialize_export(&export, accept(request)).map_err(SyncError::api)?;
    set_content_type(request, mime);
    Ok(data)
}

fn handle_export_single_setting(sync: &Sync, request: &mut Request) -> pm_api::Result<Vec<u8>> {
    let parsed = if has_query(request) {
        ExportRequest {
            from: request.query.get("from").cloned().unwrap_or_default(),
            keys: query_all(request, "key"),
        }
    } else {
        serde_json::from_slice(&request.body).map_err(|error| {
            err_export_failed()
                .wrap(&format!("failed to parse export request: {error}"))
                .api()
        })?
    };
    if parsed.from.is_empty() || parsed.keys.len() != 1 {
        return Err(pm_api::ApiError::Message(
            "missing or malformed parameters".to_string(),
        ));
    }
    let export = export_single_setting(sync, &parsed.keys[0], &parsed.from).map_err(SyncError::api)?;
    let (data, mime) = format::serialize_export(&export, accept(request)).map_err(SyncError::api)?;
    set_content_type(request, mime);
    Ok(data)
}

fn handle_export_profile(sync: &Sync, request: &mut Request) -> pm_api::Result<Vec<u8>> {
    let id = if has_query(request) {
        request.query.get("id").cloned().unwrap_or_default()
    } else {
        let parsed: ProfileExportRequest = serde_json::from_slice(&request.body).map_err(|error| {
            err_export_failed()
                .wrap(&format!("failed to parse export request: {error}"))
                .api()
        })?;
        parsed.id
    };
    if id.is_empty() {
        return Err(pm_api::ApiError::Message("missing parameters".to_string()));
    }
    let export = export_profile(sync, &id).map_err(SyncError::api)?;
    let (data, mime) =
        format::serialize_profile_export(&export, accept(request)).map_err(SyncError::api)?;
    set_content_type(request, mime);
    Ok(data)
}

// ---------------------------------------------------------------------------
// Import handlers
// ---------------------------------------------------------------------------

fn import_request(request: &Request) -> ImportRequest {
    ImportRequest {
        target: request.query.get("to").cloned().unwrap_or_default(),
        validate_only: has_param(request, "validate"),
        raw_export: String::from_utf8_lossy(&request.body).into_owned(),
        raw_mime: content_type(request),
    }
}

fn handle_import_settings(sync: &Sync, request: &Request) -> pm_api::Result<serde_json::Value> {
    let mut parsed: SettingsImportRequest = if has_query(request) {
        SettingsImportRequest {
            request: import_request(request),
            reset: has_param(request, "reset"),
            allow_unknown: has_param(request, "allowUnknown"),
            export: None,
        }
    } else {
        serde_json::from_slice(&request.body).map_err(|error| {
            err_invalid_import_request()
                .wrap(&format!("failed to parse import request: {error}"))
                .api()
        })?
    };
    prepare_export(&parsed.request, &mut parsed.export).map_err(SyncError::api)?;
    let result = import_settings(sync, &parsed).map_err(SyncError::api)?;
    serde_json::to_value(result).map_err(pm_api::ApiError::Json)
}

fn handle_import_single_setting(
    sync: &Sync,
    request: &Request,
) -> pm_api::Result<serde_json::Value> {
    let mut parsed: SingleSettingImportRequest = if has_query(request) {
        SingleSettingImportRequest {
            request: import_request(request),
            export: None,
        }
    } else {
        mime_load(&request.body, accept(request)).map_err(SyncError::api)?
    };
    prepare_export(&parsed.request, &mut parsed.export).map_err(SyncError::api)?;

    // Optional key check from the query parameters.
    if has_param(request, "key") {
        let expected = request.query.get("key").map(String::as_str).unwrap_or("");
        let found = parsed
            .export
            .as_ref()
            .map(|export| export.id.as_str())
            .unwrap_or("");
        if expected != found {
            return Err(err_mismatch().api());
        }
    }

    let result = import_single_setting(sync, &parsed).map_err(SyncError::api)?;
    serde_json::to_value(result).map_err(pm_api::ApiError::Json)
}

fn handle_import_profile(sync: &Sync, request: &Request) -> pm_api::Result<serde_json::Value> {
    let mut parsed: ProfileImportRequest = if has_query(request) {
        ProfileImportRequest {
            request: ImportRequest {
                target: String::new(),
                validate_only: has_param(request, "validate"),
                raw_export: String::from_utf8_lossy(&request.body).into_owned(),
                raw_mime: content_type(request),
            },
            allow_unknown: has_param(request, "allowUnknown"),
            allow_replace: has_param(request, "allowReplace"),
            export: None,
        }
    } else {
        serde_json::from_slice(&request.body).map_err(|error| {
            err_invalid_import_request()
                .wrap(&format!("failed to parse import request: {error}"))
                .api()
        })?
    };
    prepare_export(&parsed.request, &mut parsed.export).map_err(SyncError::api)?;
    let result = import_profile(sync, &mut parsed, SOURCE_LOCAL).map_err(SyncError::api)?;
    serde_json::to_value(result).map_err(pm_api::ApiError::Json)
}
