//! Pure helper utilities ported from the Go `base/utils` package.
//!
//! Every module is a faithful Rust re-implementation of the corresponding Go
//! source, including its observable quirks (for example, Go's
//! `RemoveFromStringSlice` does not remove an element at index 0). Where the
//! standard library already provides an equivalent (`Vec`, `Option`, …) the
//! API is adapted to idiomatic Rust, but the behaviour is kept identical.
//!
//! Not ported on purpose:
//! * `base/utils/safe.go` — already lives in `pm-apprise::teaser`.
//! * `base/utils/osdetail/*_windows.go` — Windows-only OS integration.
//! * `base/utils/debug/*` — build-tag driven debug helpers.

pub mod atomic;
pub mod broadcast_flag;
pub mod call_limiter;
pub mod fs;
pub mod mimetypes;
pub mod once_again;
pub mod renameio;
pub mod slices;
pub mod stable_pool;
pub mod structure;
pub mod uuid;
