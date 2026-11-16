//! Go `slog.Handler`-shaped adapter. Go wires `slog` to `log.GlobalWriter`
//! through a `tint` handler (`base/log/slog.go`); this layer folds a structured
//! record into the same Go-style pm-log line, with attributes rendered as
//! `key=value`, and writes it through the normal writer/rotation path.

use std::sync::Arc;

use crate::logger::{global_logger, line, Logger};
use crate::message::LogLine;
use crate::severity::Severity;

/// A structured log record, equivalent to a Go `slog.Record`.
#[derive(Clone, Debug)]
pub struct SlogRecord {
    /// Severity of the record.
    pub level: Severity,
    /// The log message.
    pub message: String,
    /// Source file reported by the handler.
    pub file: String,
    /// Source line reported by the handler.
    pub line: u32,
    /// Structured attributes in insertion order.
    pub attrs: Vec<(String, String)>,
}

impl SlogRecord {
    /// Creates a record without attributes.
    pub fn new(level: Severity, message: impl Into<String>, file: &str, line: u32) -> SlogRecord {
        SlogRecord {
            level,
            message: message.into(),
            file: file.to_owned(),
            line,
            attrs: Vec::new(),
        }
    }

    /// Adds an attribute.
    pub fn attr(mut self, key: impl Into<String>, value: impl Into<String>) -> SlogRecord {
        self.attrs.push((key.into(), value.into()));
        self
    }

    /// Renders the message with attributes appended as `key=value`, matching
    /// the text form used by Go's slog text/tint handlers.
    pub fn formatted_message(&self) -> String {
        let mut text = self.message.clone();
        for (key, value) in &self.attrs {
            text.push(' ');
            text.push_str(key);
            text.push('=');
            text.push_str(value);
        }
        text
    }
}

/// Handler that turns structured records into pm-log lines.
pub struct SlogHandler {
    logger: Arc<Logger>,
}

impl Default for SlogHandler {
    fn default() -> SlogHandler {
        SlogHandler::new()
    }
}

impl SlogHandler {
    /// Creates a handler attached to the process-wide logger.
    pub fn new() -> SlogHandler {
        SlogHandler {
            logger: global_logger(),
        }
    }

    /// Creates a handler attached to a specific logger.
    pub fn with_logger(logger: Arc<Logger>) -> SlogHandler {
        SlogHandler { logger }
    }

    /// Whether a record at `level` would be emitted.
    pub fn enabled(&self, level: Severity) -> bool {
        self.logger.enabled(level)
    }

    /// Converts the record into a [`LogLine`] without submitting it.
    pub fn to_log_line(&self, record: &SlogRecord) -> LogLine {
        line(
            record.level,
            record.formatted_message(),
            &record.file,
            record.line,
        )
    }

    /// Handles a structured record: format it and write it through pm-log.
    pub fn handle(&self, record: SlogRecord) {
        if !self.enabled(record.level) {
            return;
        }
        let line = self.to_log_line(&record);
        self.logger.submit_line(line);
    }
}

/// Maps Go `slog` numeric levels to pm-log severities. Go uses
/// DEBUG=-4, INFO=0, WARN=4, ERROR=8.
pub fn from_slog_level(level: i32) -> Severity {
    match level {
        level if level <= -4 => Severity::Trace,
        -3..=-1 => Severity::Debug,
        0..=3 => Severity::Info,
        4..=7 => Severity::Warning,
        level if level >= 8 => Severity::Error,
        _ => Severity::None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::writer::CaptureSink;
    use std::sync::Mutex;
    use std::time::Duration;

    #[test]
    fn attributes_render_as_key_value() {
        let record = SlogRecord::new(Severity::Info, "request done", "server.go", 12)
            .attr("method", "GET")
            .attr("status", "200");
        assert_eq!(record.formatted_message(), "request done method=GET status=200");
    }

    #[test]
    fn level_mapping_matches_go_slog() {
        assert_eq!(from_slog_level(-8), Severity::Trace);
        assert_eq!(from_slog_level(-4), Severity::Trace);
        assert_eq!(from_slog_level(-2), Severity::Debug);
        assert_eq!(from_slog_level(0), Severity::Info);
        assert_eq!(from_slog_level(4), Severity::Warning);
        assert_eq!(from_slog_level(8), Severity::Error);
    }

    #[test]
    fn handler_writes_go_like_line() {
        let (sink, lines): (Box<CaptureSink>, Arc<Mutex<Vec<String>>>) = {
            let (sink, lines) = CaptureSink::new();
            (Box::new(sink), lines)
        };
        let logger = Logger::with_sink(Severity::Trace, sink);
        let handler = SlogHandler::with_logger(logger);
        handler.handle(
            SlogRecord::new(Severity::Warning, "slow query", "db.go", 8).attr("duration", "1s"),
        );

        let mut output = String::new();
        for _ in 0..100 {
            output = lines.lock().unwrap().join("\n");
            if !output.is_empty() {
                break;
            }
            std::thread::sleep(Duration::from_millis(5));
        }
        assert!(output.contains("WRN"), "{output}");
        assert!(output.contains("slow query duration=1s"), "{output}");
        assert!(output.contains("db:008"), "{output}");
    }
}
