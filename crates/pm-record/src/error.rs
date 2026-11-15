/// Errors of the record layer.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("insufficient data: got {got} out of {want} bytes")]
    InsufficientData { got: usize, want: usize },
    #[error("incompatible record version: {0}")]
    IncompatibleVersion(u8),
    #[error("could not get meta section: {0}")]
    MetaSection(pm_dsd::Error),
    #[error("could not unmarshal meta section: {0}")]
    MetaUnmarshal(pm_dsd::Error),
    #[error("could not get dsd format: {0}")]
    Format(pm_dsd::Error),
    #[error("could not dump model, wrapped object format mismatch")]
    FormatMismatch,
    #[error("record payload is not a JSON object")]
    NotJsonObject,
    #[error("tried to set field {key} ({existing}) to a {got} value")]
    TypeMismatch {
        key: String,
        existing: &'static str,
        got: &'static str,
    },
    #[error("invalid path: {0}")]
    InvalidPath(String),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
}

pub type Result<T> = std::result::Result<T, Error>;
