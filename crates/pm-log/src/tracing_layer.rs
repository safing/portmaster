//! Adapter that routes existing `tracing` events through pm-log, so they use
//! the pm-log writer, rotation, deduplication and unexpected-log storage.

use std::sync::Arc;

use tracing::field::{Field, Visit};
use tracing::{Event, Metadata, Subscriber};
use tracing_subscriber::layer::{Context, Layer};
use tracing_subscriber::prelude::*;

use crate::logger::{global_logger, line, Logger};
use crate::severity::Severity;

/// A `tracing_subscriber` layer that forwards events to pm-log.
pub struct PmLogLayer {
    logger: Arc<Logger>,
}

impl Default for PmLogLayer {
    fn default() -> PmLogLayer {
        PmLogLayer::new()
    }
}

impl PmLogLayer {
    /// Creates a layer attached to the process-wide logger.
    pub fn new() -> PmLogLayer {
        PmLogLayer {
            logger: global_logger(),
        }
    }

    /// Creates a layer attached to a specific logger, for tests and embedders.
    pub fn with_logger(logger: Arc<Logger>) -> PmLogLayer {
        PmLogLayer { logger }
    }
}

#[derive(Default)]
struct EventVisitor {
    message: Option<String>,
    fields: Vec<(String, String)>,
}

impl Visit for EventVisitor {
    fn record_debug(&mut self, field: &Field, value: &dyn std::fmt::Debug) {
        let text = format!("{value:?}");
        if field.name() == "message" {
            self.message = Some(text);
        } else {
            self.fields.push((field.name().to_owned(), text));
        }
    }

    fn record_str(&mut self, field: &Field, value: &str) {
        if field.name() == "message" {
            self.message = Some(value.to_owned());
        } else {
            self.fields.push((field.name().to_owned(), value.to_owned()));
        }
    }
}

impl EventVisitor {
    fn into_message(mut self) -> String {
        let mut text = self.message.take().unwrap_or_default();
        for (key, value) in &self.fields {
            text.push(' ');
            text.push_str(key);
            text.push('=');
            text.push_str(value);
        }
        text
    }
}

impl<S> Layer<S> for PmLogLayer
where
    S: Subscriber,
{
    fn enabled(&self, metadata: &Metadata<'_>, _ctx: Context<'_, S>) -> bool {
        self.logger.enabled(Severity::from_tracing_level(*metadata.level()))
    }

    fn on_event(&self, event: &Event<'_>, _ctx: Context<'_, S>) {
        let metadata = event.metadata();
        let severity = Severity::from_tracing_level(*metadata.level());
        let mut visitor = EventVisitor::default();
        event.record(&mut visitor);
        let message = visitor.into_message();
        let file = metadata.file().unwrap_or("");
        let line_no = metadata.line().unwrap_or(0);
        self.logger
            .submit_line(line(severity, message, file, line_no));
    }
}

/// Installs the pm-log layer as the process-wide `tracing` subscriber. A
/// previously installed subscriber is left untouched, like Go's one-time
/// `slog.SetDefault`.
pub fn install() {
    let subscriber = tracing_subscriber::registry().with(PmLogLayer::new());
    let _ = tracing::subscriber::set_global_default(subscriber);
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::writer::CaptureSink;
    use std::sync::Mutex;
    use std::time::Duration;

    #[test]
    fn tracing_events_reach_pm_log() {
        let (sink, lines): (Box<CaptureSink>, Arc<Mutex<Vec<String>>>) = {
            let (sink, lines) = CaptureSink::new();
            (Box::new(sink), lines)
        };
        let logger = Logger::with_sink(Severity::Trace, sink);
        let subscriber = tracing_subscriber::registry().with(PmLogLayer::with_logger(Arc::clone(&logger)));
        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(component = "test", "hello from tracing");
            tracing::warn!("careful");
        });

        let mut output = String::new();
        for _ in 0..100 {
            output = lines.lock().unwrap().join("\n");
            if output.contains("careful") {
                break;
            }
            std::thread::sleep(Duration::from_millis(5));
        }
        assert!(output.contains("INF"), "{output}");
        assert!(output.contains("hello from tracing component=test"), "{output}");
        assert!(output.contains("WRN"), "{output}");
        assert!(output.contains("careful"), "{output}");
    }
}
