//! Functional tests of the registered endpoints and export→import round trips.

use std::sync::Arc;

use http::HeaderValue;
use pm_api::{Api, Request, PERMIT_ADMIN};
use pm_config::{Config, ConfigOption, SETTABLE_PER_APP_ANNOTATION};
use pm_database::{Databases, MemoryStorage};
use pm_profile::{Fingerprint, Profile, SOURCE_LOCAL};
use pm_sync::{ProfileExport, SettingsExport, Sync};
use serde_json::{json, Value};

struct TestEnv {
    api: Arc<Api>,
    sync: Arc<Sync>,
}

fn env() -> TestEnv {
    let config = Config::new();
    config
        .register(
            ConfigOption::new(
                "Default Action",
                "filter/defaultAction",
                "Default action",
                pm_config::OPT_TYPE_STRING,
                json!("ask"),
            )
            .with_annotation(SETTABLE_PER_APP_ANNOTATION, json!(true)),
        )
        .unwrap();
    config
        .register(ConfigOption::new(
            "Dev Mode",
            "core/devMode",
            "Dev mode",
            pm_config::OPT_TYPE_BOOL,
            json!(false),
        ))
        .unwrap();
    config.set_config_option("filter/defaultAction", json!("block")).unwrap();

    let databases = Arc::new(Databases::without_disk());
    databases.register_injected("core", "core database").unwrap();
    databases
        .inject("core", Box::new(MemoryStorage::new()))
        .unwrap();

    let sync = Sync::new(Arc::clone(&config), &databases, std::env::temp_dir().join("pm-sync-test-icons"));
    let api = Api::new("127.0.0.1:0");
    api.set_dev_mode(true); // full permissions for the tests
    sync.register_api(&api).unwrap();

    TestEnv { api, sync }
}

fn request(method: &str, path: &str, query: &str, body: Vec<u8>, content_type: &str) -> Request {
    let mut request = Request::new(method, path);
    request.query_string = query.to_string();
    request.query = pm_api::parse_query(query);
    request.body = body;
    if !content_type.is_empty() {
        request.header.insert(
            http::header::CONTENT_TYPE,
            HeaderValue::from_str(content_type).unwrap(),
        );
    }
    request
}

fn json_header(request: &mut Request, value: &str) {
    request.header.insert(
        http::header::ACCEPT,
        HeaderValue::from_str(value).unwrap(),
    );
}

#[test]
fn all_endpoints_registered_with_admin_permissions() {
    let test = env();
    let expected = [
        ("sync/settings/export", 2),
        ("sync/settings/import", 4),
        ("sync/single-setting/export", 2),
        ("sync/single-setting/import", 3),
        ("sync/profile/export", 1),
        ("sync/profile/import", 4),
    ];
    for (path, parameters) in expected {
        let endpoint = test.api.endpoint(path).unwrap_or_else(|| panic!("missing {path}"));
        assert_eq!(endpoint.read, PERMIT_ADMIN, "{path} read");
        assert_eq!(endpoint.write, PERMIT_ADMIN, "{path} write");
        assert_eq!(endpoint.parameters.len(), parameters, "{path} parameters");
        assert_eq!(endpoint.read_method, "GET", "{path} read method");
        assert_eq!(endpoint.write_method, "POST", "{path} write method");
    }
}

#[test]
fn settings_export_import_roundtrip() {
    let test = env();

    let mut req = request("GET", "/api/v1/sync/settings/export", "from=global", Vec::new(), "");
    json_header(&mut req, "application/json");
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());
    assert_eq!(
        response.headers.get(http::header::CONTENT_TYPE).unwrap(),
        "application/json"
    );
    let export: SettingsExport =
        pm_sync::parse_export(&response.body, "application/json").unwrap().0;
    assert_eq!(export.type_, "settings");
    assert_eq!(export.config["filter"]["defaultAction"], json!("block"));

    // Validate-only import of the raw export.
    let raw = String::from_utf8(response.body).unwrap();
    let req = request(
        "POST",
        "/api/v1/sync/settings/import",
        "to=global&validate",
        raw.clone().into_bytes(),
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());
    let result: Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(result["containsUnknown"], json!(false));
    assert_eq!(result["restartRequired"], json!(false));

    // Unknown settings are rejected unless allowUnknown is set.
    let rebuild = SettingsExport {
        type_: "settings".to_string(),
        config: json!({"unknown/key": "x"}).as_object().cloned().unwrap(),
    };
    let (data, _) = pm_sync::serialize_export(&rebuild, "application/json").unwrap();
    let req = request(
        "POST",
        "/api/v1/sync/settings/import",
        "to=global",
        data,
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 422, "{}", response.body_string());
    assert!(response.body_string().contains("the export contains unknown settings"));
}

#[test]
fn profile_export_import_roundtrip() {
    let test = env();

    let profile = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "Test App".to_string(),
        description: "Desc".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/usr/bin/test")],
        config: json!({"filter/defaultAction": "block"}).as_object().cloned().unwrap(),
        ..Profile::default()
    });
    test.sync.profiles.save(&profile).unwrap();

    let query = format!("id={}", profile.scoped_id());
    let mut req = request("GET", "/api/v1/sync/profile/export", &query, Vec::new(), "");
    json_header(&mut req, "application/json");
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());

    let export: ProfileExport =
        pm_sync::parse_export(&response.body, "application/json").unwrap().0;
    assert_eq!(export.type_, "profile");
    assert_eq!(export.id, profile.id);
    assert_eq!(export.name, "Test App");
    assert_eq!(export.fingerprints.len(), 1);

    let raw = String::from_utf8(response.body).unwrap();
    // Validate-only import.
    let req = request(
        "POST",
        "/api/v1/sync/profile/import",
        "validate",
        raw.clone().into_bytes(),
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());
    let result: Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(result["replacesProfiles"], json!([]));
    // The profile we exported is still in the database, so it would be replaced.
    assert_eq!(result["replacesExisting"], json!(true));

    // Real import of a modified copy: change the path so it is a new profile.
    let mut modified = export.clone();
    modified.fingerprints[0].value = "/usr/bin/other".to_string();
    modified.id.clear();
    // Body-only imports wrap the export in a ProfileImportRequest, like Go.
    let import_body = |allow_replace: bool| -> Vec<u8> {
        json!({
            "export": &modified,
            "allowUnknown": false,
            "allowReplaceProfiles": allow_replace,
        })
        .to_string()
        .into_bytes()
    };
    let req = request(
        "POST",
        "/api/v1/sync/profile/import",
        "",
        import_body(false),
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());

    let mut derivation_fingerprints = vec![Fingerprint::new("path", "", "equals", "/usr/bin/other")];
    let imported_id = pm_profile::derive_profile_id(&mut derivation_fingerprints);
    let imported = test.sync.profiles.get(&format!("local/{imported_id}")).unwrap();
    assert_eq!(imported.name, "Test App");
    assert_eq!(imported.fingerprints[0].value, "/usr/bin/other");

    // Re-importing the same profile without allowReplaceProfiles fails.
    let req = request(
        "POST",
        "/api/v1/sync/profile/import",
        "",
        import_body(false),
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 500, "{}", response.body_string());
    assert!(response.body_string().contains("import would replace existing profile"));

    // With allowReplaceProfiles it succeeds.
    let req = request(
        "POST",
        "/api/v1/sync/profile/import",
        "",
        import_body(true),
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 200, "{}", response.body_string());
    let result: Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(result["replacesExisting"], json!(true));
}

#[test]
fn wrong_type_is_rejected() {
    let test = env();
    let wrong = SettingsExport {
        type_: "profile".to_string(),
        config: json!({"filter/defaultAction": "block"}).as_object().cloned().unwrap(),
    };
    let (data, _) = pm_sync::serialize_export(&wrong, "application/json").unwrap();
    let req = request(
        "POST",
        "/api/v1/sync/settings/import",
        "to=global",
        data,
        "application/json",
    );
    let response = test.api.handle(req);
    assert_eq!(response.status, 412, "{}", response.body_string());
    assert_eq!(
        response.body_string().trim(),
        "the supplied export cannot be imported here"
    );
}
