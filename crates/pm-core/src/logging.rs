//! Logging setup.
//!
//! The Rust core keeps using `tracing` at call sites, but `pm-log` now owns the
//! writer, rotation, level filtering and the unexpected-log ring. A `tracing`
//! layer forwards every existing `tracing` event into pm-log.

use pm_log::{Severity, StartConfig};

use crate::cli::Options;

/// Initializes logging, either to stdout or to a rotating log file, and routes
/// existing `tracing` events into it.
pub fn init(options: &Options) {
    let level = severity_from_str(&options.log_level);
    let config = StartConfig {
        level,
        to_stdout: options.log_to_stdout,
        log_dir: options.log_dir.clone(),
        version: crate::cli::version(),
    };

    let fallback = StartConfig {
        to_stdout: true,
        ..config.clone()
    };
    if let Err(error) = pm_log::start(config) {
        eprintln!("failed to initialize log file: {error}");
        let _ = pm_log::start(fallback);
    }

    pm_log::install_tracing();
}

/// Parses a CLI level into a pm-log severity, warning on invalid input like Go
/// `log.Start`.
fn severity_from_str(level: &str) -> Severity {
    let parsed = Severity::parse_level(level);
    if parsed == Severity::None {
        eprintln!(
            "log warning: invalid log level {level:?}, falling back to level info"
        );
        Severity::Info
    } else {
        parsed
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn levels_map_to_tracing_levels() {
        assert_eq!(Severity::Trace.to_tracing_level(), tracing::Level::TRACE);
        assert_eq!(Severity::Warning.to_tracing_level(), tracing::Level::WARN);
        assert_eq!(Severity::Critical.to_tracing_level(), tracing::Level::ERROR);
        assert_eq!(Severity::Info.to_tracing_level(), tracing::Level::INFO);
    }

    #[test]
    fn levels_map_to_pm_log_severities() {
        assert_eq!(severity_from_str("trace"), Severity::Trace);
        assert_eq!(severity_from_str("debug"), Severity::Debug);
        assert_eq!(severity_from_str("warning"), Severity::Warning);
        assert_eq!(severity_from_str("critical"), Severity::Critical);
        assert_eq!(severity_from_str("nonsense"), Severity::Info);
    }
}
