/// Errors of the DSD and varint codecs.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("varint: buf too small")]
    BufTooSmall,
    #[error("varint: {0}")]
    Varint(&'static str),
    #[error("dsd: not enough data")]
    UnexpectedEof,
    #[error("dsd: given data is in raw format")]
    IsRaw,
    #[error("dsd: format is incompatible with operation")]
    IncompatibleFormat,
    #[error("dsd: format {0} is not supported")]
    Unsupported(&'static str),
    #[error("dsd: not a dsd list: {0}")]
    BadList(&'static str),
    #[error("dsd: json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("dsd: cbor: {0}")]
    Cbor(String),
    #[error("dsd: msgpack: {0}")]
    MsgPack(String),
    #[error("dsd: gzip: {0}")]
    Gzip(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, Error>;
