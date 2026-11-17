//! Compatible implementation of the Go `base/api` package.
//!
//! The API listens on `127.0.0.1:817` and serves endpoints below `/api/v1/`.
//! Requests are routed by path, authenticated by permissions and serialized like
//! the Go implementation.
//!
//! # Compatibility notes
//!
//! * Path patterns support `{name}` segments, which covers the registered
//!   endpoints; gorilla-specific regex patterns are not supported.
//! * Session cookies, the database WebSocket bridge and the UI zip server are
//!   not implemented yet; API keys, the authenticator hook and development mode
//!   are.
//! * `expires` in API keys is parsed and enforced; expired keys are dropped from
//!   the registry and never authorize a request.
//! * Error and content-type behavior of the Go router (security headers, CORS
//!   origin checks, permission replies) is kept.

pub mod database_ws;
mod endpoint;
mod permission;
mod session;
pub mod ui;

use std::collections::{BTreeMap, HashMap};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, RwLock};

use http::{HeaderMap, HeaderValue};
use serde::de::DeserializeOwned;
use serde_json::Value;

pub use database_ws::DatabaseApi;
pub use endpoint::{
    public_endpoint, ActionFunc, DataFunc, Endpoint, EndpointFunc, HandlerFunc, Parameter,
    RecordFunc, StructFunc, MIME_TYPE_JSON, MIME_TYPE_TEXT,
};
pub use permission::{
    bearer_token, parse_api_keys, parse_permission, permission_name, permission_role, AuthError,
    AuthProcess, AuthToken, Authenticator, Permission, DYNAMIC, NOT_FOUND, NOT_SUPPORTED, PERMIT_ADMIN,
    PERMIT_ANYONE, PERMIT_SELF, PERMIT_USER,
};
pub use session::{session_cookie, Sessions, SESSION_COOKIE_NAME};
pub use ui::UiArchives;

/// Errors of the API layer.
#[derive(Debug, thiserror::Error)]
pub enum ApiError {
    #[error("{0}")]
    Message(String),
    #[error("{message}")]
    WithStatus { status: u16, message: String },
    #[error("endpoint is invalid: {0}")]
    InvalidEndpoint(String),
    #[error("an endpoint for this path is already registered")]
    AlreadyRegistered,
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("record: {0}")]
    Record(#[from] pm_record::Error),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
}

impl ApiError {
    pub fn status(&self) -> u16 {
        match self {
            ApiError::WithStatus { status, .. } => *status,
            _ => 500,
        }
    }

    /// Adds an HTTP status code to the error.
    #[must_use]
    pub fn with_status(self, status: u16) -> ApiError {
        ApiError::WithStatus {
            status,
            message: self.to_string(),
        }
    }
}

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, ApiError>;

const API_V1_PATH: &str = "/api/v1/";
const AUTH_REQUIRED_HEADER: &str = "Bearer realm=\"Portmaster API\" domain=\"/\"";

/// An API request, including the values filled in by the router.
#[derive(Debug)]
pub struct Request {
    /// Raw request method.
    pub method: String,
    /// Request path, cleaned by the router.
    pub path: String,
    /// Raw query string.
    pub query_string: String,
    /// Parsed query parameters.
    pub query: BTreeMap<String, String>,
    /// Value of the Host header.
    pub host: String,
    /// Request headers.
    pub header: HeaderMap,
    /// Request body.
    pub body: Vec<u8>,
    /// Remote address of the caller.
    pub remote_addr: String,
    /// Effective method after resolving CORS preflight requests.
    pub effective_method: String,
    /// Whether the effective method is a read method.
    pub read_method: bool,
    /// Path of the matched endpoint.
    pub endpoint_path: String,
    /// Path variables captured from the endpoint pattern.
    pub params: BTreeMap<String, String>,
    /// Authentication token assigned by the router.
    pub auth: Option<AuthToken>,
    /// Headers that handlers may add to the response.
    pub response_header: HeaderMap,
}

impl Request {
    pub fn new(method: impl Into<String>, path: impl Into<String>) -> Request {
        Request {
            method: method.into(),
            path: path.into(),
            query_string: String::new(),
            query: BTreeMap::new(),
            host: String::new(),
            header: HeaderMap::new(),
            body: Vec::new(),
            remote_addr: String::new(),
            effective_method: String::new(),
            read_method: false,
            endpoint_path: String::new(),
            params: BTreeMap::new(),
            auth: None,
            response_header: HeaderMap::new(),
        }
    }

    #[must_use]
    pub fn with_host(mut self, host: &str) -> Request {
        self.host = host.to_string();
        self
    }

    #[must_use]
    pub fn with_remote_addr(mut self, address: &str) -> Request {
        self.remote_addr = address.to_string();
        self
    }

    #[must_use]
    pub fn with_header(mut self, name: &str, value: &str) -> Request {
        if let (Ok(name), Ok(value)) = (
            name.parse::<http::HeaderName>(),
            HeaderValue::from_str(value),
        ) {
            self.header.insert(name, value);
        }
        self
    }

    #[must_use]
    pub fn with_body(mut self, body: Vec<u8>) -> Request {
        self.body = body;
        self
    }

    #[must_use]
    pub fn with_query(mut self, query: &str) -> Request {
        self.query_string = query.to_string();
        self.query = parse_query(query);
        self
    }

    /// Returns a request header.
    pub fn header(&self, name: &str) -> Option<&str> {
        self.header.get(name)?.to_str().ok()
    }

    /// Returns a path variable.
    pub fn param(&self, name: &str) -> Option<&str> {
        self.params.get(name).map(String::as_str)
    }

    /// Returns the authenticated caller, if the authenticator identified it.
    pub fn auth(&self) -> Option<&AuthToken> {
        self.auth.as_ref()
    }

    /// Returns a query parameter.
    pub fn query_param(&self, name: &str) -> Option<&str> {
        self.query.get(name).map(String::as_str)
    }

    /// Decodes the request body as JSON.
    pub fn body_json<T: DeserializeOwned>(&self) -> Result<T> {
        Ok(serde_json::from_slice(&self.body)?)
    }
}

/// An API response.
#[derive(Debug, Clone)]
pub struct Response {
    pub status: u16,
    pub headers: HeaderMap,
    pub body: Vec<u8>,
}

impl Response {
    pub fn new(status: u16) -> Response {
        Response {
            status,
            headers: HeaderMap::new(),
            body: Vec::new(),
        }
    }

    /// Creates a response with a body and content type.
    pub fn body(status: u16, body: Vec<u8>, content_type: &str) -> Response {
        let mut response = Response::new(status);
        if let Ok(value) = HeaderValue::from_str(content_type) {
            response.headers.insert(http::header::CONTENT_TYPE, value);
        }
        response.body = body;
        response
    }

    /// Creates an error response like Go `http.Error`: message plus newline.
    pub fn error(status: u16, message: &str) -> Response {
        let mut response = Response::new(status);
        response.headers.insert(
            http::header::CONTENT_TYPE,
            HeaderValue::from_static("text/plain; charset=utf-8"),
        );
        response.headers.insert(
            http::HeaderName::from_static("x-content-type-options"),
            HeaderValue::from_static("nosniff"),
        );
        let mut body = message.as_bytes().to_vec();
        body.push(b'\n');
        response.body = body;
        response
    }

    #[must_use]
    pub fn with_header(mut self, name: &str, value: &str) -> Response {
        if let (Ok(name), Ok(value)) = (
            name.parse::<http::HeaderName>(),
            HeaderValue::from_str(value),
        ) {
            self.headers.insert(name, value);
        }
        self
    }

    pub fn body_string(&self) -> String {
        String::from_utf8_lossy(&self.body).into_owned()
    }

    /// Converts the response into an axum response.
    pub fn into_axum(self) -> axum::response::Response {
        let mut builder = http::Response::builder().status(
            http::StatusCode::from_u16(self.status)
                .unwrap_or(http::StatusCode::INTERNAL_SERVER_ERROR),
        );
        for (name, value) in self.headers.iter() {
            builder = builder.header(name, value);
        }
        builder
            .body(axum::body::Body::from(self.body))
            .unwrap_or_else(|_| {
                http::Response::builder()
                    .status(http::StatusCode::INTERNAL_SERVER_ERROR)
                    .body(axum::body::Body::empty())
                    .expect("static response")
            })
    }
}

/// The API server state: endpoint registry, keys and authenticator.
pub struct Api {
    endpoints: RwLock<BTreeMap<String, Arc<Endpoint>>>,
    /// Raw handlers served at exact paths outside `/api/v1/`.
    handlers: RwLock<BTreeMap<String, HandlerFunc>>,
    api_keys: RwLock<HashMap<String, AuthToken>>,
    authenticator: RwLock<Option<Arc<dyn Authenticator>>>,
    databases: RwLock<Option<Arc<pm_database::Databases>>>,
    ui_archives: RwLock<Option<Arc<ui::UiArchives>>>,
    sessions: session::Sessions,
    ready: AtomicBool,
    dev_mode: AtomicBool,
    listen_address: String,
}

impl Api {
    /// Creates the API and registers the meta endpoints.
    pub fn new(listen_address: &str) -> Arc<Api> {
        let api = Arc::new(Api {
            endpoints: RwLock::new(BTreeMap::new()),
            handlers: RwLock::new(BTreeMap::new()),
            api_keys: RwLock::new(HashMap::new()),
            authenticator: RwLock::new(None),
            databases: RwLock::new(None),
            ui_archives: RwLock::new(None),
            sessions: session::Sessions::new(),
            ready: AtomicBool::new(false),
            dev_mode: AtomicBool::new(false),
            listen_address: listen_address.to_string(),
        });
        api.register_meta_endpoints();
        api
    }

    /// Enables development mode, which grants full permissions and relaxes CORS.
    pub fn set_dev_mode(&self, dev_mode: bool) {
        self.dev_mode.store(dev_mode, Ordering::SeqCst);
    }

    pub fn dev_mode(&self) -> bool {
        self.dev_mode.load(Ordering::SeqCst)
    }

    pub fn listen_address(&self) -> &str {
        &self.listen_address
    }

    /// Registers an endpoint.
    pub fn register_endpoint(&self, endpoint: Endpoint) -> Result<()> {
        let endpoint = endpoint.check()?;
        let mut endpoints = self.endpoints.write().expect("lock poisoned");
        if endpoints.contains_key(&endpoint.path) {
            return Err(ApiError::AlreadyRegistered);
        }
        endpoints.insert(endpoint.path.clone(), Arc::new(endpoint));
        Ok(())
    }

    /// Registers a raw handler at an exact path outside `/api/v1/`, like Go
    /// `api.RegisterHandler`. The handler is authorized with dynamic read
    /// permission and receives the resolved authentication token.
    pub fn register_handler(&self, path: &str, func: HandlerFunc) -> Result<()> {
        let mut handlers = self.handlers.write().expect("lock poisoned");
        if handlers.contains_key(path) {
            return Err(ApiError::AlreadyRegistered);
        }
        handlers.insert(path.to_string(), func);
        Ok(())
    }

    /// Returns a registered endpoint by path.
    pub fn endpoint(&self, path: &str) -> Option<Arc<Endpoint>> {
        self.endpoints
            .read()
            .expect("lock poisoned")
            .get(path)
            .cloned()
    }

    /// Returns all endpoints, sorted by path.
    pub fn export_endpoints(&self) -> Vec<Endpoint> {
        self.endpoints
            .read()
            .expect("lock poisoned")
            .values()
            .map(|endpoint| (**endpoint).clone())
            .collect()
    }

    /// Sets the authenticator used for requests without API key.
    pub fn set_authenticator(&self, authenticator: Arc<dyn Authenticator>) {
        *self.authenticator.write().expect("lock poisoned") = Some(authenticator);
    }

    /// Marks the core as ready (or not).
    pub fn set_ready(&self, ready: bool) {
        self.ready.store(ready, Ordering::SeqCst);
    }

    /// Returns whether the core finished starting.
    pub fn ready(&self) -> bool {
        self.ready.load(Ordering::SeqCst)
    }

    /// Provides the UI archives served by the UI routes.
    pub fn set_ui_archives(&self, archives: Arc<ui::UiArchives>) {
        *self.ui_archives.write().expect("lock poisoned") = Some(archives);
    }

    /// Returns the UI archives, if one was set.
    pub fn ui_archives(&self) -> Option<Arc<ui::UiArchives>> {
        self.ui_archives.read().expect("lock poisoned").clone()
    }

    /// Returns the session store.
    pub fn sessions(&self) -> &session::Sessions {
        &self.sessions
    }

    /// Provides the database system served by the WebSocket API.
    pub fn set_databases(&self, databases: Arc<pm_database::Databases>) {
        *self.databases.write().expect("lock poisoned") = Some(databases);
    }

    /// Returns the database system, if one was set.
    pub fn databases(&self) -> Option<Arc<pm_database::Databases>> {
        self.databases.read().expect("lock poisoned").clone()
    }

    /// Replaces the API keys with the given configuration values.
    pub fn update_api_keys(&self, keys: &[String]) {
        *self.api_keys.write().expect("lock poisoned") = parse_api_keys(keys);
    }

    /// Handles a request and returns the response.
    pub fn handle(&self, mut request: Request) -> Response {
        let cleaned = clean_request_path(&request.path);
        if cleaned != request.path {
            let mut location = cleaned;
            if !request.query_string.is_empty() {
                location.push('?');
                location.push_str(&request.query_string);
            }
            let response = Response::new(301).with_header("location", &location);
            return self.decorate(response, &request, None);
        }
        request.path = cleaned;

        // Raw handlers (such as /metrics) live outside /api/v1/.
        let raw_handler = self
            .handlers
            .read()
            .expect("lock poisoned")
            .get(&request.path)
            .cloned();
        if let Some(handler) = raw_handler {
            request.effective_method = "GET".to_string();
            request.read_method = true;
            if let Err(response) = self.authorize_required(&mut request, DYNAMIC) {
                return self.decorate(response, &request, None);
            }
            let response = handler(&mut request);
            return self.decorate(response, &request, None);
        }

        // Cross-origin checks.
        let mut cors_origin = None;
        if let Some(origin) = request.header("origin").map(str::to_string) {
            if !origin_allowed(&origin, &request.host, self.dev_mode()) {
                return self.decorate(
                    Response::error(403, "Cross-Origin Request Denied."),
                    &request,
                    None,
                );
            }
            cors_origin = Some(origin);
            if request.method == "OPTIONS"
                && request.header("access-control-request-method").is_some()
            {
                if self.match_endpoint(&request.path).is_none() {
                    return self.decorate(
                        Response::error(404, "Not found."),
                        &request,
                        cors_origin.as_deref(),
                    );
                }
                return self.decorate(Response::new(200), &request, cors_origin.as_deref());
            }
        }

        // Find the endpoint.
        let Some((endpoint, params)) = self.match_endpoint(&request.path) else {
            return self.decorate(
                Response::error(404, "Not found."),
                &request,
                cors_origin.as_deref(),
            );
        };
        request.endpoint_path = endpoint.path.clone();
        request.params = params;

        // Resolve the effective method.
        let Some((effective, read_method)) = effective_method(&request) else {
            return self.decorate(
                Response::error(405, "Method not allowed."),
                &request,
                cors_origin.as_deref(),
            );
        };
        request.effective_method = effective.to_string();
        request.read_method = read_method;

        // Authenticate and authorize.
        match self.authorize(&mut request, &endpoint) {
            Ok(token) => request.auth = Some(token),
            Err(response) => {
                return self.decorate(response, &request, cors_origin.as_deref());
            }
        }

        let response = self.execute(&mut request, &endpoint);
        self.decorate(response, &request, cors_origin.as_deref())
    }

    fn match_endpoint(&self, path: &str) -> Option<(Arc<Endpoint>, BTreeMap<String, String>)> {
        let rest = path.strip_prefix(API_V1_PATH)?;
        let segments: Vec<&str> = rest.split('/').collect();
        let endpoints = self.endpoints.read().expect("lock poisoned");
        for endpoint in endpoints.values() {
            if let Some(params) = endpoint.matches(&segments) {
                return Some((Arc::clone(endpoint), params.into_iter().collect()));
            }
        }
        None
    }

    // The error is the ready-made rejection response; boxing it would only add
    // an allocation on the success path of every request.
    #[allow(clippy::result_large_err)]
    fn authorize(
        &self,
        request: &mut Request,
        endpoint: &Endpoint,
    ) -> std::result::Result<AuthToken, Response> {
        self.authorize_required(request, endpoint.required_permission(request.read_method))
    }

    /// Checks a fixed permission requirement, like Go `WrapInAuthHandler`.
    #[allow(clippy::result_large_err)]
    pub fn authorize_required(
        &self,
        request: &mut Request,
        required: Permission,
    ) -> std::result::Result<AuthToken, Response> {
        let mut required = required;
        match required {
            NOT_FOUND => return Err(Response::error(404, "Not found.")),
            NOT_SUPPORTED => return Err(Response::error(405, "Method not allowed.")),
            PERMIT_ANYONE => return Ok(AuthToken::public()),
            DYNAMIC => required = PERMIT_ANYONE,
            permission if !(PERMIT_ANYONE..=PERMIT_SELF).contains(&permission) => {
                return Err(Response::error(
                    500,
                    "Internal server error during authentication.",
                ))
            }
            _ => {}
        }

        let mut token = if self.dev_mode() {
            Some(AuthToken::full())
        } else {
            self.check_api_key(request)
                .or_else(|| self.check_session_cookie(request))
        };

        if token.is_none() {
            let authenticator = self.authenticator.read().expect("lock poisoned").clone();
            if let Some(authenticator) = authenticator {
                match authenticator.authenticate(request) {
                    Ok(found) => {
                        if let Some(found) = &found {
                            self.create_session(request, found.clone());
                        }
                        token = found;
                    }
                    Err(AuthError::Denied(message)) => {
                        if required > PERMIT_ANYONE {
                            return Err(Response::error(403, &message));
                        }
                    }
                    Err(AuthError::Internal(message)) => {
                        tracing::error!(error = %message, "api: authenticator failed");
                        return Err(Response::error(
                            500,
                            "Internal server error during authentication.",
                        ));
                    }
                }
            }
        }

        let token = token.unwrap_or_else(AuthToken::public);
        let request_permission = if request.read_method {
            token.read
        } else {
            token.write
        };
        if !(PERMIT_ANYONE..=PERMIT_SELF).contains(&request_permission) {
            return Err(Response::error(
                500,
                "Internal server error during authentication.",
            ));
        }

        if request_permission < required {
            if !token.is_authenticated() {
                let mut response = Response::error(401, "Authorization required.");
                response.headers.insert(
                    http::header::WWW_AUTHENTICATE,
                    HeaderValue::from_static(AUTH_REQUIRED_HEADER),
                );
                return Err(response);
            }
            return Err(Response::error(403, "Insufficient permissions."));
        }

        Ok(token)
    }

    /// Adds the security headers of the Go router to a response.
    pub fn apply_security_headers(&self, headers: &mut HeaderMap) {
        let security: &[(&str, &str)] = &[
            ("referrer-policy", "same-origin"),
            ("x-content-type-options", "nosniff"),
            ("x-frame-options", "deny"),
            ("x-xss-protection", "1; mode=block"),
            ("x-dns-prefetch-control", "off"),
        ];
        for (name, value) in security {
            if !headers.contains_key(*name) {
                headers.insert(name_as_name(name), HeaderValue::from_static(value));
            }
        }
        if !self.dev_mode() {
            headers.insert(
                http::HeaderName::from_static("content-security-policy"),
                HeaderValue::from_static(
                    "default-src 'self'; connect-src https://*.safing.io 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:",
                ),
            );
        }
    }

    fn check_session_cookie(&self, request: &Request) -> Option<AuthToken> {
        let cookie = request.header("cookie")?;
        let value = session::cookie_value(cookie, SESSION_COOKIE_NAME)?;
        self.sessions.check(&value)
    }

    fn create_session(&self, request: &mut Request, token: AuthToken) {
        let Some(value) = self.sessions.create(token) else {
            return;
        };
        if let Ok(header) = HeaderValue::from_str(&session_cookie(&value)) {
            request
                .response_header
                .append(http::header::SET_COOKIE, header);
        }
    }

    fn check_api_key(&self, request: &Request) -> Option<AuthToken> {
        let key = bearer_token(&request.header)?;
        let token = self
            .api_keys
            .read()
            .expect("lock poisoned")
            .get(key.as_str())
            .cloned()?;
        // Defense in depth, matching Go's `checkAPIKey`: even if a stale token
        // is still registered, an expired key must never authorize.
        if token.expired() {
            return None;
        }
        Some(token)
    }

    fn execute(&self, request: &mut Request, endpoint: &Endpoint) -> Response {
        let Some(func) = &endpoint.func else {
            return Response::error(500, "missing handler");
        };

        let mime_with_charset = format!("{}; charset=utf-8", endpoint.mime_type);
        let response = match func {
            EndpointFunc::Action(func) => match func(request) {
                Ok(mut message) => {
                    if !message.ends_with('\n') {
                        message.push('\n');
                    }
                    Response::body(200, message.into_bytes(), &mime_with_charset)
                }
                Err(err) => error_response(err),
            },
            EndpointFunc::Data(func) => match func(request) {
                Ok(data) => Response::body(200, data, &mime_with_charset),
                Err(err) => error_response(err),
            },
            EndpointFunc::Struct(func) => match func(request) {
                Ok(value) => match mime_dump(&value, request.header("accept")) {
                    Ok((data, mime_type)) => Response::body(200, data, &mime_type),
                    Err(err) => error_response(err),
                },
                Err(err) => error_response(err),
            },
            EndpointFunc::Record(func) => match func(request) {
                Ok(record) => match record.to_json_with_meta(false) {
                    Ok(data) => Response::body(200, data, &mime_with_charset),
                    Err(err) => error_response(ApiError::Record(err)),
                },
                Err(err) => error_response(err),
            },
            EndpointFunc::Handler(func) => func(request),
        };

        if response.body.is_empty() || request.method == "HEAD" {
            let mut response = response;
            response.status = 204;
            response.body.clear();
            return response;
        }
        response
    }

    pub(crate) fn decorate(
        &self,
        mut response: Response,
        request: &Request,
        cors_origin: Option<&str>,
    ) -> Response {
        for (name, value) in request.response_header.iter() {
            response.headers.insert(name.clone(), value.clone());
        }

        self.apply_security_headers(&mut response.headers);

        if let Some(origin) = cors_origin {
            response.headers.insert(
                http::header::ACCESS_CONTROL_ALLOW_ORIGIN,
                HeaderValue::from_str(origin).unwrap_or(HeaderValue::from_static("*")),
            );
            response.headers.insert(
                http::header::ACCESS_CONTROL_ALLOW_METHODS,
                HeaderValue::from_static("*"),
            );
            response.headers.insert(
                http::header::ACCESS_CONTROL_ALLOW_HEADERS,
                HeaderValue::from_static("*"),
            );
            response.headers.insert(
                http::header::ACCESS_CONTROL_ALLOW_CREDENTIALS,
                HeaderValue::from_static("true"),
            );
            response.headers.insert(
                http::header::ACCESS_CONTROL_EXPOSE_HEADERS,
                HeaderValue::from_static("*"),
            );
            response.headers.insert(
                http::header::ACCESS_CONTROL_MAX_AGE,
                HeaderValue::from_static("60"),
            );
            // Go adds this on the same branch as the CORS headers.
            response
                .headers
                .append(http::header::VARY, HeaderValue::from_static("Origin"));
        }

        response
    }
}

fn name_as_name(name: &str) -> http::HeaderName {
    http::HeaderName::from_bytes(name.as_bytes()).expect("static header name")
}

fn error_response(err: ApiError) -> Response {
    Response::error(err.status(), &err.to_string())
}

/// Serializes a value according to the Accept header, like `dsd.MimeDump`.
fn mime_dump(value: &Value, accept: Option<&str>) -> Result<(Vec<u8>, String)> {
    let accept = accept.unwrap_or("").to_lowercase();
    if accept.contains("cbor") {
        let dumped = pm_dsd::dump(value, pm_dsd::format::CBOR)?;
        return Ok((dumped[1..].to_vec(), "application/cbor".to_string()));
    }
    if accept.contains("msgpack") {
        let dumped = pm_dsd::dump(value, pm_dsd::format::MSG_PACK)?;
        return Ok((dumped[1..].to_vec(), "application/msgpack".to_string()));
    }
    Ok((pm_dsd::gojson::to_vec(value)?, MIME_TYPE_JSON.to_string()))
}

/// Returns the effective method and whether it is a read method.
fn effective_method(request: &Request) -> Option<(&'static str, bool)> {
    let method = if request.method == "OPTIONS" {
        request.header("access-control-request-method")?
    } else {
        request.method.as_str()
    };
    match method {
        "GET" | "HEAD" => Some(("GET", true)),
        "POST" => Some(("POST", false)),
        "PUT" => Some(("PUT", false)),
        "DELETE" => Some(("DELETE", false)),
        _ => None,
    }
}

/// Checks whether a cross-origin request may access the API.
pub(crate) fn origin_allowed(origin: &str, host: &str, dev_mode: bool) -> bool {
    let parsed = origin.split_once("://");
    let Some((scheme, rest)) = parsed else {
        return false;
    };
    let origin_host = rest.trim_end_matches('/');
    let origin_hostname = origin_host.split(':').next().unwrap_or("");

    if scheme == "chrome-extension" {
        return true;
    }
    if origin_host == host || origin_hostname == host {
        return true;
    }
    if dev_mode && (origin_hostname == "127.0.0.1" || origin_hostname == "localhost") {
        return true;
    }
    false
}

/// Cleans a request path, like Go `cleanRequestPath`.
pub fn clean_request_path(path: &str) -> String {
    if path.is_empty() || path == "/" {
        return "/".to_string();
    }
    let path = if path.starts_with('/') {
        path.to_string()
    } else {
        format!("/{path}")
    };
    let trailing_slash = path.ends_with('/');

    let mut segments: Vec<&str> = Vec::new();
    for segment in path.split('/') {
        match segment {
            "" | "." => {}
            ".." => {
                segments.pop();
            }
            other => segments.push(other),
        }
    }
    let mut cleaned = format!("/{}", segments.join("/"));
    if trailing_slash && !cleaned.ends_with('/') {
        cleaned.push('/');
    }
    cleaned
}

/// Parses a query string.
pub fn parse_query(query: &str) -> BTreeMap<String, String> {
    let mut params = BTreeMap::new();
    for pair in query.split('&') {
        if pair.is_empty() {
            continue;
        }
        let (name, value) = pair.split_once('=').unwrap_or((pair, ""));
        params.insert(percent_decode(name), percent_decode(value));
    }
    params
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
                let hex = std::str::from_utf8(&bytes[index + 1..index + 3]).ok();
                match hex.and_then(|hex| u8::from_str_radix(hex, 16).ok()) {
                    Some(byte) => {
                        out.push(byte);
                        index += 3;
                    }
                    None => {
                        out.push(b'%');
                        index += 1;
                    }
                }
            }
            byte => {
                out.push(byte);
                index += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

/// Registers the config endpoints.
impl Api {
    pub fn register_config_endpoints(&self, config: Arc<pm_config::Config>) {
        let exported = config.clone();
        self.register_endpoint(
            public_endpoint(
                "config/options",
                "Export Configuration Options",
                "Returns a list of all registered configuration options and their metadata. This does not include the current active or default settings.",
            )
            .with_struct(move |_| Ok(serde_json::to_value(exported.export_options())?)),
        )
        .expect("config endpoint is valid");
    }

    fn register_meta_endpoints(self: &Arc<Self>) {
        let weak = Arc::downgrade(self);

        let registry = weak.clone();
        self.register_endpoint(
            public_endpoint(
                "endpoints",
                "Export API Endpoints",
                "Returns a list of all registered endpoints and their metadata.",
            )
            .with_data(move |_| {
                let api = registry
                    .upgrade()
                    .ok_or_else(|| ApiError::Message("api is shutting down".to_string()))?;
                Ok(pm_dsd::gojson::to_vec(&api.export_endpoints())?)
            }),
        )
        .expect("meta endpoint is valid");

        self.register_endpoint(
            public_endpoint("ping", "Ping", "Pong.").with_action(|_| Ok("Pong.".to_string())),
        )
        .expect("meta endpoint is valid");

        let ready_state = weak.clone();
        self.register_endpoint(
            public_endpoint(
                "ready",
                "Ready",
                "Check if Portmaster has completed starting and is ready.",
            )
            .with_action(move |_| {
                let ready = ready_state
                    .upgrade()
                    .map(|api| api.ready())
                    .unwrap_or(false);
                if ready {
                    Ok("Portmaster is ready.".to_string())
                } else {
                    Err(ApiError::WithStatus {
                        status: 425,
                        message: "portmaster is not ready, reload (F5) to try again".to_string(),
                    })
                }
            }),
        )
        .expect("meta endpoint is valid");

        self.register_endpoint(
            Endpoint::new(
                "auth/permissions",
                "View Current Permissions",
                "Returns the current permissions assigned to the request.",
            )
            .with_read(DYNAMIC)
            .with_struct(|request| {
                let token = request.auth.clone().unwrap_or_else(AuthToken::public);
                Ok(serde_json::json!({
                    "Read": token.read,
                    "Write": token.write,
                    "ReadRole": permission_role(token.read),
                    "WriteRole": permission_role(token.write),
                }))
            }),
        )
        .expect("meta endpoint is valid");

        self.register_endpoint(
            Endpoint::new(
                "auth/bearer",
                "Request HTTP Bearer Auth",
                "Returns an HTTP Bearer Auth request, if not authenticated.",
            )
            .with_read(DYNAMIC)
            .with_handler(|request| {
                if request
                    .auth
                    .as_ref()
                    .is_some_and(|token| token.read != PERMIT_ANYONE)
                {
                    return Response::body(
                        200,
                        b"Authenticated.\n".to_vec(),
                        "text/plain; charset=utf-8",
                    );
                }
                let mut response = Response::error(401, "Authorization required.");
                response.headers.insert(
                    http::header::WWW_AUTHENTICATE,
                    HeaderValue::from_static(AUTH_REQUIRED_HEADER),
                );
                response
            }),
        )
        .expect("meta endpoint is valid");

        self.register_endpoint(
            Endpoint::new(
                "auth/basic",
                "Request HTTP Basic Auth",
                "Returns an HTTP Basic Auth request, if not authenticated.",
            )
            .with_read(DYNAMIC)
            .with_handler(|request| {
                if request
                    .auth
                    .as_ref()
                    .is_some_and(|token| token.read != PERMIT_ANYONE)
                {
                    return Response::body(
                        200,
                        b"Authenticated.\n".to_vec(),
                        "text/plain; charset=utf-8",
                    );
                }
                let mut response = Response::error(401, "Authorization required.");
                response.headers.insert(
                    http::header::WWW_AUTHENTICATE,
                    HeaderValue::from_static("Basic realm=\"Portmaster API\" domain=\"/\""),
                );
                response
            }),
        )
        .expect("meta endpoint is valid");

        self.register_endpoint(
            Endpoint::new(
                "auth/reset",
                "Reset Authenticated Session",
                "Resets authentication status internally and in the browser.",
            )
            .with_read(PERMIT_ANYONE)
            .with_handler({
                let weak = weak.clone();
                move |request| {
                    if let Some(api) = weak.upgrade() {
                        if let Some(cookie) = request.header("cookie") {
                            if let Some(value) = session::cookie_value(cookie, SESSION_COOKIE_NAME)
                            {
                                api.sessions.delete(&value);
                            }
                        }
                    }
                    request.response_header.insert(
                        http::header::SET_COOKIE,
                        HeaderValue::from_static(
                            "Portmaster-API-Token=; Path=/; Max-Age=0; HttpOnly; SameSite=Strict",
                        ),
                    );
                    let mut response = Response::error(401, "Session deleted.");
                    response.headers.insert(
                        http::HeaderName::from_static("clear-site-data"),
                        HeaderValue::from_static("*"),
                    );
                    response.headers.insert(
                        http::header::WWW_AUTHENTICATE,
                        HeaderValue::from_static("None realm=\"Portmaster API\""),
                    );
                    response
                }
            }),
        )
        .expect("meta endpoint is valid");
    }
}

/// Serves the API with axum until the future is cancelled.
pub async fn serve(api: Arc<Api>, listener: tokio::net::TcpListener) -> std::io::Result<()> {
    let app = axum::Router::new()
        .route(
            database_ws::DB_API_PATH,
            axum::routing::get(database_ws::database_ws_upgrade),
        )
        .route("/assets/{*path}", axum::routing::get(ui::serve_assets))
        .route(
            "/ui/modules/{module}",
            axum::routing::get(ui::redirect_module),
        )
        .route(
            "/ui/modules/{module}/",
            axum::routing::get(ui::serve_module_index),
        )
        .route(
            "/ui/modules/{module}/{*path}",
            axum::routing::get(ui::serve_module_file),
        )
        .route("/", axum::routing::get(ui::redirect_root))
        .fallback(axum::routing::any(handle_axum))
        .with_state(api);
    // Connect info gives requests their remote address for authentication.
    axum::serve(
        listener,
        app.into_make_service_with_connect_info::<std::net::SocketAddr>(),
    )
    .await
}

async fn handle_axum(
    axum::extract::State(api): axum::extract::State<Arc<Api>>,
    request: axum::extract::Request,
) -> axum::response::Response {
    let (parts, body) = request.into_parts();
    let body = axum::body::to_bytes(body, 20_000_000)
        .await
        .unwrap_or_default();

    let mut api_request = Request::new(parts.method.as_str(), parts.uri.path());
    api_request.query_string = parts.uri.query().unwrap_or("").to_string();
    api_request.query = parse_query(&api_request.query_string);
    api_request.host = parts
        .headers
        .get(http::header::HOST)
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

    // Handlers and authenticators are synchronous and may block (process
    // lookups, retries), so they stay off the async workers.
    let response = match tokio::task::spawn_blocking(move || api.handle(api_request)).await {
        Ok(response) => response,
        Err(_) => Response::error(500, "Internal server error."),
    };
    let mut builder = http::Response::builder().status(
        http::StatusCode::from_u16(response.status)
            .unwrap_or(http::StatusCode::INTERNAL_SERVER_ERROR),
    );
    for (name, value) in response.headers.iter() {
        builder = builder.header(name, value);
    }
    builder
        .body(axum::body::Body::from(response.body))
        .unwrap_or_else(|_| {
            http::Response::builder()
                .status(http::StatusCode::INTERNAL_SERVER_ERROR)
                .body(axum::body::Body::empty())
                .expect("static response")
        })
}
