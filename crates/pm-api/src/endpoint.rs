//! API endpoints and their registry.

use std::sync::Arc;

use serde::Serialize;

use crate::permission::{Permission, DYNAMIC, NOT_SUPPORTED, PERMIT_ANYONE, PERMIT_SELF};
use crate::{ApiError, Request, Response, Result};

/// MIME type constants.
pub const MIME_TYPE_JSON: &str = "application/json";
pub const MIME_TYPE_TEXT: &str = "text/plain";

pub type ActionFunc = Arc<dyn Fn(&mut Request) -> Result<String> + Send + Sync>;
pub type DataFunc = Arc<dyn Fn(&mut Request) -> Result<Vec<u8>> + Send + Sync>;
pub type StructFunc = Arc<dyn Fn(&mut Request) -> Result<serde_json::Value> + Send + Sync>;
pub type RecordFunc = Arc<dyn Fn(&mut Request) -> Result<pm_record::Record> + Send + Sync>;
pub type HandlerFunc = Arc<dyn Fn(&mut Request) -> Response + Send + Sync>;

/// The handler of an endpoint; exactly one is set.
#[derive(Clone)]
pub enum EndpointFunc {
    /// Simple action with a message for the user.
    Action(ActionFunc),
    /// Raw data for further processing.
    Data(DataFunc),
    /// Any serializable value.
    Struct(StructFunc),
    /// A database record, marshalled including metadata.
    Record(RecordFunc),
    /// Fully custom response.
    Handler(HandlerFunc),
}

impl EndpointFunc {
    fn default_mime_type(&self) -> &'static str {
        match self {
            EndpointFunc::Action(_) | EndpointFunc::Data(_) | EndpointFunc::Handler(_) => {
                MIME_TYPE_TEXT
            }
            EndpointFunc::Struct(_) | EndpointFunc::Record(_) => MIME_TYPE_JSON,
        }
    }
}

/// Describes a parameterized variation of an endpoint.
#[derive(Debug, Clone, Serialize)]
pub struct Parameter {
    #[serde(rename = "Method")]
    pub method: String,
    #[serde(rename = "Field")]
    pub field: String,
    #[serde(rename = "Value")]
    pub value: String,
    #[serde(rename = "Description")]
    pub description: String,
}

/// An API endpoint.
///
/// Field order and JSON names match the Go `api.Endpoint` struct.
#[derive(Clone, Serialize)]
pub struct Endpoint {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Description")]
    pub description: String,
    #[serde(rename = "Parameters", default, skip_serializing_if = "Vec::is_empty")]
    pub parameters: Vec<Parameter>,
    #[serde(rename = "Path")]
    pub path: String,
    #[serde(rename = "MimeType")]
    pub mime_type: String,
    #[serde(rename = "Read", skip_serializing_if = "is_not_supported")]
    pub read: Permission,
    #[serde(
        rename = "ReadMethod",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub read_method: String,
    #[serde(rename = "Write", skip_serializing_if = "is_not_supported")]
    pub write: Permission,
    #[serde(
        rename = "WriteMethod",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub write_method: String,
    #[serde(skip)]
    pub func: Option<EndpointFunc>,
}

fn is_not_supported(permission: &Permission) -> bool {
    *permission == NOT_SUPPORTED
}

impl std::fmt::Debug for Endpoint {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Endpoint")
            .field("path", &self.path)
            .field("read", &self.read)
            .field("write", &self.write)
            .finish_non_exhaustive()
    }
}

impl Endpoint {
    /// Creates an endpoint with the given path and description.
    pub fn new(path: &str, name: &str, description: &str) -> Endpoint {
        Endpoint {
            name: name.to_string(),
            description: description.to_string(),
            parameters: Vec::new(),
            path: path.to_string(),
            mime_type: String::new(),
            read: NOT_SUPPORTED,
            read_method: String::new(),
            write: NOT_SUPPORTED,
            write_method: String::new(),
            func: None,
        }
    }

    #[must_use]
    pub fn with_read(mut self, permission: Permission) -> Endpoint {
        self.read = permission;
        self
    }

    #[must_use]
    pub fn with_write(mut self, permission: Permission) -> Endpoint {
        self.write = permission;
        self
    }

    #[must_use]
    pub fn with_mime_type(mut self, mime_type: &str) -> Endpoint {
        self.mime_type = mime_type.to_string();
        self
    }

    #[must_use]
    pub fn with_read_method(mut self, method: &str) -> Endpoint {
        self.read_method = method.to_string();
        self
    }

    #[must_use]
    pub fn with_write_method(mut self, method: &str) -> Endpoint {
        self.write_method = method.to_string();
        self
    }

    #[must_use]
    pub fn with_parameter(mut self, parameter: Parameter) -> Endpoint {
        self.parameters.push(parameter);
        self
    }

    #[must_use]
    pub fn with_action(
        mut self,
        func: impl Fn(&mut Request) -> Result<String> + Send + Sync + 'static,
    ) -> Endpoint {
        self.func = Some(EndpointFunc::Action(Arc::new(func)));
        self
    }

    #[must_use]
    pub fn with_data(
        mut self,
        func: impl Fn(&mut Request) -> Result<Vec<u8>> + Send + Sync + 'static,
    ) -> Endpoint {
        self.func = Some(EndpointFunc::Data(Arc::new(func)));
        self
    }

    #[must_use]
    pub fn with_struct(
        mut self,
        func: impl Fn(&mut Request) -> Result<serde_json::Value> + Send + Sync + 'static,
    ) -> Endpoint {
        self.func = Some(EndpointFunc::Struct(Arc::new(func)));
        self
    }

    #[must_use]
    pub fn with_record(
        mut self,
        func: impl Fn(&mut Request) -> Result<pm_record::Record> + Send + Sync + 'static,
    ) -> Endpoint {
        self.func = Some(EndpointFunc::Record(Arc::new(func)));
        self
    }

    #[must_use]
    pub fn with_handler(
        mut self,
        func: impl Fn(&mut Request) -> Response + Send + Sync + 'static,
    ) -> Endpoint {
        self.func = Some(EndpointFunc::Handler(Arc::new(func)));
        self
    }

    /// Validates the endpoint and fills in defaults, like Go `Endpoint.check`.
    pub(crate) fn check(mut self) -> Result<Endpoint> {
        if self.path.trim().is_empty() {
            return Err(ApiError::InvalidEndpoint("path is missing".to_string()));
        }

        if !(DYNAMIC..=PERMIT_SELF).contains(&self.read) {
            return Err(ApiError::InvalidEndpoint(
                "invalid read permission".to_string(),
            ));
        }
        if !(DYNAMIC..=PERMIT_SELF).contains(&self.write) {
            return Err(ApiError::InvalidEndpoint(
                "invalid write permission".to_string(),
            ));
        }

        if self.read != NOT_SUPPORTED {
            match self.read_method.as_str() {
                "GET" => {}
                "" => self.read_method = "GET".to_string(),
                _ => return Err(ApiError::InvalidEndpoint("invalid read method".to_string())),
            }
        } else {
            self.read_method.clear();
        }
        if self.write != NOT_SUPPORTED {
            match self.write_method.as_str() {
                "POST" | "PUT" | "DELETE" => {}
                "" => self.write_method = "POST".to_string(),
                _ => {
                    return Err(ApiError::InvalidEndpoint(
                        "invalid write method".to_string(),
                    ))
                }
            }
        } else {
            self.write_method.clear();
        }

        let Some(func) = &self.func else {
            return Err(ApiError::InvalidEndpoint(
                "only one function may be set".to_string(),
            ));
        };
        if self.mime_type.is_empty() {
            self.mime_type = func.default_mime_type().to_string();
        }

        Ok(self)
    }

    /// Returns the required permission for the request method.
    pub(crate) fn required_permission(&self, read_method: bool) -> Permission {
        if read_method {
            self.read
        } else {
            self.write
        }
    }

    /// Checks whether a path matches the endpoint pattern and returns the
    /// captured variables. `{name}` captures one path segment.
    pub(crate) fn matches(&self, segments: &[&str]) -> Option<Vec<(String, String)>> {
        let pattern: Vec<&str> = self.path.split('/').collect();
        if pattern.len() != segments.len() {
            return None;
        }

        let mut params = Vec::new();
        for (part, segment) in pattern.iter().zip(segments) {
            if let Some(name) = part.strip_prefix('{').and_then(|p| p.strip_suffix('}')) {
                params.push((name.to_string(), (*segment).to_string()));
            } else if part != segment {
                return None;
            }
        }
        Some(params)
    }
}

/// Creates an endpoint that is open to anyone.
pub fn public_endpoint(path: &str, name: &str, description: &str) -> Endpoint {
    Endpoint::new(path, name, description).with_read(PERMIT_ANYONE)
}
