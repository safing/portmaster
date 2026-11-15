//! Logging setup.

use std::path::PathBuf;

use tracing_subscriber::fmt::writer::MakeWriter;

use crate::cli::Options;

/// Initializes tracing, either to stdout or to a log file.
pub fn init(options: &Options) {
    let level = level_from_str(&options.log_level);

    if options.log_to_stdout {
        tracing_subscriber::fmt()
            .with_max_level(level)
            .with_target(false)
            .init();
        return;
    }

    let file = Appender {
        path: options.log_dir.join("portmaster-core-rs.log"),
    };
    if tracing_subscriber::fmt()
        .with_max_level(level)
        .with_target(false)
        .with_ansi(false)
        .with_writer(file)
        .try_init()
        .is_err()
    {
        // Fall back to stdout if the file cannot be opened.
        tracing_subscriber::fmt()
            .with_max_level(level)
            .with_target(false)
            .init();
    }
}

fn level_from_str(level: &str) -> tracing::Level {
    match level.to_lowercase().as_str() {
        "trace" => tracing::Level::TRACE,
        "debug" => tracing::Level::DEBUG,
        "warning" | "warn" => tracing::Level::WARN,
        "error" | "critical" => tracing::Level::ERROR,
        _ => tracing::Level::INFO,
    }
}

/// Appends to a log file; a new handle is opened per event, which keeps logging
/// independent of the process lifetime and needs no background task.
struct Appender {
    path: PathBuf,
}

impl<'a> MakeWriter<'a> for Appender {
    type Writer = Box<dyn std::io::Write + 'a>;

    fn make_writer(&'a self) -> Self::Writer {
        match std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)
        {
            Ok(file) => Box::new(file),
            Err(_) => Box::new(std::io::stdout()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn levels_map_to_tracing_levels() {
        assert_eq!(level_from_str("trace"), tracing::Level::TRACE);
        assert_eq!(level_from_str("warning"), tracing::Level::WARN);
        assert_eq!(level_from_str("critical"), tracing::Level::ERROR);
        assert_eq!(level_from_str("nonsense"), tracing::Level::INFO);
    }
}
