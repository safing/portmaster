//! Optional pilot UI served from `<data-dir>/pilot-web/`.
//!
//! The regular Portmaster UI keeps being served from the zip archives set up in
//! [`pm_api::ui`]. This module only adds the extra `/pilot` routes; when the
//! `pilot-web` directory does not exist those routes answer with 404 and every
//! other route behaves exactly as before.
//!
//! Routes:
//!
//! * `/pilot` and `/pilot/` serve `pilot-web/index.html`.
//! * `/pilot/<path>` serves `pilot-web/<path>` after traversal validation.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use axum::body::Body;
use axum::extract::{Extension, Path as UrlPath, State};
use axum::response::{IntoResponse, Response as AxumResponse};
use axum::Router;
use http::{header, HeaderValue, StatusCode};

use pm_api::Api;

/// Name of the pilot asset directory, relative to the core data directory.
pub const PILOT_ASSET_DIR: &str = "pilot-web";

/// Request extension carrying the absolute path to the pilot asset directory.
#[derive(Clone, Debug)]
pub struct PilotDir(PathBuf);

/// Normalizes a requested URL path into safe relative path segments.
///
/// Rejects absolute paths (both Unix and Windows style), any `..` component and
/// drive-letter prefixes. An empty path maps to `index.html`.
fn clean_pilot_path(requested: &str) -> Option<PathBuf> {
    if requested.starts_with('/') || requested.starts_with('\\') {
        return None;
    }

    let mut path = PathBuf::new();
    let mut empty = true;
    // Treat both separators as separators so Windows-style traversal cannot be
    // smuggled through on non-Windows hosts.
    for segment in requested.split(['/', '\\']) {
        match segment {
            "" | "." => {}
            ".." => return None,
            other => {
                if other.contains(':') {
                    return None;
                }
                path.push(other);
                empty = false;
            }
        }
    }
    if empty {
        path.push("index.html");
    }
    Some(path)
}

/// Resolves a requested path to a canonical file inside the pilot directory.
///
/// Canonicalization resolves symlinks, and the explicit prefix check makes sure
/// a symlink inside `pilot-web` cannot point outside of it.
fn resolve_pilot_file(pilot_dir: &Path, requested: &str) -> Option<(PathBuf, String)> {
    let relative = clean_pilot_path(requested)?;
    let root = std::fs::canonicalize(pilot_dir).ok()?;

    let candidate = std::fs::canonicalize(root.join(&relative)).ok()?;
    if !candidate.starts_with(&root) {
        return None;
    }

    // A path pointing at a directory resolves to that directory's index.
    let file = if candidate.is_dir() {
        std::fs::canonicalize(candidate.join("index.html")).ok()?
    } else {
        candidate
    };
    if !file.starts_with(&root) || !file.is_file() {
        return None;
    }

    let display = file.display().to_string();
    Some((file, display))
}

/// Applies the same security headers the regular API/UI responses get.
fn with_security_headers(api: &Api, mut response: AxumResponse) -> AxumResponse {
    api.apply_security_headers(response.headers_mut());
    response
}

/// Serves one pilot asset, or a 404 when it is missing or the directory is not
/// deployed.
async fn pilot_response(api: &Api, pilot_dir: &Path, requested: &str) -> AxumResponse {
    // Cheap existence check first: an absent directory is always a plain 404.
    if !pilot_dir.is_dir() {
        return with_security_headers(
            api,
            (StatusCode::NOT_FOUND, "pilot ui is not installed\n").into_response(),
        );
    }

    let Some((path, name)) = resolve_pilot_file(pilot_dir, requested) else {
        return with_security_headers(
            api,
            (StatusCode::NOT_FOUND, "pilot resource not found\n").into_response(),
        );
    };

    let data = match tokio::fs::read(&path).await {
        Ok(data) => data,
        Err(error) => {
            tracing::debug!(%error, path = %path.display(), "pilot: failed to read asset");
            return with_security_headers(
                api,
                (StatusCode::NOT_FOUND, "pilot resource not found\n").into_response(),
            );
        }
    };

    // Reuse the MIME mapping already used by the UI archive server; it covers
    // .html, .js, .wasm and .css (plus the other common web asset types).
    let content_type = pm_api::ui::content_type(&name);
    let mut response = AxumResponse::new(Body::from(data));
    *response.status_mut() = StatusCode::OK;
    if let Ok(value) = HeaderValue::from_str(content_type) {
        response.headers_mut().insert(header::CONTENT_TYPE, value);
    }
    with_security_headers(api, response)
}

/// `/pilot` and `/pilot/` -> `pilot-web/index.html`.
pub async fn serve_pilot_index(
    State(api): State<Arc<Api>>,
    Extension(dir): Extension<PilotDir>,
) -> AxumResponse {
    pilot_response(&api, &dir.0, "").await
}

/// `/pilot/<path>` -> `pilot-web/<path>`.
pub async fn serve_pilot_file(
    State(api): State<Arc<Api>>,
    Extension(dir): Extension<PilotDir>,
    UrlPath(path): UrlPath<String>,
) -> AxumResponse {
    pilot_response(&api, &dir.0, &path).await
}

pub async fn serve_pilot_js(
    State(api): State<Arc<Api>>,
    Extension(dir): Extension<PilotDir>,
) -> AxumResponse {
    pilot_response(&api, &dir.0, "pm_leptos_pilot.js").await
}

pub async fn serve_pilot_wasm(
    State(api): State<Arc<Api>>,
    Extension(dir): Extension<PilotDir>,
) -> AxumResponse {
    pilot_response(&api, &dir.0, "pm_leptos_pilot_bg.wasm").await
}

/// Builds just the optional pilot routes with `Arc<Api>` as router state.
///
/// This can be merged into the main API router, e.g.
/// `pm_api_router.merge(pilot_router(&data_dir))`, without touching any of the
/// existing UI or API routes.
pub fn pilot_router(data_dir: impl AsRef<Path>) -> Router<Arc<Api>> {
    let dir = PilotDir(data_dir.as_ref().join(PILOT_ASSET_DIR));
    let mut router = Router::new()
        .route("/pilot", axum::routing::get(serve_pilot_index))
        .route("/pilot/", axum::routing::get(serve_pilot_index))
        .route("/pilot/{*path}", axum::routing::get(serve_pilot_file));

    // UI type switch: PORTMASTER_UI_TYPE=rust makes the Rust/WASM pilot the
    // main UI at `/`, leaving the TypeScript UI one systemd override away.
    if std::env::var("PORTMASTER_UI_TYPE").as_deref() == Ok("rust") {
        router = router
            .route("/", axum::routing::get(serve_pilot_index))
            .route(
                "/pm_leptos_pilot.js",
                axum::routing::get(serve_pilot_js),
            )
            .route(
                "/pm_leptos_pilot_bg.wasm",
                axum::routing::get(serve_pilot_wasm),
            );
    } else {
        router = router.route("/", axum::routing::get(pm_api::ui::redirect_root));
    }
    router.layer(Extension(dir))
}

/// Fully assembled API server: the regular routes from `pm_api::serve` plus the
/// optional pilot routes.
///
/// UI/API handlers are reused from `pm_api`; the only thing mirrored here is
/// the private axum->API request bridge that `pm_api::serve` does not expose.
pub async fn serve(
    api: Arc<Api>,
    listener: tokio::net::TcpListener,
    data_dir: impl AsRef<Path>,
) -> std::io::Result<()> {
    let app = Router::new()
        .route(
            pm_api::database_ws::DB_API_PATH,
            axum::routing::get(pm_api::database_ws::database_ws_upgrade),
        )
        .route(
            "/assets/{*path}",
            axum::routing::get(pm_api::ui::serve_assets),
        )
        .route(
            "/ui/modules/{module}",
            axum::routing::get(pm_api::ui::redirect_module),
        )
        .route(
            "/ui/modules/{module}/",
            axum::routing::get(pm_api::ui::serve_module_index),
        )
        .route(
            "/ui/modules/{module}/{*path}",
            axum::routing::get(pm_api::ui::serve_module_file),
        )
        .merge(pilot_router(data_dir))
        .fallback(axum::routing::any(handle_axum))
        .with_state(api);

    axum::serve(
        listener,
        app.into_make_service_with_connect_info::<std::net::SocketAddr>(),
    )
    .await
}

/// Mirror of the private `pm_api::serve` fallback: converts an axum request to
/// an API request and runs it through the existing `Api` dispatcher.
async fn handle_axum(
    State(api): State<Arc<Api>>,
    request: axum::extract::Request,
) -> AxumResponse {
    let (parts, body) = request.into_parts();
    let body = axum::body::to_bytes(body, 20_000_000)
        .await
        .unwrap_or_default();

    let mut api_request = pm_api::Request::new(parts.method.as_str(), parts.uri.path());
    api_request.query_string = parts.uri.query().unwrap_or("").to_string();
    api_request.query = pm_api::parse_query(&api_request.query_string);
    api_request.host = parts
        .headers
        .get(header::HOST)
        .and_then(|value| value.to_str().ok())
        .unwrap_or("")
        .to_string();
    api_request.remote_addr = parts
        .extensions
        .get::<axum::extract::ConnectInfo<std::net::SocketAddr>>()
        .map(|info| info.0.to_string())
        .unwrap_or_default();
    api_request.header = parts.headers;
    api_request.body = body.to_vec();

    let response = match tokio::task::spawn_blocking(move || api.handle(api_request)).await {
        Ok(response) => response,
        Err(_) => pm_api::Response::error(500, "Internal server error."),
    };
    response.into_axum()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cleans_pilot_paths() {
        assert_eq!(
            clean_pilot_path("").unwrap().to_str(),
            Some("index.html")
        );
        assert_eq!(
            clean_pilot_path("app.js").unwrap().to_str(),
            Some("app.js")
        );
        assert_eq!(
            clean_pilot_path("assets/app.js").unwrap().to_str(),
            Some("assets/app.js")
        );
        assert_eq!(
            clean_pilot_path("/app.js"),
            None,
            "absolute paths must be rejected"
        );
        assert_eq!(clean_pilot_path(".."), None);
        assert_eq!(clean_pilot_path("a/../../secret"), None);
        assert_eq!(clean_pilot_path("a\\..\\secret"), None);
        assert_eq!(clean_pilot_path("C:/secret"), None);
    }

    #[test]
    fn resolves_only_files_inside_the_pilot_dir() {
        let root = std::env::temp_dir().join(format!("pm-core-pilot-test-{}", std::process::id()));
        let pilot = root.join(PILOT_ASSET_DIR);
        let nested = pilot.join("assets");
        std::fs::create_dir_all(&nested).unwrap();
        std::fs::write(pilot.join("index.html"), b"index").unwrap();
        std::fs::write(nested.join("app.js"), b"app").unwrap();
        std::fs::write(root.join("secret.txt"), b"secret").unwrap();

        let (index, _) = resolve_pilot_file(&pilot, "").unwrap();
        assert_eq!(index, std::fs::canonicalize(pilot.join("index.html")).unwrap());

        let (asset, _) = resolve_pilot_file(&pilot, "assets/app.js").unwrap();
        assert_eq!(asset, std::fs::canonicalize(nested.join("app.js")).unwrap());

        assert!(resolve_pilot_file(&pilot, "../secret.txt").is_none());
        assert!(resolve_pilot_file(&pilot, "missing.js").is_none());

        std::fs::remove_dir_all(&root).ok();
    }
}
