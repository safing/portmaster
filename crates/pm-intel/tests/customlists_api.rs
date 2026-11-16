//! Tests for the `customlists/update` API endpoint.

use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;

use pm_api::{Api, Request};
use pm_intel::customlists::{register_api, CustomListService, UPDATE_ENDPOINT_PATH};

static COUNTER: AtomicU32 = AtomicU32::new(0);

struct TempFile(PathBuf);

impl TempFile {
    fn with(content: &str) -> TempFile {
        let name = format!(
            "pm-intel-customlists-api-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        );
        let path = std::env::temp_dir().join(name);
        let mut file = std::fs::File::create(&path).expect("create temp file");
        file.write_all(content.as_bytes()).expect("write list");
        TempFile(path)
    }

    fn path_str(&self) -> &str {
        self.0.to_str().expect("utf-8 path")
    }
}

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

fn api_with(service: Arc<CustomListService>) -> Arc<Api> {
    let api = Api::new("127.0.0.1:817");
    api.set_dev_mode(true);
    register_api(&api, service).expect("register endpoint");
    api
}

#[test]
fn endpoint_metadata_matches_go() {
    let api = api_with(Arc::new(CustomListService::new()));

    let endpoint = api.endpoint(UPDATE_ENDPOINT_PATH).expect("endpoint");
    assert_eq!(endpoint.name, "Update custom filter list");
    assert_eq!(
        endpoint.description,
        "Reload the filter list from the configured file."
    );
    // Go `api.PermitUser`.
    assert_eq!(endpoint.write, pm_api::PERMIT_USER);
}

#[test]
fn unconfigured_list_returns_the_go_error() {
    let api = api_with(Arc::new(CustomListService::new()));

    let response = api.handle(Request::new("POST", format!("/api/v1/{UPDATE_ENDPOINT_PATH}")));
    assert_ne!(response.status, 200);
    let body = String::from_utf8_lossy(&response.body);
    assert!(
        body.contains("custom filter list not configured"),
        "unexpected body: {body}"
    );
}

#[test]
fn reloads_the_configured_file() {
    let file = TempFile::with("example.com\n1.2.3.4\n");
    let service = Arc::new(CustomListService::new());
    service.set_file_path(file.path_str());
    let api = api_with(Arc::clone(&service));

    let response = api.handle(Request::new("POST", format!("/api/v1/{UPDATE_ENDPOINT_PATH}")));
    assert_eq!(response.status, 200);
    assert_eq!(
        String::from_utf8_lossy(&response.body),
        "Custom filter list loaded successfully.\n"
    );
    assert!(service.lists().is_loaded());

    // A second call finds an unchanged file and still reports success.
    let response = api.handle(Request::new("POST", format!("/api/v1/{UPDATE_ENDPOINT_PATH}")));
    assert_eq!(response.status, 200);
}
