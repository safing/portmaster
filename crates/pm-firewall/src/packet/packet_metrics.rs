//! Packet verdict metrics, port of Go
//! `service/firewall/interception/introspection.go`.
//!
//! The Go code records `(start, duration, verdict)` for traced packets and
//! flushes them to the file given by `--write-packet-metrics` every five
//! seconds. Here the recorder is attached to each nfqueue packet and the
//! writer is driven by the core.

use std::fs::OpenOptions;
use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use super::queued::VerdictRecorder;

/// One recorded packet verdict.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PacketMetricRecord {
    /// Start of the packet handling in Unix nanoseconds.
    pub start: i64,
    /// Time until the verdict was set.
    pub duration: Duration,
    /// Verdict name.
    pub verdict: String,
}

/// Collects and writes packet metrics.
pub struct PacketMetrics {
    destination: PathBuf,
    records: Mutex<Vec<PacketMetricRecord>>,
    enabled: AtomicBool,
    initialized: AtomicBool,
}

impl PacketMetrics {
    /// Creates a writer for the given file.
    pub fn new(destination: PathBuf) -> PacketMetrics {
        PacketMetrics {
            destination,
            records: Mutex::new(Vec::new()),
            enabled: AtomicBool::new(false),
            initialized: AtomicBool::new(false),
        }
    }

    /// Returns whether metrics are enabled.
    pub fn is_enabled(&self) -> bool {
        self.enabled.load(Ordering::SeqCst)
    }

    /// Enables recording and creates the output file, like Go `writeMetrics`.
    pub fn enable(&self) -> std::io::Result<()> {
        self.enabled.store(true, Ordering::SeqCst);
        if !self.initialized.swap(true, Ordering::SeqCst) {
            std::fs::write(&self.destination, b"")?;
        }
        Ok(())
    }

    /// Disables recording and drops pending records, like Go `stop`.
    pub fn disable(&self) {
        self.enabled.store(false, Ordering::SeqCst);
        self.records
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clear();
    }

    /// Returns a recorder for a packet, like Go `packetMetrics.record`.
    pub fn recorder(self: &Arc<Self>) -> VerdictRecorder {
        let metrics = Arc::clone(self);
        Arc::new(move |start, duration, verdict| metrics.record(start, duration, verdict))
    }

    /// Records one verdict, if enabled.
    pub fn record(&self, start: i64, duration: Duration, verdict: &str) {
        if !self.is_enabled() {
            return;
        }
        self.records
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .push(PacketMetricRecord {
                start,
                duration,
                verdict: verdict.to_string(),
            });
    }

    /// Writes pending records to the file and clears them.
    pub fn flush(&self) -> std::io::Result<()> {
        let records: Vec<_> = std::mem::take(
            &mut *self.records.lock().unwrap_or_else(|error| error.into_inner()),
        );
        if records.is_empty() {
            return Ok(());
        }

        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.destination)?;
        for record in records {
            writeln!(
                file,
                "{};{};{};{:.2}",
                record.start,
                format_duration(record.duration),
                record.verdict,
                record.duration.as_secs_f64() * 1_000_000.0
            )?;
        }
        Ok(())
    }

    /// Flushes every five seconds until the token is cancelled.
    pub async fn run(self: &Arc<Self>, token: tokio_util::sync::CancellationToken) {
        let mut interval = tokio::time::interval(Duration::from_secs(5));
        loop {
            tokio::select! {
                () = token.cancelled() => {
                    if self.is_enabled() {
                        if let Err(error) = self.flush() {
                            tracing::warn!(%error, "filter: failed to write packet metrics");
                        }
                    }
                    return;
                }
                _ = interval.tick() => {
                    if self.is_enabled() {
                        if let Err(error) = self.flush() {
                            tracing::warn!(%error, "filter: failed to write packet metrics");
                        }
                    }
                }
            }
        }
    }
}

/// Formats a duration like Go `time.Duration.String`.
fn format_duration(duration: Duration) -> String {
    let nanos = duration.as_nanos();
    if nanos < 1_000 {
        return format!("{nanos}ns");
    }
    if nanos < 1_000_000 {
        return trim_float(nanos as f64 / 1_000.0, "µs");
    }
    if nanos < 1_000_000_000 {
        return trim_float(nanos as f64 / 1_000_000.0, "ms");
    }
    trim_float(nanos as f64 / 1_000_000_000.0, "s")
}

fn trim_float(value: f64, unit: &str) -> String {
    let text = format!("{value:.3}");
    let text = text.trim_end_matches('0').trim_end_matches('.');
    format!("{text}{unit}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn formats_go_durations() {
        assert_eq!(format_duration(Duration::from_nanos(999)), "999ns");
        assert_eq!(format_duration(Duration::from_micros(1)), "1µs");
        assert_eq!(format_duration(Duration::from_micros(1500)), "1.5ms");
        assert_eq!(format_duration(Duration::from_millis(1500)), "1.5s");
    }

    #[test]
    fn recorder_only_records_when_enabled() {
        let dir = std::env::temp_dir().join(format!(
            "pm-firewall-recorder-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .subsec_nanos()
        ));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("metrics.txt");
        let metrics = Arc::new(PacketMetrics::new(path));
        let recorder = metrics.recorder();
        recorder(7, Duration::from_millis(1), "Accept");
        assert!(metrics
            .records
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .is_empty());
        metrics.enable().unwrap();
        recorder(9, Duration::from_millis(2), "Drop");
        assert_eq!(
            metrics
                .records
                .lock()
                .unwrap_or_else(|error| error.into_inner())
                .len(),
            1
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn writes_records_in_go_format() {
        let dir = std::env::temp_dir().join(format!(
            "pm-firewall-metrics-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .subsec_nanos()
        ));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("metrics.txt");
        let metrics = Arc::new(PacketMetrics::new(path.clone()));
        metrics.record(1, Duration::from_millis(1), "Accept");
        metrics.flush().unwrap(); // disabled: nothing written
        assert!(!path.exists());

        metrics.enable().unwrap();
        metrics.record(1_700_000_000_000_000_000, Duration::from_micros(1500), "Accept");
        metrics.record(1_700_000_000_000_000_001, Duration::from_millis(1), "Block");
        metrics.flush().unwrap();
        let content = std::fs::read_to_string(&path).unwrap();
        assert_eq!(
            content,
            "1700000000000000000;1.5ms;Accept;1500.00\n1700000000000000001;1ms;Block;1000.00\n"
        );

        metrics.disable();
        metrics.record(2, Duration::from_secs(1), "Drop");
        metrics.flush().unwrap();
        assert_eq!(std::fs::read_to_string(&path).unwrap(), content);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
