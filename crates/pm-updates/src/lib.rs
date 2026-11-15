//! Compatible implementation of the Go `service/updates` index model.
//!
//! The crate covers update indexes and artifacts: parsing (with an injectable
//! signature verifier), platform filtering, filename validation, version
//! handling, upgrade decisions and SHA-256 verification.
//!
//! # Compatibility notes
//!
//! * Signature verification uses `jess` in Go. Until it is ported, callers pass
//!   an [`IndexVerifier`] callback to [`Index::parse`]; without one the index is
//!   accepted unsigned, like Go with a nil trust store.
//! * The downloader, the updater module (workers, events, upgrade application)
//!   and local index generation (`index_scan.go`) are not ported yet.
//! * Versions follow `hashicorp/go-version`: a leading `v` is accepted, missing
//!   segments are padded to three and build metadata is ignored when comparing.
//! * The "is from the future" error message formats the remaining time with the
//!   same Go duration layout, but the exact seconds depend on the clock.

mod downloader;
mod index;
mod version;

pub use downloader::{
    copy_and_check_sha256_sum, decompress, default_user_agent, Downloader, HttpClient,
    HttpsClient,
};
pub use index::{
    check_sha256_sum, check_sha256_sum_file, go_platform, validate_artifact_filename, Artifact,
    Index, IndexVerifier, MAX_UNPACK_SIZE,
};
pub use version::{Version, VersionError};

/// Errors of the update system.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("read index file: {0}")]
    ReadIndex(#[source] std::io::Error),
    #[error("verify: {0}")]
    Verify(String),
    #[error("parse index: {0}")]
    Parse(String),
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("invalid artifact filename {0:?}")]
    InvalidFilename(String),
    #[error("invalid artifact filename {0:?}: non-canonical path")]
    NonCanonicalFilename(String),
    #[error("invalid artifact filename {0:?}: invalid path")]
    InvalidPathName(String),
    #[error("invalid artifact filename {0:?}: contains path components")]
    FilenameComponents(String),
    #[error("invalid artifact filename {0:?}: contains ':'")]
    FilenameColon(String),
    #[error("invalid index version {0:?}: {1}")]
    InvalidVersion(String, String),
    #[error("missing version number")]
    MissingVersion,
    #[error("missing publish date")]
    MissingPublishDate,
    #[error("is from the future ({0})")]
    FromTheFuture(String),
    #[error("current index cannot do upgrades: {0}")]
    CurrentCannotUpgrade(String),
    #[error("new index cannot do upgrade: {0}")]
    NewCannotUpgrade(String),
    #[error("new index name ({new:?}) does not match current index name ({current:?})")]
    NameMismatch { current: String, new: String },
    #[error("new index has newer version but older publish date")]
    NewerVersionOlderDate,
    #[error("new index has older version but newer publish date")]
    OlderVersionNewerDate,
    #[error("new index is a breaking change downgrade")]
    BreakingDowngrade,
    #[error("same index")]
    SameIndex,
    #[error("verify {0}: {1}")]
    ArtifactVerification(String, String),
    #[error("invalid hex encoding for expected hash {0}")]
    InvalidHashHex(String),
    #[error("invalid size for expected hash {0}")]
    InvalidHashSize(String),
    #[error("sha256sum mismatch")]
    ChecksumMismatch,
    #[error("open file: {0}")]
    OpenFile(#[source] std::io::Error),
    #[error("{0}")]
    Process(String),
}

impl Error {
    /// Returns whether the error is [`Error::SameIndex`].
    pub fn is_same_index(&self) -> bool {
        matches!(self, Error::SameIndex)
    }
}

/// Result type of the update system.
pub type Result<T> = std::result::Result<T, Error>;

/// Deserializes `null` as the default value, like Go decoding `nil` into a
/// slice.
pub(crate) fn deserialize_null_default<'de, D, T>(
    deserializer: D,
) -> std::result::Result<T, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Default + serde::Deserialize<'de>,
{
    use serde::Deserialize;
    Ok(Option::<T>::deserialize(deserializer)
        .map_err(serde::de::Error::custom)?
        .unwrap_or_default())
}
