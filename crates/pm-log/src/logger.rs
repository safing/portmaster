//! Global logger, background writer and public facade, ported from Go
//! `base/log/logging.go`, `input.go` and `output.go`.

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU64, Ordering};
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use crate::format::{format_line, LineCounter, Timestamp};
use crate::message::{strip_source_suffix, LogLine};
use crate::severity::Severity;
use crate::writer::{self, LogWriter, Sink};

const MAX_UNEXPECTED_LOGS: usize = 10;

/// What the background writer consumes. Raw strings bypass line formatting.
enum WriterEvent {
    Line(LogLine),
    Raw(String),
}

/// Configuration for [`start`].
#[derive(Clone, Debug)]
pub struct StartConfig {
    /// Initial level.
    pub level: Severity,
    /// Write to stdout instead of a file.
    pub to_stdout: bool,
    /// Directory for log files when `to_stdout` is false.
    pub log_dir: PathBuf,
    /// Version string printed in the `running` banner.
    pub version: String,
}

impl Default for StartConfig {
    fn default() -> StartConfig {
        StartConfig {
            level: Severity::Info,
            to_stdout: false,
            log_dir: PathBuf::from("."),
            version: "Portmaster".to_owned(),
        }
    }
}

#[derive(Default)]
struct Shared {
    unexpected: Mutex<std::collections::VecDeque<String>>,
    unexpected_counter: LineCounter,
    warn_lines: AtomicU64,
    err_lines: AtomicU64,
    crit_lines: AtomicU64,
    shutdown: AtomicBool,
    stdout: AtomicBool,
}

/// A logger instance. The global logger is a `Logger` wrapped in an `Arc`;
/// tests can create independent instances with [`Logger::with_sink`].
pub struct Logger {
    level: AtomicU32,
    running: AtomicBool,
    tx: Mutex<Option<Sender<WriterEvent>>>,
    pending: Mutex<Vec<WriterEvent>>,
    shared: Arc<Shared>,
}

impl Default for Logger {
    fn default() -> Logger {
        Logger::new()
    }
}

impl Logger {
    /// Creates a logger that buffers messages until [`Logger::start_with_sink`]
    /// is called.
    pub fn new() -> Logger {
        Logger {
            level: AtomicU32::new(Severity::Info as u32),
            running: AtomicBool::new(false),
            tx: Mutex::new(None),
            pending: Mutex::new(Vec::new()),
            shared: Arc::new(Shared::default()),
        }
    }

    /// Creates a logger that immediately writes into `sink`, for tests and
    /// embedders that want to own the sink.
    pub fn with_sink(level: Severity, sink: Box<dyn Sink>) -> Arc<Logger> {
        let logger = Arc::new(Logger::new());
        logger.start_with_sink(level, sink);
        logger
    }

    /// Starts writing with the given sink. Returns false if already started.
    pub fn start_with_sink(&self, level: Severity, sink: Box<dyn Sink>) -> bool {
        if self.running.swap(true, Ordering::AcqRel) {
            return false;
        }
        self.shared.stdout.store(sink.is_stdout(), Ordering::Relaxed);
        self.set_level(level);

        let (tx, rx) = mpsc::channel::<WriterEvent>();
        {
            let mut guard = self.tx.lock().unwrap();
            *guard = Some(tx);
        }

        let shared = Arc::clone(&self.shared);
        let name = if sink.is_stdout() {
            "pm-log-writer-stdout"
        } else {
            "pm-log-writer-file"
        };
        let _ = std::thread::Builder::new()
            .name(name.to_owned())
            .spawn(move || writer_loop(rx, sink, shared));

        let pending = std::mem::take(&mut *self.pending.lock().unwrap());
        if let Some(tx) = self.tx.lock().unwrap().as_ref() {
            for event in pending {
                let _ = tx.send(event);
            }
        }
        true
    }

    /// Sets the active level. Level changes are always recorded in the output.
    pub fn set_level(&self, level: Severity) {
        let previous = decode_level(self.level.swap(level as u32, Ordering::AcqRel));
        if previous == level {
            return;
        }
        let time = Timestamp::now().format();
        let text = if self.shared.stdout.load(Ordering::Relaxed) {
            format!(
                "\x1b[2m{time}\x1b[22m log level changed from \x1b[34m{}\x1b[0m to \x1b[34m{}\x1b[0m\n",
                previous.name(),
                level.name()
            )
        } else {
            format!(
                "{time} log level changed from {} to {}\n",
                previous.name(),
                level.name()
            )
        };
        self.write_raw(&text);
    }

    /// Returns the active level.
    pub fn get_level(&self) -> Severity {
        decode_level(self.level.load(Ordering::Acquire))
    }

    /// Returns true when `level` passes the filter.
    pub fn enabled(&self, level: Severity) -> bool {
        level as u32 >= self.level.load(Ordering::Acquire)
    }

    /// Submits a fully formed line to the background writer.
    pub fn submit_line(&self, line: LogLine) {
        self.count(&line);
        if !self.enabled(line.level) {
            return;
        }
        self.send(WriterEvent::Line(line));
    }

    /// Returns a copy of the last unexpected log lines, oldest first.
    pub fn get_last_unexpected_logs(&self) -> Vec<String> {
        self.shared.unexpected.lock().unwrap().iter().cloned().collect()
    }

    /// Total number of warning lines logged since startup.
    pub fn total_warning_log_lines(&self) -> u64 {
        self.shared.warn_lines.load(Ordering::Relaxed)
    }

    /// Total number of error lines logged since startup.
    pub fn total_error_log_lines(&self) -> u64 {
        self.shared.err_lines.load(Ordering::Relaxed)
    }

    /// Total number of critical lines logged since startup.
    pub fn total_critical_log_lines(&self) -> u64 {
        self.shared.crit_lines.load(Ordering::Relaxed)
    }

    /// Signals the writer to flush remaining lines and write the EOF banner.
    pub fn shutdown(&self) {
        if self.running.swap(false, Ordering::AcqRel) {
            self.shared.shutdown.store(true, Ordering::Release);
        }
    }

    /// Whether this logger has been started.
    pub fn is_running(&self) -> bool {
        self.running.load(Ordering::Acquire)
    }

    fn count(&self, line: &LogLine) {
        match line.level {
            Severity::Warning => {
                self.shared.warn_lines.fetch_add(1, Ordering::Relaxed);
            }
            Severity::Error => {
                self.shared.err_lines.fetch_add(1, Ordering::Relaxed);
            }
            Severity::Critical => {
                self.shared.crit_lines.fetch_add(1, Ordering::Relaxed);
            }
            _ => {}
        }
    }

    fn send(&self, event: WriterEvent) {
        {
            let guard = self.tx.lock().unwrap();
            if let Some(tx) = guard.as_ref() {
                if let Err(error) = tx.send(event) {
                    drop(guard);
                    self.buffer(error.0);
                }
                return;
            }
        }
        self.buffer(event);
    }

    fn buffer(&self, event: WriterEvent) {
        let mut pending = self.pending.lock().unwrap();
        if pending.len() < 1024 {
            pending.push(event);
        }
    }

    fn write_raw(&self, text: &str) {
        self.send(WriterEvent::Raw(text.to_owned()));
    }
}

fn decode_level(value: u32) -> Severity {
    match value {
        0 => Severity::None,
        1 => Severity::Trace,
        2 => Severity::Debug,
        3 => Severity::Info,
        4 => Severity::Warning,
        5 => Severity::Error,
        6 => Severity::Critical,
        _ => Severity::None,
    }
}

static GLOBAL: OnceLock<Arc<Logger>> = OnceLock::new();

/// Returns the process-wide logger.
pub fn global_logger() -> Arc<Logger> {
    Arc::clone(GLOBAL.get_or_init(|| Arc::new(Logger::new())))
}

/// Starts the process-wide logger. Calling it more than once is a no-op, like
/// Go `log.Start`.
pub fn start(config: StartConfig) -> std::io::Result<()> {
    let logger = global_logger();
    if logger.is_running() {
        return Ok(());
    }

    let sink: Box<dyn Sink> = if config.to_stdout {
        Box::new(LogWriter::new_stdout())
    } else {
        Box::new(LogWriter::new_file(&config.log_dir, writer::Rotation::default())?)
    };
    logger.shared.stdout.store(config.to_stdout, Ordering::Relaxed);
    logger.start_with_sink(config.level, sink);

    write_banners(&logger, &config.version, config.to_stdout);

    if !config.to_stdout {
        if let Err(error) = writer::clean_old_logs(
            &config.log_dir,
            writer::Rotation::default().max_age,
            None,
        ) {
            log(
                Severity::Error,
                format!("log: failed to clean old log files: {error}"),
            );
        }
    }
    Ok(())
}

fn write_banners(logger: &Logger, version: &str, is_stdout: bool) {
    let time = Timestamp::now().format();
    if is_stdout && crate::color_supported() {
        logger.write_raw(&format!("\x1b[2m{time}\x1b[22m \x1b[34mBOF {}\x1b[0m\n", crate::RIGHT_ARROW));
        logger.write_raw(&format!("\x1b[2m{time}\x1b[22m \x1b[34mrunning {version}\x1b[0m\n"));
    } else {
        logger.write_raw(&format!("{time} BOF {}\n", crate::RIGHT_ARROW));
        logger.write_raw(&format!("{time} running {version}\n"));
    }
}

/// Flushes and stops the process-wide logger.
pub fn shutdown() {
    global_logger().shutdown();
}

/// Sets the process-wide log level.
pub fn set_log_level(level: Severity) {
    global_logger().set_level(level);
}

/// Returns the process-wide log level.
pub fn get_log_level() -> Severity {
    global_logger().get_level()
}

/// Returns the last ten unexpected log lines, oldest first.
pub fn get_last_unexpected_logs() -> Vec<String> {
    global_logger().get_last_unexpected_logs()
}

/// Total warning lines logged since startup.
pub fn total_warning_log_lines() -> u64 {
    global_logger().total_warning_log_lines()
}

/// Total error lines logged since startup.
pub fn total_error_log_lines() -> u64 {
    global_logger().total_error_log_lines()
}

/// Total critical lines logged since startup.
pub fn total_critical_log_lines() -> u64 {
    global_logger().total_critical_log_lines()
}

/// Submits a message at `level` to the process-wide logger.
#[track_caller]
pub fn log(level: Severity, msg: impl Into<String>) {
    let logger = global_logger();
    let location = std::panic::Location::caller();
    logger.submit_line(LogLine {
        msg: msg.into(),
        level,
        timestamp: Timestamp::now(),
        file: strip_source_suffix(location.file()),
        line: location.line(),
        tracer: None,
    });
}

/// Logs a trace message.
#[track_caller]
pub fn trace(msg: impl Into<String>) {
    log(Severity::Trace, msg);
}

/// Logs a debug message.
#[track_caller]
pub fn debug(msg: impl Into<String>) {
    log(Severity::Debug, msg);
}

/// Logs an info message.
#[track_caller]
pub fn info(msg: impl Into<String>) {
    log(Severity::Info, msg);
}

/// Logs a warning message.
#[track_caller]
pub fn warning(msg: impl Into<String>) {
    log(Severity::Warning, msg);
}

/// Logs an error message.
#[track_caller]
pub fn error(msg: impl Into<String>) {
    log(Severity::Error, msg);
}

/// Logs a critical message.
#[track_caller]
pub fn critical(msg: impl Into<String>) {
    log(Severity::Critical, msg);
}

/// Builds a line with explicit file/line data, used by the adapters.
pub fn line(level: Severity, msg: impl Into<String>, file: &str, line_no: u32) -> LogLine {
    LogLine {
        msg: msg.into(),
        level,
        timestamp: Timestamp::now(),
        file: strip_source_suffix(file),
        line: line_no,
        tracer: None,
    }
}

fn writer_loop(rx: Receiver<WriterEvent>, mut sink: Box<dyn Sink>, shared: Arc<Shared>) {
    let mut current: Option<LogLine> = None;
    let mut duplicates = 0u64;

    fn flush(current: &mut Option<LogLine>, duplicates: &mut u64, sink: &mut dyn Sink, shared: &Shared) {
        if let Some(line) = current.take() {
            sink.write_message(&line, *duplicates);
            record_unexpected(&line, shared);
            *duplicates = 0;
        }
    }

    fn ingest(
        event: WriterEvent,
        current: &mut Option<LogLine>,
        duplicates: &mut u64,
        sink: &mut dyn Sink,
        shared: &Shared,
    ) {
        match event {
            WriterEvent::Raw(text) => {
                flush(current, duplicates, sink, shared);
                sink.write_raw(&text);
            }
            WriterEvent::Line(line) => match current.take() {
                None => *current = Some(line),
                Some(existing) if existing.equal(&line) => {
                    *duplicates += 1;
                    *current = Some(existing);
                }
                Some(existing) => {
                    sink.write_message(&existing, *duplicates);
                    record_unexpected(&existing, shared);
                    *duplicates = 0;
                    *current = Some(line);
                }
            },
        }
    }

    loop {
        match rx.recv_timeout(Duration::from_millis(10)) {
            Ok(event) => {
                ingest(event, &mut current, &mut duplicates, sink.as_mut(), &shared);
                // Drain the whole immediately available batch before flushing,
                // so consecutive duplicates are collapsed like in Go.
                while let Ok(next) = rx.try_recv() {
                    ingest(next, &mut current, &mut duplicates, sink.as_mut(), &shared);
                }
                flush(&mut current, &mut duplicates, sink.as_mut(), &shared);
            }
            Err(mpsc::RecvTimeoutError::Timeout) => {
                if shared.shutdown.load(Ordering::Acquire) {
                    break;
                }
            }
            Err(mpsc::RecvTimeoutError::Disconnected) => break,
        }
    }

    flush(&mut current, &mut duplicates, sink.as_mut(), &shared);
    let time = Timestamp::now().format();
    let eof = if sink.is_stdout() && crate::color_supported() {
        format!("\x1b[2m{time}\x1b[22m \x1b[34mEOF {}\x1b[0m\n", crate::LEFT_ARROW)
    } else {
        format!("{time} EOF {}\n", crate::LEFT_ARROW)
    };
    sink.write_raw(&eof);
    sink.close();
}

fn record_unexpected(line: &LogLine, shared: &Shared) {
    let unexpected = line.level >= Severity::Warning
        || line
            .tracer
            .as_ref()
            .is_some_and(|actions| actions.iter().any(|a| a.level >= Severity::Warning));
    if !unexpected {
        return;
    }

    let counter = shared.unexpected_counter.next();
    let text = format_line(line, 0, false, counter);
    let mut guard = shared.unexpected.lock().unwrap();
    if guard.len() == MAX_UNEXPECTED_LOGS {
        guard.pop_front();
    }
    guard.push_back(text);
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::writer::CaptureSink;

    fn wait_for_lines(arc: &std::sync::Arc<std::sync::Mutex<Vec<String>>>, count: usize) -> Vec<String> {
        for _ in 0..100 {
            {
                let guard = arc.lock().unwrap();
                if guard.len() >= count {
                    return guard.clone();
                }
            }
            std::thread::sleep(Duration::from_millis(5));
        }
        arc.lock().unwrap().clone()
    }

    #[test]
    fn level_filter_and_stats() {
        let (sink, lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Warning, Box::new(sink));
        logger.submit_line(line(Severity::Info, "filtered", "a.rs", 1));
        logger.submit_line(line(Severity::Warning, "kept", "a.rs", 2));
        logger.submit_line(line(Severity::Error, "bad", "a.rs", 3));
        logger.submit_line(line(Severity::Critical, "fatal", "a.rs", 4));
        let out = wait_for_lines(&lines, 2);
        assert!(out.iter().any(|l| l.contains("kept")), "{out:?}");
        assert!(out.iter().any(|l| l.contains("bad")), "{out:?}");
        assert!(!out.iter().any(|l| l.contains("filtered")), "{out:?}");
        assert_eq!(logger.total_warning_log_lines(), 1);
        assert_eq!(logger.total_error_log_lines(), 1);
        assert_eq!(logger.total_critical_log_lines(), 1);
    }

    #[test]
    fn unexpected_logs_ring_keeps_last_ten() {
        let (sink, _lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Trace, Box::new(sink));
        for i in 0..12 {
            logger.submit_line(line(Severity::Error, format!("error {i}"), "a.rs", i));
        }
        let mut found = Vec::new();
        for _ in 0..100 {
            found = logger.get_last_unexpected_logs();
            if found.last().is_some_and(|line| line.contains("error 11")) {
                break;
            }
            std::thread::sleep(Duration::from_millis(5));
        }
        assert_eq!(found.len(), 10, "{found:?}");
        assert!(found[0].contains("error 2"), "{found:?}");
        assert!(found[9].contains("error 11"), "{found:?}");
    }

    #[test]
    fn level_change_is_logged_directly() {
        let (sink, lines) = CaptureSink::new();
        let logger = Logger::with_sink(Severity::Info, Box::new(sink));
        logger.set_level(Severity::Debug);
        let out = wait_for_lines(&lines, 1);
        assert!(
            out.iter()
                .any(|l| l.contains("log level changed from info to debug")),
            "{out:?}"
        );
    }

    #[test]
    fn pre_start_messages_are_flushed() {
        let logger = Logger::new();
        logger.submit_line(line(Severity::Info, "early", "a.rs", 1));
        let (sink, lines) = CaptureSink::new();
        assert!(logger.start_with_sink(Severity::Info, Box::new(sink)));
        let out = wait_for_lines(&lines, 1);
        assert!(out.iter().any(|l| l.contains("early")), "{out:?}");
    }

    #[test]
    fn raw_banners_bypass_line_formatting() {
        let logger = Logger::new();
        logger.write_raw("RAW BANNER\n");
        let (sink, lines) = CaptureSink::new();
        logger.start_with_sink(Severity::Info, Box::new(sink));
        let out = wait_for_lines(&lines, 1);
        assert_eq!(out, vec!["RAW BANNER\n".to_owned()]);
    }
}
