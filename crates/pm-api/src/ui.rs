//! Serving the user interface from zip archives, like `service/ui`.
//!
//! Archives are read from `<bin-dir>/<module>.zip` on first use and kept in
//! memory. Routes:
//!
//! * `/assets/<path>` serves from `assets.zip` (missing paths fall back to
//!   `index.html`).
//! * `/ui/modules/<module>` redirects to the module root with a trailing slash.
//! * `/ui/modules/<module>/` serves `index.html`.
//! * `/ui/modules/<module>/<path>` serves a file, falling back to `index.html`
//!   for unknown paths (the UI uses client-side routing).
//! * `/` redirects to `/ui/modules/portmaster/`.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, RwLock};

use axum::body::Body;
use axum::extract::{Path as UrlPath, State};
use axum::response::{IntoResponse, Response as AxumResponse};
use http::{header, HeaderValue, StatusCode};

use crate::Api;

/// Default module served at `/`.
pub const DEFAULT_MODULE: &str = "portmaster";
/// Module name of the shared assets archive.
pub const ASSETS_MODULE: &str = "assets";

/// Zip archives of the UI, served from the binary directory.
pub struct UiArchives {
    bin_dir: PathBuf,
    archives: RwLock<HashMap<String, Arc<Archive>>>,
    upgrade_lock: AtomicBool,
}

struct Archive {
    files: HashMap<String, Vec<u8>>,
}

impl UiArchives {
    pub fn new(bin_dir: impl Into<PathBuf>) -> UiArchives {
        UiArchives {
            bin_dir: bin_dir.into(),
            archives: RwLock::new(HashMap::new()),
            upgrade_lock: AtomicBool::new(false),
        }
    }

    /// Enables the upgrade lock and closes all open archives, like Go
    /// `UI.EnableUpgradeLock`.
    pub fn enable_upgrade_lock(&self) {
        self.upgrade_lock.store(true, Ordering::SeqCst);
        self.close_archives();
    }

    /// Disables the upgrade lock, like Go `UI.DisableUpgradeLock`.
    pub fn disable_upgrade_lock(&self) {
        self.upgrade_lock.store(false, Ordering::SeqCst);
    }

    /// Returns whether resources are locked for an upgrade.
    pub fn is_upgrade_locked(&self) -> bool {
        self.upgrade_lock.load(Ordering::SeqCst)
    }

    /// Drops all cached archives, like Go `UI.CloseArchives`.
    pub fn close_archives(&self) {
        self.archives
            .write()
            .expect("lock poisoned")
            .clear();
    }

    /// Returns the archive of a module, loading it on first use.
    fn archive(&self, module: &str) -> Result<Arc<Archive>, String> {
        if let Some(archive) = self
            .archives
            .read()
            .expect("lock poisoned")
            .get(module)
            .cloned()
        {
            return Ok(archive);
        }

        // Axum percent-decodes path captures, so reject anything that is not
        // a plain module name before joining it to bin_dir.
        if module.is_empty()
            || !module
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
        {
            return Err("invalid module".to_string());
        }
        let path = self.bin_dir.join(format!("{module}.zip"));
        let archive = Arc::new(Archive::load(&path, module)?);
        self.archives
            .write()
            .expect("lock poisoned")
            .insert(module.to_string(), Arc::clone(&archive));
        Ok(archive)
    }
}

impl Archive {
    fn load(path: &Path, module: &str) -> Result<Archive, String> {
        let file = std::fs::File::open(path).map_err(|error| {
            format!(
                "failed to load module {module}: {}: {error}",
                path.display()
            )
        })?;
        let mut zip = zip::ZipArchive::new(file)
            .map_err(|error| format!("failed to read module {module}: {error}"))?;

        let mut files = HashMap::new();
        for index in 0..zip.len() {
            let mut entry = zip
                .by_index(index)
                .map_err(|error| format!("failed to read module {module}: {error}"))?;
            if entry.is_dir() {
                continue;
            }
            let Some(name) = entry.enclosed_name() else {
                continue;
            };
            let name = name.to_string_lossy().trim_start_matches('/').to_string();
            let mut data = Vec::with_capacity(entry.size() as usize);
            std::io::Read::read_to_end(&mut entry, &mut data)
                .map_err(|error| format!("failed to read module {module}: {error}"))?;
            files.insert(name, data);
        }

        Ok(Archive { files })
    }

    fn get(&self, path: &str) -> Option<&Vec<u8>> {
        self.files.get(path)
    }
}

/// Normalizes a requested resource path and rejects traversal.
fn clean_resource_path(path: &str) -> Option<String> {
    let mut segments = Vec::new();
    for segment in path.split('/') {
        match segment {
            "" | "." => {}
            ".." => return None,
            other => segments.push(other),
        }
    }
    if segments.is_empty() {
        return Some("index.html".to_string());
    }
    Some(segments.join("/"))
}

/// Returns the content type for a file name.
pub fn content_type(path: &str) -> &'static str {
    let extension = path
        .rsplit_once('.')
        .map(|(_, extension)| extension.to_ascii_lowercase())
        .unwrap_or_default();
    match extension.as_str() {
        "html" | "htm" => "text/html; charset=utf-8",
        "js" | "mjs" => "application/javascript; charset=utf-8",
        "css" => "text/css; charset=utf-8",
        "json" | "map" => "application/json; charset=utf-8",
        "txt" => "text/plain; charset=utf-8",
        "svg" => "image/svg+xml",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "webp" => "image/webp",
        "ico" => "image/x-icon",
        "woff" => "font/woff",
        "woff2" => "font/woff2",
        "ttf" => "font/ttf",
        "wasm" => "application/wasm",
        _ => "application/octet-stream",
    }
}

fn serve(module: &str, requested_path: &str, archives: &UiArchives) -> AxumResponse {
    if archives.is_upgrade_locked() {
        return (
            StatusCode::LOCKED,
            "Resources locked, upgrade in progress.\n",
        )
            .into_response();
    }

    let Some(path) = clean_resource_path(requested_path) else {
        return (StatusCode::BAD_REQUEST, "invalid path.\n").into_response();
    };

    let archive = match archives.archive(module) {
        Ok(archive) => archive,
        Err(error) => {
            tracing::debug!(%error, "ui: failed to serve resource");
            return (StatusCode::INTERNAL_SERVER_ERROR, format!("{error}\n")).into_response();
        }
    };

    // Unknown paths fall back to the single page application index.
    let (path, data) = match archive.get(&path) {
        Some(data) => (path, data),
        None => match archive.get("index.html") {
            Some(data) => ("index.html".to_string(), data),
            None => {
                return (
                    StatusCode::NOT_FOUND,
                    format!("resource \"{requested_path}\" not found\n"),
                )
                    .into_response()
            }
        },
    };

    let mut response = AxumResponse::new(Body::from(data.clone()));
    *response.status_mut() = StatusCode::OK;
    if let Ok(value) = HeaderValue::from_str(content_type(&path)) {
        response.headers_mut().insert(header::CONTENT_TYPE, value);
    }
    response
}

fn api_archives(api: &Api) -> Option<Arc<UiArchives>> {
    api.ui_archives()
}

/// `/assets/<path>`
pub async fn serve_assets(
    State(api): State<Arc<Api>>,
    UrlPath(path): UrlPath<String>,
) -> AxumResponse {
    let mut response = match api_archives(&api) {
        Some(archives) => serve(ASSETS_MODULE, &path, &archives),
        None => (StatusCode::NOT_FOUND, "ui is not configured\n").into_response(),
    };
    api.apply_security_headers(response.headers_mut());
    response
}

/// `/ui/modules/<module>`
pub async fn redirect_module(UrlPath(module): UrlPath<String>) -> AxumResponse {
    let location = format!("/ui/modules/{module}/");
    let mut response = StatusCode::PERMANENT_REDIRECT.into_response();
    if let Ok(value) = HeaderValue::from_str(&location) {
        response.headers_mut().insert(header::LOCATION, value);
    }
    response
}

/// `/ui/modules/<module>/`
pub async fn serve_module_index(
    State(api): State<Arc<Api>>,
    UrlPath(module): UrlPath<String>,
) -> AxumResponse {
    let mut response = match api_archives(&api) {
        Some(archives) => serve(&module, "index.html", &archives),
        None => (StatusCode::NOT_FOUND, "ui is not configured\n").into_response(),
    };
    api.apply_security_headers(response.headers_mut());
    response
}

/// `/ui/modules/<module>/<path>`
pub async fn serve_module_file(
    State(api): State<Arc<Api>>,
    UrlPath((module, path)): UrlPath<(String, String)>,
) -> AxumResponse {
    let mut response = match api_archives(&api) {
        Some(archives) => serve(&module, &path, &archives),
        None => (StatusCode::NOT_FOUND, "ui is not configured\n").into_response(),
    };
    api.apply_security_headers(response.headers_mut());
    response
}

/// `/`
pub async fn redirect_root() -> AxumResponse {
    let mut response = StatusCode::TEMPORARY_REDIRECT.into_response();
    if let Ok(value) = HeaderValue::from_str(&format!("/ui/modules/{DEFAULT_MODULE}/")) {
        response.headers_mut().insert(header::LOCATION, value);
    }
    response
}

/// Registers the UI endpoints, like Go `service/ui.registerAPIEndpoints`.
pub fn register_ui_endpoints(api: &Api) -> crate::Result<()> {
    let archives = api
        .ui_archives()
        .ok_or_else(|| crate::ApiError::Message("ui is not configured".to_string()))?;
    api.register_endpoint(
        crate::Endpoint::new(
            "ui/reload",
            "Reload UI Assets",
            "Removes all assets from the cache and reloads the current (possibly updated) version from disk when requested.",
        )
        .with_write(crate::PERMIT_USER)
        .with_action(move |_| {
            archives.close_archives();
            Ok("all ui archives successfully reloaded".to_string())
        }),
    )
}
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn content_types() {
        assert_eq!(content_type("index.html"), "text/html; charset=utf-8");
        assert_eq!(
            content_type("main.js"),
            "application/javascript; charset=utf-8"
        );
        assert_eq!(content_type("logo.PNG"), "image/png");
        assert_eq!(content_type("file.unknown"), "application/octet-stream");
        assert_eq!(content_type("no-extension"), "application/octet-stream");
    }

    #[test]
    fn resource_paths_are_cleaned() {
        assert_eq!(clean_resource_path("").as_deref(), Some("index.html"));
        assert_eq!(clean_resource_path("/a//b.js").as_deref(), Some("a/b.js"));
        assert_eq!(clean_resource_path("./a/./b").as_deref(), Some("a/b"));
        assert_eq!(clean_resource_path("../secret"), None);
        assert_eq!(clean_resource_path("a/../../secret"), None);
    }
}

