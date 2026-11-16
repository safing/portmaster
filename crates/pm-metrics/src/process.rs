//! Runtime and process metrics — Rust equivalent of Go's
//! `vm.WriteProcessMetrics` used by `base/metrics/metrics_runtime.go`.
//!
//! Instead of delegating to a Go-specific library, this module reads the same
//! kernel-provided values from `/proc/self/*` and registers them as ordinary
//! gauges/fetching counters in the [`Registry`]. Because registration goes
//! through the registry, the `portmaster` namespace and global labels (e.g.
//! `instance`) are applied exactly like to every other metric — no manual
//! per-line rewriting is needed, and no metric ID collides with the Go port.
//!
//! # What is intentionally *not* ported
//!
//! The Go collector also emits a large family of `go_*` metrics. Most of them
//! describe the Go runtime and have no meaningful Rust equivalent:
//!
//! * `go_goroutines` — goroutines do not exist in Rust; [`process_threads`]
//!   reports OS threads instead.
//! * `go_gc_duration_seconds*` — Rust has no automatic tracing garbage collector
//!   (no GC pauses to measure).
//! * `go_memstats_*` — Go heap arenas/span accounting does not correspond to the
//!   system allocator; RSS and virtual size are reported instead.
//! * `go_info{version=...}` — already covered by the existing `info` metric's
//!   `go_version`/`go_compiler` labels.
//!
//! The `process_*` family below is the portable part and is reproduced with
//! clear names and units (bytes, seconds, counts). On non-Linux platforms the
//! parsers return no data and the gauges report zero rather than failing.

use std::time::{SystemTime, UNIX_EPOCH};

use pm_api::PERMIT_ADMIN;
use pm_config::EXPERTISE_LEVEL_DEVELOPER;

use crate::registry::Registry;
use crate::types::{GaugeFn, Options};
use crate::{Error, Result};

/// Linux user-space clock ticks per second (`sysconf(_SC_CLK_TCK)`), used by
/// the `utime`/`stime`/`starttime` fields of `/proc/self/stat`.
const CLOCK_TICKS_PER_SEC: f64 = 100.0;
/// `/proc` reports memory sizes in KiB.
const KIB: f64 = 1024.0;

/// A snapshot of the process values we expose. All fields are zero when the
/// corresponding `/proc` file is unavailable (e.g. on non-Linux hosts).
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub struct ProcessSample {
    /// Resident set size in bytes (`VmRSS`).
    pub resident_bytes: f64,
    /// Virtual memory size in bytes (`VmSize`).
    pub virtual_bytes: f64,
    /// CPU time consumed by the process, in seconds (`utime + stime`).
    pub cpu_seconds: f64,
    /// Unix time at which the process started, in seconds.
    pub start_time_seconds: f64,
    /// Number of OS threads (`Threads:`).
    pub threads: f64,
    /// Number of currently open file descriptors.
    pub open_fds: f64,
    /// Soft limit on open file descriptors (`Max open files`).
    pub max_fds: f64,
    /// Bytes read from storage (`read_bytes` from `/proc/self/io`).
    pub io_read_bytes: f64,
    /// Bytes written to storage (`write_bytes` from `/proc/self/io`).
    pub io_write_bytes: f64,
}

/// Reads `/proc/self/stat`, extracts `utime`, `stime` and `starttime`.
///
/// The `comm` field is parenthesised and may itself contain spaces or `)`, so
/// parsing starts after the *last* `)`, like the Go kernel helpers do.
pub fn parse_stat(contents: &str) -> Option<(u64, u64, u64)> {
    let close = contents.rfind(')')?;
    let fields: Vec<&str> = contents[close + 1..].split_whitespace().collect();
    // After comm: state(3) ppid(4) ... utime(14) stime(15) ... starttime(22).
    let utime = fields.get(11)?.parse().ok()?;
    let stime = fields.get(12)?.parse().ok()?;
    let starttime = fields.get(19)?.parse().ok()?;
    Some((utime, stime, starttime))
}

/// Parses `VmRSS:`, `VmSize:` (both KiB) and `Threads:` from `/proc/self/status`.
pub fn parse_status(contents: &str) -> (f64, f64, f64) {
    let mut resident = 0.0;
    let mut virtual_size = 0.0;
    let mut threads = 0.0;
    for line in contents.lines() {
        let mut parts = line.split_whitespace();
        match parts.next() {
            Some("VmRSS:") => resident = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0.0) * KIB,
            Some("VmSize:") => {
                virtual_size = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0.0) * KIB
            }
            Some("Threads:") => threads = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0.0),
            _ => {}
        }
    }
    (resident, virtual_size, threads)
}

/// Parses the `read_bytes` and `write_bytes` fields of `/proc/self/io`.
pub fn parse_io(contents: &str) -> (f64, f64) {
    let mut read = 0.0;
    let mut write = 0.0;
    for line in contents.lines() {
        let mut parts = line.split_whitespace();
        match parts.next() {
            Some("read_bytes:") => read = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0.0),
            Some("write_bytes:") => write = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0.0),
            _ => {}
        }
    }
    (read, write)
}

/// Parses the first uptime value of `/proc/uptime` in seconds.
pub fn parse_uptime(contents: &str) -> Option<f64> {
    contents.split_whitespace().next()?.parse().ok()
}

/// Parses the soft open-file limit from the `Max open files` line of
/// `/proc/self/limits`. `unlimited` maps to `u64::MAX`.
pub fn parse_max_fds(contents: &str) -> Option<f64> {
    for line in contents.lines() {
        if !line.trim_start().starts_with("Max open files") {
            continue;
        }
        let value = line.split_whitespace().nth(3)?;
        if value == "unlimited" {
            return Some(u64::MAX as f64);
        }
        return value.parse::<u64>().ok().map(|limit| limit as f64);
    }
    None
}

/// Counts entries in a `/proc/self/fd` listing (without `.` and `..`).
pub fn count_fds(contents: &str) -> f64 {
    contents
        .lines()
        .filter(|entry| *entry != "." && *entry != ".." && !entry.is_empty())
        .count() as f64
}

fn read_proc(path: &str) -> Option<String> {
    std::fs::read_to_string(path).ok()
}

/// Reads a fresh sample from `/proc/self`. Unknown/unreadable files leave the
/// corresponding fields at zero, so the metrics stay present and well-formed.
pub fn sample() -> ProcessSample {
    let mut result = ProcessSample::default();

    if let Some(status) = read_proc("/proc/self/status") {
        let (resident, virtual_size, threads) = parse_status(&status);
        result.resident_bytes = resident;
        result.virtual_bytes = virtual_size;
        result.threads = threads;
    }

    let mut cpu_ticks = 0u64;
    let mut start_ticks = 0u64;
    if let Some(stat) = read_proc("/proc/self/stat") {
        if let Some((utime, stime, starttime)) = parse_stat(&stat) {
            cpu_ticks = utime.saturating_add(stime);
            start_ticks = starttime;
        }
    }
    result.cpu_seconds = cpu_ticks as f64 / CLOCK_TICKS_PER_SEC;

    if let Some(uptime) = read_proc("/proc/uptime").and_then(|contents| parse_uptime(&contents)) {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|elapsed| elapsed.as_secs_f64())
            .unwrap_or(0.0);
        let boot_time = now - uptime;
        result.start_time_seconds = boot_time + start_ticks as f64 / CLOCK_TICKS_PER_SEC;
    }

    if let Some(limits) = read_proc("/proc/self/limits") {
        result.max_fds = parse_max_fds(&limits).unwrap_or(0.0);
    }
    if let Some(io) = read_proc("/proc/self/io") {
        let (read, write) = parse_io(&io);
        result.io_read_bytes = read;
        result.io_write_bytes = write;
    }
    // Counting directory entries via the standard library needs no unsafe:
    // `read_dir` yields one entry per open descriptor.
    if let Ok(entries) = std::fs::read_dir("/proc/self/fd") {
        result.open_fds = entries.count() as f64;
    }

    result
}

fn metric_options(name: &str) -> Options {
    Options {
        name: name.to_string(),
        permission: PERMIT_ADMIN,
        expertise_level: EXPERTISE_LEVEL_DEVELOPER,
        ..Options::default()
    }
}

/// Registers the process metrics, ignoring duplicate registrations so a second
/// call (or a registry that already has a metric with the same name) cannot
/// break startup.
pub fn register_process_metrics(registry: &Registry) -> Result<()> {
    macro_rules! gauge {
        ($id:literal, $name:literal, $field:ident) => {
            let reader = || sample().$field;
            match registry.new_gauge(
                $id,
                None,
                Some(GaugeFn::new(reader)),
                Some(metric_options($name)),
            ) {
                Ok(_) => {}
                Err(Error::AlreadyRegistered) => {
                    tracing::debug!("metrics: process metric {} already registered", $id);
                }
                Err(error) => return Err(error),
            }
        };
    }

    gauge!(
        "process_resident_memory_bytes",
        "Resident Memory",
        resident_bytes
    );
    gauge!("process_virtual_memory_bytes", "Virtual Memory", virtual_bytes);
    gauge!("process_cpu_seconds_total", "CPU Time", cpu_seconds);
    gauge!("process_start_time_seconds", "Process Start Time", start_time_seconds);
    gauge!("process_threads", "Process Threads", threads);
    gauge!("process_open_fds", "Open File Descriptors", open_fds);
    gauge!("process_max_fds", "Maximum File Descriptors", max_fds);
    gauge!("process_io_read_bytes", "Bytes Read", io_read_bytes);
    gauge!("process_io_write_bytes", "Bytes Written", io_write_bytes);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const STAT: &str = "1234 (my process) S 1 2 3 4 5 6 7 8 9 10 123 45 6 7 20 0 8 0 98765 1 2";
    const STATUS: &str = "Name:\tmy process\nVmPeak:\t 123456 kB\nVmSize:\t 100000 kB\nVmRSS:\t 25000 kB\nThreads:\t8\n";
    const IO: &str = "rchar: 100\nwchar: 200\nsyscr: 1\nsyscw: 2\nread_bytes: 4096\nwrite_bytes: 8192\n";
    const UPTIME: &str = "12345.67 67890.12\n";
    const LIMITS: &str = "Limit                     Soft Limit           Hard Limit           Units\nMax open files            1048576              1048576              files\n";

    #[test]
    fn parses_proc_stat_with_spaces_in_comm() {
        assert_eq!(parse_stat(STAT), Some((123, 45, 98765)));
        assert_eq!(parse_stat("garbage"), None);
    }

    #[test]
    fn parses_proc_status() {
        let (resident, virtual_size, threads) = parse_status(STATUS);
        assert_eq!(resident, 25_000.0 * KIB);
        assert_eq!(virtual_size, 100_000.0 * KIB);
        assert_eq!(threads, 8.0);
    }

    #[test]
    fn parses_proc_io_uptime_and_limits() {
        assert_eq!(parse_io(IO), (4096.0, 8192.0));
        assert_eq!(parse_uptime(UPTIME), Some(12345.67));
        assert_eq!(parse_max_fds(LIMITS), Some(1048576.0));
        assert_eq!(
            parse_max_fds("Max open files             unlimited           unlimited           files\n"),
            Some(u64::MAX as f64)
        );
    }

    #[test]
    fn counts_open_fds() {
        assert_eq!(count_fds("0\n1\n2\n.\n..\n"), 3.0);
    }

    #[test]
    fn live_sample_is_sane() {
        let sample = sample();
        assert!(sample.threads > 0.0, "at least one OS thread");
        assert!(sample.resident_bytes > 0.0, "a running process has RSS");
        assert!(sample.open_fds >= 3.0, "stdin/stdout/stderr are open");
        assert!(sample.max_fds > 0.0);
        assert!(sample.start_time_seconds > 0.0);
    }

    #[test]
    fn registration_is_idempotent() {
        let registry = Registry::new();
        registry.mark_ready();
        register_process_metrics(&registry).unwrap();
        assert!(registry.len() >= 9);
        // A second registration must not fail the process.
        register_process_metrics(&registry).unwrap();
    }
}
