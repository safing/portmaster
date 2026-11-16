//! Port of the Go `base/log` package.
//!
//! The crate provides the pieces of that package that the Rust core needs:
//! severity levels with Go's exact numbers and names, the Go line format and
//! timestamps, stdout/file writers with log-file cleanup and optional size
//! rotation, context tracers, an unexpected-log ring, a Go-`slog`-shaped
//! adapter and a `tracing` layer so existing `tracing` calls flow through the
//! same writer.
//!
//! ```
//! use pm_log::{Severity, StartConfig};
//!
//! let config = StartConfig {
//!     level: Severity::Info,
//!     to_stdout: true,
//!     ..StartConfig::default()
//! };
//! pm_log::start(config).unwrap();
//! pm_log::info("hello from pm-log");
//! ```
//!
//! Note: Go formats timestamps in local time; this port uses UTC (see the
//! per-module docs and the porting report).

pub mod format;
pub mod logger;
pub mod message;
pub mod severity;
pub mod slog_adapter;
pub mod trace;
pub mod tracing_layer;
pub mod writer;

pub use format::{format_duplicates, format_line, go_duration, LineCounter, Timestamp};
pub use logger::{
    critical, debug, error, get_last_unexpected_logs, get_log_level, global_logger, info,
    line, log, set_log_level, shutdown, start, total_critical_log_lines, total_error_log_lines,
    total_warning_log_lines, trace, warning, Logger, StartConfig,
};
pub use message::{LogLine, Message};
pub use severity::Severity;
pub use slog_adapter::{from_slog_level, SlogHandler, SlogRecord};
pub use trace::{ContextTracer, TraceContext};
pub use tracing_layer::{install as install_tracing, PmLogLayer};
pub use writer::{clean_old_logs, LogWriter, Rotation, Sink};

/// Arrow used in formatted lines, matching Go's Unix build (`▶`).
pub const RIGHT_ARROW: &str = "▶";
/// Arrow used in EOF banners, matching Go's Unix build (`◀`).
pub const LEFT_ARROW: &str = "◀";

/// Formats and logs a trace message.
#[macro_export]
macro_rules! tracef {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Trace, format!($($arg)*)) };
}

/// Formats and logs a debug message.
#[macro_export]
macro_rules! debugf {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Debug, format!($($arg)*)) };
}

/// Formats and logs an info message.
#[macro_export]
macro_rules! infof {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Info, format!($($arg)*)) };
}

/// Formats and logs a warning message.
#[macro_export]
macro_rules! warningf {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Warning, format!($($arg)*)) };
}

/// Formats and logs an error message.
#[macro_export]
macro_rules! errorf {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Error, format!($($arg)*)) };
}

/// Formats and logs a critical message.
#[macro_export]
macro_rules! criticalf {
    ($($arg:tt)*) => { $crate::log($crate::Severity::Critical, format!($($arg)*)) };
}
