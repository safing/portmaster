//! The log-line data model, ported from Go `base/log/logging.go`.
//!
//! Go keeps a pointer to the `ContextTracer` inside each line. Here the trace
//! actions are snapshotted into a `Vec<LogLine>` so lines own their data and
//! can be formatted without borrowing a live tracer.

use crate::format::Timestamp;
use crate::severity::Severity;

/// Structured view of a log record, like Go's `Message` interface.
pub trait Message {
    /// Message text.
    fn text(&self) -> &str;
    /// Severity of the record.
    fn severity(&self) -> Severity;
    /// Timestamp of the record.
    fn time(&self) -> Timestamp;
    /// Source file (without the `.go`/`.rs` suffix).
    fn file(&self) -> &str;
    /// Source line number, or 0 if unknown.
    fn line_number(&self) -> u32;
}

/// A single log record.
#[derive(Clone, Debug)]
pub struct LogLine {
    /// Message text.
    pub msg: String,
    /// Severity level.
    pub level: Severity,
    /// Times at which the line was created.
    pub timestamp: Timestamp,
    /// Source file with the language suffix already removed, like Go.
    pub file: String,
    /// Source line number, or 0 if unknown.
    pub line: u32,
    /// Trace actions attached by `ContextTracer::submit`, if any.
    pub tracer: Option<Vec<LogLine>>,
}

impl Message for LogLine {
    fn text(&self) -> &str {
        &self.msg
    }

    fn severity(&self) -> Severity {
        self.level
    }

    fn time(&self) -> Timestamp {
        self.timestamp
    }

    fn file(&self) -> &str {
        &self.file
    }

    fn line_number(&self) -> u32 {
        self.line
    }
}

impl LogLine {
    /// Returns true when two lines are duplicates, matching Go
    /// `logLine.Equal`: same message, tracer presence, file, line and level.
    pub fn equal(&self, other: &LogLine) -> bool {
        self.msg == other.msg
            && self.tracer.is_none()
            && other.tracer.is_none()
            && self.file == other.file
            && self.line == other.line
            && self.level == other.level
    }
}

/// Strips the three-character language suffix from a source path, matching Go
/// `file[:len(file)-3]` (which removes `.go`).
pub(crate) fn strip_source_suffix(file: &str) -> String {
    if file.len() > 3 {
        file[..file.len() - 3].to_owned()
    } else {
        String::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn duplicate_detection_matches_go() {
        let base = LogLine {
            msg: "same".into(),
            level: Severity::Info,
            timestamp: Timestamp(1),
            file: "a.go".into(),
            line: 5,
            tracer: None,
        };
        let mut copy = base.clone();
        copy.timestamp = Timestamp(999);
        assert!(base.equal(&copy));
        copy.msg = "different".into();
        assert!(!base.equal(&copy));

        // Go never treats lines with tracers as duplicates.
        let mut with_tracer = base.clone();
        with_tracer.tracer = Some(Vec::new());
        assert!(!base.equal(&with_tracer));
        assert!(!with_tracer.equal(&with_tracer.clone()));
    }

    #[test]
    fn source_suffix_is_stripped() {
        assert_eq!(strip_source_suffix("/src/main.go"), "/src/main");
        assert_eq!(strip_source_suffix("ab"), "");
    }
}
