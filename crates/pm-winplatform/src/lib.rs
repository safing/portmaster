//! Windows-only platform integrations for the Portmaster core.
//!
//! This crate collects the parts of the Go core that talk to Windows APIs and
//! would otherwise keep the rest of the Rust port platform-specific:
//!
//! * [`iphelper`] — `iphlpapi.dll` TCP/UDP state tables (the Windows analogue
//!   of `pm-netstate`'s `/proc` reader on Linux).
//! * [`osdetail`] — service control, PowerShell/cmd helpers, DNS cache toggles,
//!   Windows version checks, `svchost.exe` service discovery and console color
//!   detection.
//! * [`integration`] — loading `portmaster-core.dll` and its ETW entry points.
//! * [`binmeta`] — deriving human readable binary names and profile icons.
//!
//! # Platform support
//!
//! The pure parsers and formatters are compiled on every platform and are
//! covered by unit tests. Real FFI is confined to `#[cfg(windows)]` modules.
//! On non-Windows targets the Windows entry points are present as no-op stubs
//! that return [`Error::UnsupportedPlatform`], so dependent crates keep
//! compiling and the Linux build stays entirely `unsafe`-free.
//!
//! # Unsafe policy
//!
//! The workspace forbids `unsafe` globally. This crate does not inherit the
//! workspace lints (no `[lints] workspace = true`) and sets
//! `unsafe_code = "deny"` itself; only the small `extern` blocks under
//! `#[cfg(windows)]` carry a scoped `#[allow(unsafe_code)]`.

pub mod binmeta;
pub mod integration;
pub mod iphelper;
pub mod osdetail;

use thiserror::Error;

/// Errors shared by the platform integrations.
#[derive(Debug, Error)]
pub enum Error {
    /// The operation is only available on Windows.
    #[error("operation is only supported on Windows")]
    UnsupportedPlatform,
}

/// Result type of the platform integrations.
pub type Result<T> = std::result::Result<T, Error>;
