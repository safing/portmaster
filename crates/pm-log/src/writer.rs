//! Log writers, ported from Go `base/log/writer.go`.
//!
//! Go `base/log` creates one timestamped file per process and removes files
//! older than a threshold at startup; it has no size-based rotation and no
//! compression. This port keeps that behaviour as the default and adds an
//! opt-in size rotation so the writer can be used as a general rolling sink.

use std::fs::{self, OpenOptions};
use std::io::{self, Write as IoWrite};
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime};

use crate::format::{format_line, LineCounter, Timestamp};
use crate::message::LogLine;

/// Output sink for formatted log lines.
pub trait Sink: Send {
    /// Writes one formatted line (a trailing newline is added by the writer).
    fn write_message(&mut self, line: &LogLine, duplicates: u64);
    /// Writes a raw string verbatim (used for BOF/EOF/version banners).
    fn write_raw(&mut self, text: &str);
    /// Whether this sink is stdout, which enables ANSI colors like Go.
    fn is_stdout(&self) -> bool {
        false
    }
    /// Name of the current file, used to skip it during cleanup.
    fn current_file_name(&self) -> Option<String> {
        None
    }
    /// Closes the sink.
    fn close(&mut self) {}
}

/// File rotation settings. The defaults match Go `base/log` exactly.
#[derive(Copy, Clone, Debug)]
pub struct Rotation {
    /// Rotate once the current file would exceed this many bytes.
    /// `None` disables size-based rotation, as in Go.
    pub max_size: Option<u64>,
    /// Delete files whose modification time is older than this duration.
    pub max_age: Duration,
}

impl Default for Rotation {
    fn default() -> Rotation {
        Rotation {
            max_size: None,
            max_age: Duration::from_secs(30 * 24 * 60 * 60),
        }
    }
}

/// Writer used by the logger. It is either stdout or a single (rotating) file.
pub struct LogWriter {
    file: Box<dyn IoWrite + Send>,
    path: Option<PathBuf>,
    is_stdout: bool,
    /// Whether to emit ANSI colors, gated by console support on Windows.
    use_color: bool,
    rotation: Rotation,
    current_size: u64,
    counter: LineCounter,
}

impl LogWriter {
    /// Creates a stdout writer. Main log lines are colored, like Go.
    pub fn new_stdout() -> LogWriter {
        LogWriter {
            file: Box::new(io::stdout()),
            path: None,
            is_stdout: true,
            use_color: crate::color_supported(),
            rotation: Rotation::default(),
            current_size: 0,
            counter: LineCounter::default(),
        }
    }

    /// Creates a file writer in `dir`, named `<UTC timestamp>.log`, like Go
    /// `NewFileWriter`.
    pub fn new_file(dir: &Path, rotation: Rotation) -> io::Result<LogWriter> {
        create_log_dir(dir)?;
        let path = new_log_path(dir)?;
        let file = OpenOptions::new().create(true).append(true).open(&path)?;
        let current_size = file.metadata().map(|m| m.len()).unwrap_or(0);
        Ok(LogWriter {
            file: Box::new(file),
            path: Some(path),
            is_stdout: false,
            use_color: false,
            rotation,
            current_size,
            counter: LineCounter::default(),
        })
    }

    /// Forces a rotation, closing the current file and opening a new one.
    pub fn rotate(&mut self) -> io::Result<()> {
        let Some(dir) = self.path.as_ref().and_then(|p| p.parent()) else {
            return Ok(());
        };
        let dir = dir.to_path_buf();
        let path = new_log_path(&dir)?;
        let file = OpenOptions::new().create(true).append(true).open(&path)?;
        self.file = Box::new(file);
        self.current_size = 0;
        self.path = Some(path);
        if let Some(dir) = self.path.as_ref().and_then(|p| p.parent()) {
            let _ = clean_old_logs(dir, self.rotation.max_age, self.current_file_name());
        }
        Ok(())
    }

    fn should_rotate(&self, extra: usize) -> bool {
        match self.rotation.max_size {
            Some(max) => self.current_size.saturating_add(extra as u64) > max,
            None => false,
        }
    }
}

impl Sink for LogWriter {
    fn write_message(&mut self, line: &LogLine, duplicates: u64) {
        let text = format_line(line, duplicates, self.use_color, self.counter.next());
        self.write_raw(&text);
        self.write_raw("\n");
    }

    fn write_raw(&mut self, text: &str) {
        if self.is_stdout {
            let _ = self.file.write_all(text.as_bytes());
            let _ = self.file.flush();
            return;
        }
        if self.should_rotate(text.len() + 1) && self.rotate().is_err() {
            return;
        }
        if self.file.write_all(text.as_bytes()).is_ok() {
            self.current_size = self.current_size.saturating_add(text.len() as u64);
        }
        let _ = self.file.flush();
    }

    fn is_stdout(&self) -> bool {
        self.is_stdout
    }

    fn current_file_name(&self) -> Option<String> {
        self.path
            .as_ref()
            .and_then(|p| p.file_name())
            .map(|n| n.to_string_lossy().into_owned())
    }

    fn close(&mut self) {
        let _ = self.file.flush();
    }
}

/// Creates the log directory with the strict `0o700` permissions used by Go
/// `os.MkdirAll(dir, 0o700)`, without changing an existing directory.
#[cfg(unix)]
fn create_log_dir(dir: &Path) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;

    if dir.exists() {
        return Ok(());
    }
    fs::create_dir_all(dir)?;
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
}

#[cfg(not(unix))]
fn create_log_dir(dir: &Path) -> io::Result<()> {
    fs::create_dir_all(dir)
}

/// Allocates a free `<UTC timestamp>.log` path in `dir`, adding a numeric
/// suffix if a file already exists for the current second.
fn new_log_path(dir: &Path) -> io::Result<PathBuf> {
    let base = Timestamp::now().format_filename();
    let candidate = dir.join(format!("{base}.log"));
    if !candidate.exists() {
        return Ok(candidate);
    }
    for suffix in 1..u32::MAX {
        let candidate = dir.join(format!("{base}-{suffix}.log"));
        if !candidate.exists() {
            return Ok(candidate);
        }
    }
    Err(io::Error::new(
        io::ErrorKind::AlreadyExists,
        "could not allocate a log file name",
    ))
}

/// Deletes files in `dir` older than `threshold`, skipping directories and
/// `skip_name`. This is a direct port of Go `CleanOldLogs`.
pub fn clean_old_logs(dir: &Path, threshold: Duration, skip_name: Option<String>) -> io::Result<()> {
    let delete_older_than = SystemTime::now()
        .checked_sub(threshold)
        .unwrap_or(SystemTime::UNIX_EPOCH);

    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name().to_string_lossy().into_owned();
        if skip_name.as_deref() == Some(name.as_str()) {
            continue;
        }
        let Ok(metadata) = entry.metadata() else {
            continue;
        };
        if metadata.is_dir() {
            continue;
        }
        if let Ok(modified) = metadata.modified() {
            if modified < delete_older_than {
                let _ = fs::remove_file(entry.path());
            }
        }
    }
    Ok(())
}

/// Writer used by tests: captures the raw output in a shared buffer.
#[cfg(test)]
pub(crate) struct CaptureSink {
    pub lines: std::sync::Arc<std::sync::Mutex<Vec<String>>>,
}

#[cfg(test)]
impl CaptureSink {
    pub(crate) fn new() -> (CaptureSink, std::sync::Arc<std::sync::Mutex<Vec<String>>>) {
        let lines = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
        (
            CaptureSink {
                lines: std::sync::Arc::clone(&lines),
            },
            lines,
        )
    }
}

#[cfg(test)]
impl Sink for CaptureSink {
    fn write_message(&mut self, line: &LogLine, duplicates: u64) {
        let text = format_line(line, duplicates, false, 1);
        self.lines.lock().unwrap().push(text);
    }

    fn write_raw(&mut self, text: &str) {
        self.lines.lock().unwrap().push(text.to_owned());
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::severity::Severity;

    fn make_line(msg: &str) -> LogLine {
        LogLine {
            msg: msg.into(),
            level: Severity::Info,
            timestamp: Timestamp(1767323045006),
            file: "test.go".into(),
            line: 7,
            tracer: None,
        }
    }

    #[test]
    fn stdout_writer_formats_and_colors() {
        let mut writer = LogWriter::new_stdout();
        assert!(writer.is_stdout());
        assert_eq!(writer.current_file_name(), None);
        // Writing to captured stdout is harmless; just verify no panic.
        writer.write_message(&make_line("hello"), 0);
    }

    #[test]
    fn file_writer_creates_timestamped_file() {
        let dir = std::env::temp_dir().join(format!("pm-log-writer-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        let writer = LogWriter::new_file(&dir, Rotation::default()).unwrap();
        let name = writer.current_file_name().unwrap();
        assert!(name.ends_with(".log"), "{name}");
        assert_eq!(name.len(), 23, "{name}");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn size_rotation_creates_a_second_file() {
        let dir = std::env::temp_dir().join(format!("pm-log-rotate-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        let rotation = Rotation {
            max_size: Some(100),
            max_age: Duration::from_secs(3600),
        };
        let mut writer = LogWriter::new_file(&dir, rotation).unwrap();
        writer.write_message(&make_line("first"), 0);
        writer.write_message(&make_line("second"), 0);
        let files: Vec<_> = fs::read_dir(&dir)
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .collect();
        assert_eq!(files.len(), 2, "{files:?}");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn clean_old_logs_removes_stale_files_only() {
        let dir = std::env::temp_dir().join(format!("pm-log-clean-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("old.log"), b"old").unwrap();
        fs::write(dir.join("new.log"), b"new").unwrap();
        fs::create_dir(dir.join("subdir")).unwrap();

        let old = SystemTime::now() - Duration::from_secs(60 * 60 * 24 * 40);
        let old_file = OpenOptions::new()
            .write(true)
            .open(dir.join("old.log"))
            .unwrap();
        old_file
            .set_times(fs::FileTimes::new().set_modified(old))
            .unwrap();

        clean_old_logs(&dir, Duration::from_secs(30 * 24 * 3600), None).unwrap();
        assert!(!dir.join("old.log").exists());
        assert!(dir.join("new.log").exists());
        assert!(dir.join("subdir").exists());
        let _ = fs::remove_dir_all(&dir);
    }
}
