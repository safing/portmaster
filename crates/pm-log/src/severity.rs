//! Severity levels, ported byte-for-byte from Go `base/log/logging.go` and
//! `base/log/formatting.go`.

use std::fmt;
use std::str::FromStr;

/// Log level severity. The numeric values are part of the public contract and
/// match the Go `Severity` constants exactly.
#[repr(u32)]
#[derive(Copy, Clone, Debug, Default, Eq, PartialEq, Ord, PartialOrd, Hash)]
pub enum Severity {
    /// Invalid / unknown level. Go uses `Severity(0)` for this.
    #[default]
    None = 0,
    /// Tiny steps, see Go `TraceLevel`.
    Trace = 1,
    /// Minor errors or unexpected events, see Go `DebugLevel`.
    Debug = 2,
    /// Mildly significant events, see Go `InfoLevel`.
    Info = 3,
    /// Bad events where nothing broke, see Go `WarningLevel`.
    Warning = 4,
    /// Errors that impair functionality, see Go `ErrorLevel`.
    Error = 5,
    /// Events that completely break the system, see Go `CriticalLevel`.
    Critical = 6,
}

impl Severity {
    /// Returns the lowercase name, like Go `Severity.Name()`.
    pub fn name(self) -> &'static str {
        match self {
            Severity::Trace => "trace",
            Severity::Debug => "debug",
            Severity::Info => "info",
            Severity::Warning => "warning",
            Severity::Error => "error",
            Severity::Critical => "critical",
            Severity::None => "none",
        }
    }

    /// Returns the three-letter abbreviation, like Go `Severity.String()`.
    pub fn short(self) -> &'static str {
        match self {
            Severity::Trace => "TRC",
            Severity::Debug => "DBG",
            Severity::Info => "INF",
            Severity::Warning => "WRN",
            Severity::Error => "ERR",
            Severity::Critical => "CRT",
            Severity::None => "NON",
        }
    }

    /// Parses a level name case-insensitively, like Go `ParseLevel`.
    /// Unknown names yield [`Severity::None`] (Go returns `0`).
    pub fn parse_level(level: &str) -> Severity {
        match level.to_ascii_lowercase().as_str() {
            "trace" => Severity::Trace,
            "debug" => Severity::Debug,
            "info" => Severity::Info,
            "warning" => Severity::Warning,
            "error" => Severity::Error,
            "critical" => Severity::Critical,
            _ => Severity::None,
        }
    }

    /// ANSI color prefix for the level, matching Go `Severity.color()`.
    pub fn color(self) -> &'static str {
        match self {
            Severity::Debug => "\x1b[36m",
            Severity::Info => "\x1b[92m",
            Severity::Warning => "\x1b[93m",
            Severity::Error => "\x1b[91m",
            Severity::Critical => "\x1b[35m",
            Severity::Trace | Severity::None => "",
        }
    }

    /// Maps the severity to a `tracing` level, used by the tracing adapter.
    pub fn to_tracing_level(self) -> tracing::Level {
        match self {
            Severity::Trace => tracing::Level::TRACE,
            Severity::Debug => tracing::Level::DEBUG,
            Severity::Info => tracing::Level::INFO,
            Severity::Warning => tracing::Level::WARN,
            Severity::Error | Severity::Critical | Severity::None => tracing::Level::ERROR,
        }
    }

    /// Maps a `tracing` level to a severity. Go has no `critical` in tracing,
    /// so error stays error unless a caller explicitly uses [`Severity::Critical`].
    pub fn from_tracing_level(level: tracing::Level) -> Severity {
        match level {
            tracing::Level::TRACE => Severity::Trace,
            tracing::Level::DEBUG => Severity::Debug,
            tracing::Level::INFO => Severity::Info,
            tracing::Level::WARN => Severity::Warning,
            tracing::Level::ERROR => Severity::Error,
        }
    }

    /// Maps the severity to a slog level number (Go `log/slog` values).
    pub fn to_slog_level(self) -> i32 {
        match self {
            Severity::Trace | Severity::Debug => -4,
            Severity::Info => 0,
            Severity::Warning => 4,
            Severity::Error | Severity::Critical => 8,
            Severity::None => 4,
        }
    }
}

impl FromStr for Severity {
    type Err = ();

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let level = Severity::parse_level(s);
        if level == Severity::None {
            Err(())
        } else {
            Ok(level)
        }
    }
}

impl fmt::Display for Severity {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.short())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn severity_numbers_and_names_match_go() {
        assert_eq!(Severity::Trace as u32, 1);
        assert_eq!(Severity::Debug as u32, 2);
        assert_eq!(Severity::Info as u32, 3);
        assert_eq!(Severity::Warning as u32, 4);
        assert_eq!(Severity::Error as u32, 5);
        assert_eq!(Severity::Critical as u32, 6);
        assert_eq!(Severity::None.name(), "none");
        assert_eq!(Severity::Warning.name(), "warning");
    }

    #[test]
    fn short_names_match_go() {
        assert_eq!(Severity::Trace.short(), "TRC");
        assert_eq!(Severity::Debug.short(), "DBG");
        assert_eq!(Severity::Info.short(), "INF");
        assert_eq!(Severity::Warning.short(), "WRN");
        assert_eq!(Severity::Error.short(), "ERR");
        assert_eq!(Severity::Critical.short(), "CRT");
        assert_eq!(Severity::None.short(), "NON");
    }

    #[test]
    fn parsing_is_case_insensitive() {
        assert_eq!(Severity::parse_level("WaRnInG"), Severity::Warning);
        assert_eq!(Severity::parse_level("critical"), Severity::Critical);
        assert_eq!(Severity::parse_level("nonsense"), Severity::None);
        assert!("trace".parse::<Severity>().is_ok());
        assert!("nonsense".parse::<Severity>().is_err());
    }

    #[test]
    fn slog_levels_match_go() {
        assert_eq!(Severity::Trace.to_slog_level(), -4);
        assert_eq!(Severity::Info.to_slog_level(), 0);
        assert_eq!(Severity::Warning.to_slog_level(), 4);
        assert_eq!(Severity::Error.to_slog_level(), 8);
    }
}
