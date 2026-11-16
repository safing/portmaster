//! Context tracers, ported from Go `base/log/trace.go`.
//!
//! Go stores a tracer in a `context.Context`. Rust has no such context in this
//! crate, so [`TraceContext`] is a small wrapper that plays the same role:
//! [`TraceContext::add_tracer`] mirrors `AddTracer` and [`TraceContext::tracer`]
//! mirrors `Tracer`.

use std::sync::{Arc, Mutex};

use crate::format::Timestamp;
use crate::logger::{global_logger, Logger};
use crate::message::{strip_source_suffix, LogLine};
use crate::severity::Severity;

/// A collector of trace actions that are submitted as one log line, like Go
/// `ContextTracer`.
pub struct ContextTracer {
    logs: Mutex<Vec<LogLine>>,
    fields: Vec<(String, String)>,
    logger: Arc<Logger>,
}

impl Clone for ContextTracer {
    fn clone(&self) -> ContextTracer {
        ContextTracer {
            logs: Mutex::new(self.logs.lock().unwrap().clone()),
            fields: self.fields.clone(),
            logger: Arc::clone(&self.logger),
        }
    }
}

impl Default for ContextTracer {
    fn default() -> ContextTracer {
        ContextTracer::new()
    }
}

impl ContextTracer {
    /// Creates a tracer attached to the process-wide logger.
    pub fn new() -> ContextTracer {
        ContextTracer::with_logger(global_logger())
    }

    /// Creates a tracer attached to a specific logger, for tests and embedders.
    pub fn with_logger(logger: Arc<Logger>) -> ContextTracer {
        ContextTracer {
            logs: Mutex::new(Vec::new()),
            fields: Vec::new(),
            logger,
        }
    }

    /// Creates a child tracer that inherits the structured fields.
    pub fn child(&self) -> ContextTracer {
        ContextTracer {
            logs: Mutex::new(Vec::new()),
            fields: self.fields.clone(),
            logger: Arc::clone(&self.logger),
        }
    }

    /// Adds a structured field rendered as `key=value` on each action.
    pub fn with_field(mut self, key: impl Into<String>, value: impl Into<String>) -> ContextTracer {
        self.fields.push((key.into(), value.into()));
        self
    }

    /// Adds a structured field in place.
    pub fn add_field(&mut self, key: impl Into<String>, value: impl Into<String>) {
        self.fields.push((key.into(), value.into()));
    }

    /// Submits collected actions as a single log line, like Go
    /// `ContextTracer.Submit`.
    pub fn submit(&self) {
        let mut logs = self.logs.lock().unwrap();
        if logs.is_empty() {
            return;
        }
        let main = logs.pop().expect("logs is not empty");
        let actions = logs.clone();
        drop(logs);

        self.logger.submit_line(LogLine {
            msg: main.msg,
            level: main.level,
            timestamp: main.timestamp,
            file: main.file,
            line: main.line,
            tracer: Some(actions),
        });
    }

    /// Records a trace action.
    #[track_caller]
    pub fn trace(&self, msg: impl Into<String>) {
        self.log(Severity::Trace, msg);
    }

    /// Records a debug action.
    #[track_caller]
    pub fn debug(&self, msg: impl Into<String>) {
        self.log(Severity::Debug, msg);
    }

    /// Records an info action.
    #[track_caller]
    pub fn info(&self, msg: impl Into<String>) {
        self.log(Severity::Info, msg);
    }

    /// Records a warning action.
    #[track_caller]
    pub fn warning(&self, msg: impl Into<String>) {
        self.log(Severity::Warning, msg);
    }

    /// Alias for [`ContextTracer::warning`], matching the `Warn` name used by
    /// Rust logging crates.
    #[track_caller]
    pub fn warn(&self, msg: impl Into<String>) {
        self.warning(msg);
    }

    /// Records an error action.
    #[track_caller]
    pub fn error(&self, msg: impl Into<String>) {
        self.log(Severity::Error, msg);
    }

    /// Records a critical action.
    #[track_caller]
    pub fn critical(&self, msg: impl Into<String>) {
        self.log(Severity::Critical, msg);
    }

    /// Number of collected actions, like `len(tracer.logs)`.
    pub fn len(&self) -> usize {
        self.logs.lock().unwrap().len()
    }

    /// Whether no actions have been collected yet.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns a copy of the collected actions.
    pub fn actions(&self) -> Vec<LogLine> {
        self.logs.lock().unwrap().clone()
    }

    #[track_caller]
    fn log(&self, level: Severity, msg: impl Into<String>) {
        let location = std::panic::Location::caller();
        let mut text = msg.into();
        for (key, value) in &self.fields {
            text.push(' ');
            text.push_str(key);
            text.push('=');
            text.push_str(value);
        }
        self.logs.lock().unwrap().push(LogLine {
            msg: text,
            level,
            timestamp: Timestamp::now(),
            file: strip_source_suffix(location.file()),
            line: location.line(),
            tracer: None,
        });
    }
}

/// Replacement for a Go `context.Context` carrying a tracer.
#[derive(Clone)]
pub struct TraceContext {
    tracer: Option<Arc<ContextTracer>>,
}

impl Default for TraceContext {
    fn default() -> TraceContext {
        TraceContext::root()
    }
}

impl TraceContext {
    /// Creates an empty context.
    pub fn root() -> TraceContext {
        TraceContext { tracer: None }
    }

    /// Adds a new tracer to the context, like Go `AddTracer`. Returns `None`
    /// when the level is not trace, or when a tracer already exists.
    pub fn add_tracer(&mut self) -> Option<Arc<ContextTracer>> {
        let logger = global_logger();
        if logger.get_level() != Severity::Trace || self.tracer.is_some() {
            return None;
        }
        let tracer = Arc::new(ContextTracer::with_logger(logger));
        self.tracer = Some(Arc::clone(&tracer));
        Some(tracer)
    }

    /// Returns the tracer attached to the context, like Go `Tracer`.
    pub fn tracer(&self) -> Option<Arc<ContextTracer>> {
        self.tracer.as_ref().map(Arc::clone)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::writer::CaptureSink;
    use std::time::Duration;

    #[test]
    fn submit_formats_main_line_with_actions() {
        let (sink, lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Trace, Box::new(sink));
        let tracer = ContextTracer::with_logger(logger);
        tracer.trace("api: request received");
        tracer.trace("database: fetching");
        tracer.warning("database: partial failure");
        tracer.submit();

        let out = wait(&lines);
        assert!(out.contains("WRN"), "{out}");
        assert!(out.contains("Σ="), "{out}");
        assert!(out.contains("api: request received"), "{out}");
        assert!(out.contains("database: fetching"), "{out}");
        assert!(out.contains("TRC"), "{out}");
    }

    #[test]
    fn fields_are_appended_and_inherited_by_children() {
        let (sink, lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Trace, Box::new(sink));
        let tracer = ContextTracer::with_logger(logger)
            .with_field("request", "abc")
            .with_field("user", "42");
        let child = tracer.child();
        child.info("handled");
        child.submit();
        let out = wait(&lines);
        assert!(out.contains("handled request=abc user=42"), "{out}");
    }

    #[test]
    fn context_adds_tracer_only_at_trace_level() {
        let logger = global_logger();
        let original = logger.get_level();
        logger.set_level(Severity::Info);
        let mut ctx = TraceContext::root();
        assert!(ctx.add_tracer().is_none());
        logger.set_level(Severity::Trace);
        let tracer = ctx.add_tracer().expect("tracer at trace level");
        assert!(ctx.add_tracer().is_none());
        assert!(Arc::ptr_eq(ctx.tracer().as_ref().unwrap(), &tracer));
        logger.set_level(original);
    }

    #[test]
    fn submit_of_empty_tracer_is_a_noop() {
        let (sink, lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Trace, Box::new(sink));
        let tracer = ContextTracer::with_logger(logger);
        tracer.submit();
        assert!(lines.lock().unwrap().is_empty());
    }

    fn wait(arc: &Arc<Mutex<Vec<String>>>) -> String {
        for _ in 0..100 {
            let guard = arc.lock().unwrap();
            if !guard.is_empty() {
                return guard.join("\n");
            }
            drop(guard);
            std::thread::sleep(Duration::from_millis(5));
        }
        arc.lock().unwrap().join("\n")
    }
}
