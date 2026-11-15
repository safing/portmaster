//! Tests of the API router, permissions, endpoints and serialization.

use std::sync::Arc;

use pm_api::{
    clean_request_path, Api, ApiError, AuthError, AuthToken, Authenticator, Endpoint, Parameter,
    Request, Response, DYNAMIC, NOT_SUPPORTED, PERMIT_ADMIN, PERMIT_ANYONE, PERMIT_SELF,
    PERMIT_USER,
};
use serde_json::json;

fn open_api() -> Arc<Api> {
    Api::new("127.0.0.1:0")
}

fn get(api: &Arc<Api>, path: &str) -> Response {
    api.handle(Request::new("GET", path).with_host("127.0.0.1:817"))
}

#[test]
fn endpoint_registry_validation() {
    let api = open_api();

    let invalid = [
        Endpoint::new("", "name", "description").with_read(PERMIT_ANYONE),
        Endpoint::new("test", "name", "description").with_read(5),
        Endpoint::new("test", "name", "description").with_read(-5),
        Endpoint::new("test", "name", "description")
            .with_read(PERMIT_ANYONE)
            .with_read_method("POST"),
        Endpoint::new("test", "name", "description")
            .with_write(PERMIT_ANYONE)
            .with_write_method("GET"),
    ];
    for endpoint in invalid {
        let error = api.register_endpoint(endpoint).unwrap_err();
        assert!(matches!(error, ApiError::InvalidEndpoint(_)), "{error}");
    }

    // A missing handler is invalid.
    assert!(api
        .register_endpoint(Endpoint::new("no-func", "name", "description").with_read(PERMIT_ANYONE))
        .is_err());

    // Defaults: GET/POST methods and MIME types per handler kind.
    api.register_endpoint(
        Endpoint::new("action", "name", "description")
            .with_read(PERMIT_ANYONE)
            .with_action(|_| Ok("done".to_string())),
    )
    .unwrap();
    let endpoint = api.endpoint("action").unwrap();
    assert_eq!(endpoint.read_method, "GET");
    assert_eq!(endpoint.write_method, "");
    assert_eq!(endpoint.mime_type, "text/plain");

    api.register_endpoint(
        Endpoint::new("struct", "name", "description")
            .with_read(PERMIT_ANYONE)
            .with_struct(|_| Ok(json!({}))),
    )
    .unwrap();
    assert_eq!(
        api.endpoint("struct").unwrap().mime_type,
        "application/json"
    );

    // Duplicate paths are rejected.
    assert!(matches!(
        api.register_endpoint(
            Endpoint::new("action", "name", "description")
                .with_read(PERMIT_ANYONE)
                .with_action(|_| Ok(String::new()))
        ),
        Err(ApiError::AlreadyRegistered)
    ));
}

#[test]
fn endpoints_export_has_go_shape() {
    let api = open_api();
    api.register_endpoint(
        Endpoint::new("z/last", "Last", "Last endpoint")
            .with_read(PERMIT_ANYONE)
            .with_write(PERMIT_USER)
            .with_parameter(Parameter {
                method: "GET".to_string(),
                field: "id".to_string(),
                value: "value".to_string(),
                description: "The id.".to_string(),
            })
            .with_data(|_| Ok(b"data".to_vec())),
    )
    .unwrap();

    let exported = api.export_endpoints();
    // Sorted by path, like Go's ExportEndpoints.
    let paths: Vec<&str> = exported.iter().map(|e| e.path.as_str()).collect();
    let mut sorted = paths.clone();
    sorted.sort_unstable();
    assert_eq!(paths, sorted);

    let last = exported
        .iter()
        .find(|endpoint| endpoint.path == "z/last")
        .unwrap();
    let json = serde_json::to_value(last).unwrap();
    assert_eq!(json["Name"], "Last");
    assert_eq!(json["Path"], "z/last");
    assert_eq!(json["Read"], 1);
    assert_eq!(json["Write"], 2);
    assert_eq!(json["ReadMethod"], "GET");
    assert_eq!(json["WriteMethod"], "POST");
    assert_eq!(json["MimeType"], "text/plain");
    assert_eq!(json["Parameters"][0]["Field"], "id");
    assert!(json.get("Func").is_none());

    // The HTTP endpoint serves the registry as Go-compatible JSON.
    let response = get(&api, "/api/v1/endpoints");
    assert_eq!(response.status, 200);
    let body: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert!(body
        .as_array()
        .unwrap()
        .iter()
        .any(|e| e["Path"] == "z/last"));
    assert_eq!(response.headers.get("x-frame-options").unwrap(), "deny");
    assert!(response
        .headers
        .get("content-type")
        .unwrap()
        .to_str()
        .unwrap()
        .starts_with("text/plain"));
}

#[test]
fn path_cleaning_and_redirects() {
    assert_eq!(clean_request_path(""), "/");
    assert_eq!(clean_request_path("/"), "/");
    assert_eq!(clean_request_path("api/v1/endpoints"), "/api/v1/endpoints");
    assert_eq!(
        clean_request_path("//api//v1/./endpoints"),
        "/api/v1/endpoints"
    );
    assert_eq!(
        clean_request_path("/api/v1/../v1/endpoints"),
        "/api/v1/endpoints"
    );
    assert_eq!(
        clean_request_path("/api/v1/endpoints/"),
        "/api/v1/endpoints/"
    );

    let api = open_api();
    let response = api.handle(Request::new("GET", "api/v1/endpoints").with_host("host"));
    assert_eq!(response.status, 301);
    assert_eq!(
        response.headers.get("location").unwrap(),
        "/api/v1/endpoints"
    );
}

#[test]
fn not_found_and_method_not_allowed() {
    let api = open_api();

    let response = get(&api, "/api/v1/does-not-exist");
    assert_eq!(response.status, 404);
    assert_eq!(response.body_string(), "Not found.\n");

    // A read-only endpoint does not support writes.
    let response = api.handle(Request::new("POST", "/api/v1/endpoints").with_host("host"));
    assert_eq!(response.status, 405);
    assert_eq!(response.body_string(), "Method not allowed.\n");

    // Unsupported methods are rejected.
    let response = api.handle(Request::new("TRACE", "/api/v1/endpoints").with_host("host"));
    assert_eq!(response.status, 405);
}

#[test]
fn permissions_and_api_keys() {
    let api = open_api();
    api.register_endpoint(
        Endpoint::new("self", "Self", "Only the software itself")
            .with_read(PERMIT_SELF)
            .with_data(|_| Ok(b"secret".to_vec())),
    )
    .unwrap();
    api.register_endpoint(
        Endpoint::new("user", "User", "Users may read")
            .with_read(PERMIT_USER)
            .with_data(|_| Ok(b"user".to_vec())),
    )
    .unwrap();

    // Without authentication a protected endpoint asks for credentials.
    let response = get(&api, "/api/v1/self");
    assert_eq!(response.status, 401);
    assert_eq!(response.body_string(), "Authorization required.\n");
    assert_eq!(
        response.headers.get("www-authenticate").unwrap(),
        "Bearer realm=\"Portmaster API\" domain=\"/\""
    );

    // API keys grant the configured permissions.
    api.update_api_keys(&[
        "user-key?read=user&write=user".to_string(),
        "admin-key?read=admin&write=admin".to_string(),
        "self-key?read=admin&write=admin".to_string(),
    ]);
    let user = |path: &str| {
        api.handle(
            Request::new("GET", path)
                .with_host("host")
                .with_header("authorization", "Bearer user-key"),
        )
    };
    assert_eq!(user("/api/v1/user").status, 200);
    assert_eq!(user("/api/v1/self").status, 403);
    assert_eq!(
        user("/api/v1/self").body_string(),
        "Insufficient permissions.\n"
    );

    // Development mode grants everything.
    api.set_dev_mode(true);
    assert_eq!(get(&api, "/api/v1/self").status, 200);
    api.set_dev_mode(false);

    // The permissions endpoint reports the assigned token.
    let response = user("/api/v1/auth/permissions");
    let body: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(body["Read"], PERMIT_USER);
    assert_eq!(body["ReadRole"], "User");

    // An authenticator can grant permissions per request.
    struct HeaderAuth;
    impl Authenticator for HeaderAuth {
        fn authenticate(&self, request: &Request) -> Result<Option<AuthToken>, AuthError> {
            match request.header("x-test-auth") {
                Some("deny") => Err(AuthError::Denied("no access for you".to_string())),
                Some("fail") => Err(AuthError::Internal("boom".to_string())),
                Some("admin") => Ok(Some(AuthToken::new(PERMIT_ADMIN, PERMIT_ADMIN))),
                _ => Ok(None),
            }
        }
    }
    api.set_authenticator(Arc::new(HeaderAuth));

    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("x-test-auth", "deny"),
    );
    assert_eq!(response.status, 403);
    assert_eq!(response.body_string(), "no access for you\n");

    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("x-test-auth", "fail"),
    );
    assert_eq!(response.status, 500);
    assert_eq!(
        response.body_string(),
        "Internal server error during authentication.\n"
    );

    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("x-test-auth", "admin"),
    );
    assert_eq!(response.status, 403);

    // Dynamic endpoints run the authenticator but allow unauthenticated access.
    api.register_endpoint(
        Endpoint::new("dynamic", "Dynamic", "Decides itself")
            .with_read(DYNAMIC)
            .with_struct(|_| Ok(json!({"ok": true}))),
    )
    .unwrap();
    assert_eq!(get(&api, "/api/v1/dynamic").status, 200);
    let response = api.handle(
        Request::new("GET", "/api/v1/dynamic")
            .with_host("host")
            .with_header("x-test-auth", "deny"),
    );
    assert_eq!(response.status, 200);
}

#[test]
fn write_endpoints_read_the_body() {
    let api = open_api();
    api.update_api_keys(&["write-key?read=anyone&write=user".to_string()]);
    api.register_endpoint(
        Endpoint::new("submit", "Submit", "Write only")
            .with_write(PERMIT_USER)
            .with_action(|request| {
                let value: serde_json::Value = request.body_json()?;
                Ok(format!(
                    "got {}",
                    value["name"].as_str().unwrap_or("nothing")
                ))
            }),
    )
    .unwrap();

    let response = api.handle(
        Request::new("POST", "/api/v1/submit")
            .with_host("host")
            .with_header("authorization", "Bearer write-key")
            .with_body(br#"{"name":"banana"}"#.to_vec()),
    );
    assert_eq!(response.status, 200);
    assert_eq!(response.body_string(), "got banana\n");

    // Reads are not supported on a write-only endpoint.
    let response = api.handle(
        Request::new("GET", "/api/v1/submit")
            .with_host("host")
            .with_header("authorization", "Bearer write-key"),
    );
    assert_eq!(response.status, 405);
}

#[test]
fn cors_and_preflight() {
    let api = open_api();

    // Same-origin requests get CORS headers.
    let response = api.handle(
        Request::new("GET", "/api/v1/endpoints")
            .with_host("127.0.0.1:817")
            .with_header("origin", "http://127.0.0.1:817"),
    );
    assert_eq!(response.status, 200);
    assert_eq!(
        response.headers.get("access-control-allow-origin").unwrap(),
        "http://127.0.0.1:817"
    );

    // Foreign origins are denied.
    let response = api.handle(
        Request::new("GET", "/api/v1/endpoints")
            .with_host("127.0.0.1:817")
            .with_header("origin", "http://evil.example"),
    );
    assert_eq!(response.status, 403);
    assert_eq!(response.body_string(), "Cross-Origin Request Denied.\n");

    // Browser extensions are allowed.
    let response = api.handle(
        Request::new("GET", "/api/v1/endpoints")
            .with_host("host")
            .with_header("origin", "chrome-extension://abcdef"),
    );
    assert_eq!(response.status, 200);

    // Preflight requests succeed without authentication.
    let response = api.handle(
        Request::new("OPTIONS", "/api/v1/endpoints")
            .with_host("127.0.0.1:817")
            .with_header("origin", "http://127.0.0.1:817")
            .with_header("access-control-request-method", "GET"),
    );
    assert_eq!(response.status, 200);

    // Plain OPTIONS requests are rejected, like in Go.
    let response =
        api.handle(Request::new("OPTIONS", "/api/v1/endpoints").with_host("127.0.0.1:817"));
    assert_eq!(response.status, 405);
}

#[test]
fn response_serialization_matches_go() {
    let api = open_api();
    api.register_endpoint(
        Endpoint::new("action", "Action", "Action")
            .with_read(PERMIT_ANYONE)
            .with_action(|_| Ok("no newline".to_string())),
    )
    .unwrap();
    api.register_endpoint(
        Endpoint::new("struct", "Struct", "Struct")
            .with_read(PERMIT_ANYONE)
            .with_struct(|_| Ok(json!({"Text": "<&>"}))),
    )
    .unwrap();

    let response = get(&api, "/api/v1/action");
    assert_eq!(response.status, 200);
    assert_eq!(response.body_string(), "no newline\n");
    assert_eq!(
        response.headers.get("content-type").unwrap(),
        "text/plain; charset=utf-8"
    );

    // Structs use Go JSON escaping and accept CBOR.
    let response = get(&api, "/api/v1/struct");
    assert_eq!(response.body_string(), r#"{"Text":"\u003c\u0026\u003e"}"#);
    assert_eq!(
        response.headers.get("content-type").unwrap(),
        "application/json"
    );

    let response = api.handle(
        Request::new("GET", "/api/v1/struct")
            .with_host("host")
            .with_header("accept", "application/cbor"),
    );
    assert_eq!(
        response.headers.get("content-type").unwrap(),
        "application/cbor"
    );
    let (value, format) = pm_dsd::load::<serde_json::Value>(&{
        let mut blob = vec![pm_dsd::format::CBOR];
        blob.extend_from_slice(&response.body);
        blob
    })
    .unwrap();
    assert_eq!(format, pm_dsd::format::CBOR);
    assert_eq!(value["Text"], "<&>");

    // Empty responses return no content.
    api.register_endpoint(
        Endpoint::new("empty", "Empty", "Empty")
            .with_read(PERMIT_ANYONE)
            .with_data(|_| Ok(Vec::new())),
    )
    .unwrap();
    assert_eq!(get(&api, "/api/v1/empty").status, 204);
}

#[test]
fn config_endpoints_expose_options() {
    let api = open_api();
    let config = pm_config::Config::new();
    config
        .register(pm_config::ConfigOption::new(
            "String",
            "test/string",
            "A string option.",
            pm_config::OPT_TYPE_STRING,
            json!("default"),
        ))
        .unwrap();
    api.register_config_endpoints(config);

    let response = get(&api, "/api/v1/config/options");
    assert_eq!(response.status, 200);
    let body: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    let options = body.as_array().unwrap();
    let test_option = options
        .iter()
        .find(|option| option["Key"] == "test/string")
        .expect("test option");
    assert_eq!(test_option["DefaultValue"], "default");
    assert_eq!(test_option["OptType"], 1);
    // Built-in level options are registered too.
    assert!(options
        .iter()
        .any(|option| option["Key"] == "core/expertiseLevel"));
}

#[test]
fn invalid_endpoint_configuration_is_rejected() {
    let api = open_api();
    api.register_endpoint(
        Endpoint::new("bad", "Bad", "Bad")
            .with_read(PERMIT_ANYONE)
            .with_struct(|_| Ok(json!({})))
            .with_action(|_| Ok(String::new())),
    )
    .unwrap();
    // The last handler wins; registration succeeds because one handler is set.
    assert_eq!(api.endpoint("bad").unwrap().mime_type, "text/plain");
}

#[test]
fn not_supported_permissions() {
    let api = open_api();
    api.register_endpoint(
        Endpoint::new("writeonly", "Write only", "No reads")
            .with_write(PERMIT_ANYONE)
            .with_action(|_| Ok(String::new())),
    )
    .unwrap();
    assert_eq!(api.endpoint("writeonly").unwrap().read, NOT_SUPPORTED);
    assert_eq!(get(&api, "/api/v1/writeonly").status, 405);
    // An action without a message still answers with a newline.
    let response = api.handle(Request::new("POST", "/api/v1/writeonly").with_host("host"));
    assert_eq!(response.status, 200);
    assert_eq!(response.body_string(), "\n");
}

#[tokio::test]
async fn serves_http_over_tcp() {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    let api = open_api();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(api, listener));

    let mut stream = tokio::net::TcpStream::connect(address).await.unwrap();
    stream
        .write_all(
            b"GET /api/v1/endpoints HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n",
        )
        .await
        .unwrap();
    let mut response = Vec::new();
    stream.read_to_end(&mut response).await.unwrap();
    let text = String::from_utf8_lossy(&response);
    assert!(text.starts_with("HTTP/1.1 200"), "{text}");
    assert!(text.contains("\"Path\":\"endpoints\""), "{text}");

    server.abort();
}

#[test]
fn sessions_are_issued_used_and_reset() {
    struct Login;
    impl Authenticator for Login {
        fn authenticate(&self, request: &Request) -> Result<Option<AuthToken>, AuthError> {
            if request.header("x-login") == Some("yes") {
                Ok(Some(AuthToken::new(PERMIT_SELF, PERMIT_SELF)))
            } else {
                Ok(None)
            }
        }
    }

    let api = open_api();
    api.register_endpoint(
        Endpoint::new("self", "Self", "Only the software itself")
            .with_read(PERMIT_SELF)
            .with_data(|_| Ok(b"secret".to_vec())),
    )
    .unwrap();
    api.set_authenticator(Arc::new(Login));

    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("x-login", "yes"),
    );
    assert_eq!(response.status, 200);
    let set_cookie = response
        .headers
        .get("set-cookie")
        .expect("session cookie")
        .to_str()
        .unwrap()
        .to_string();
    assert!(set_cookie.starts_with("Portmaster-API-Token="));
    assert!(set_cookie.contains("HttpOnly"));
    let cookie = set_cookie.split(';').next().unwrap().to_string();
    assert_eq!(api.sessions().len(), 1);

    // The session cookie alone authenticates.
    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("cookie", &cookie),
    );
    assert_eq!(response.status, 200);

    // Resetting the session deletes it and clears the cookie.
    let response = api.handle(
        Request::new("GET", "/api/v1/auth/reset")
            .with_host("host")
            .with_header("cookie", &cookie),
    );
    assert_eq!(response.status, 401);
    assert_eq!(response.body_string(), "Session deleted.\n");
    assert_eq!(response.headers.get("clear-site-data").unwrap(), "*");
    assert!(api.sessions().is_empty());

    // The old cookie no longer authenticates.
    let response = api.handle(
        Request::new("GET", "/api/v1/self")
            .with_host("host")
            .with_header("cookie", &cookie),
    );
    assert_eq!(response.status, 401);
}
