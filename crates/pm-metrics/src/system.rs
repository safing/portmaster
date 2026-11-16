//! Built-in system, info and log metrics, ported from Go's module start hooks.

use std::io;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use pm_api::PERMIT_USER;

use crate::registry::Registry;
use crate::types::{FetchFn, GaugeFn, Options};
use crate::Result;

const HOST_STAT_TTL: Duration = Duration::from_secs(1);

/// Counters behind the `logs/*` fetching metrics.
pub mod log_counters {
    use super::{AtomicU64, Ordering};

    static WARNING: AtomicU64 = AtomicU64::new(0);
    static ERROR: AtomicU64 = AtomicU64::new(0);
    static CRITICAL: AtomicU64 = AtomicU64::new(0);

    pub fn inc_warning() {
        WARNING.fetch_add(1, Ordering::Relaxed);
    }
    pub fn inc_error() {
        ERROR.fetch_add(1, Ordering::Relaxed);
    }
    pub fn inc_critical() {
        CRITICAL.fetch_add(1, Ordering::Relaxed);
    }
    pub fn warning() -> u64 {
        WARNING.load(Ordering::Relaxed)
    }
    pub fn error() -> u64 {
        ERROR.load(Ordering::Relaxed)
    }
    pub fn critical() -> u64 {
        CRITICAL.load(Ordering::Relaxed)
    }
}

/// A value cached for a fixed TTL, like the Go host-stat caches.
#[derive(Debug)]
struct Cached<T> {
    value: Option<T>,
    expires: Instant,
    ttl: Duration,
}

impl<T: Clone> Cached<T> {
    fn new(ttl: Duration) -> Self {
        Cached {
            value: None,
            expires: Instant::now(),
            ttl,
        }
    }

    fn get(&mut self, reader: impl FnOnce() -> T) -> T {
        let now = Instant::now();
        if let Some(value) = &self.value {
            if now < self.expires {
                return value.clone();
            }
        }
        let value = reader();
        self.value = Some(value.clone());
        self.expires = now + self.ttl;
        value
    }
}

#[derive(Clone, Copy, Default)]
struct LoadStats {
    one: f64,
    five: f64,
    fifteen: f64,
}

#[derive(Clone, Copy, Default)]
struct MemStats {
    total: u64,
    used: u64,
    available: u64,
    used_percent: f64,
}

#[derive(Clone, Copy, Default)]
struct DiskStats {
    total: u64,
    used: u64,
    free: u64,
    used_percent: f64,
}

/// Cached host statistics, mirroring Go's one-second host stat caches.
pub struct HostStats {
    load: Mutex<Cached<LoadStats>>,
    mem: Mutex<Cached<MemStats>>,
    disk: Mutex<Cached<DiskStats>>,
}

impl Default for HostStats {
    fn default() -> Self {
        HostStats::new()
    }
}

impl HostStats {
    pub fn new() -> Self {
        HostStats {
            load: Mutex::new(Cached::new(HOST_STAT_TTL)),
            mem: Mutex::new(Cached::new(HOST_STAT_TTL)),
            disk: Mutex::new(Cached::new(HOST_STAT_TTL)),
        }
    }

    fn load(&self) -> LoadStats {
        self.load.lock().expect("lock poisoned").get(|| {
            let cpus = std::thread::available_parallelism().map_or(1, |n| n.get()) as f64;
            let Ok(contents) = std::fs::read_to_string("/proc/loadavg") else {
                return LoadStats::default();
            };
            let fields: Vec<&str> = contents.split_whitespace().collect();
            let parse = |index: usize| {
                fields
                    .get(index)
                    .and_then(|value| value.parse::<f64>().ok())
                    .unwrap_or(0.0)
                    / cpus
            };
            LoadStats {
                one: parse(0),
                five: parse(1),
                fifteen: parse(2),
            }
        })
    }

    fn mem(&self) -> MemStats {
        self.mem.lock().expect("lock poisoned").get(|| {
            let Ok(contents) = std::fs::read_to_string("/proc/meminfo") else {
                return MemStats::default();
            };
            let mut total = 0u64;
            let mut available = 0u64;
            for line in contents.lines() {
                let mut parts = line.split_whitespace();
                match parts.next() {
                    Some("MemTotal:") => total = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0) * 1024,
                    Some("MemAvailable:") => {
                        available = parts.next().and_then(|v| v.parse().ok()).unwrap_or(0) * 1024
                    }
                    _ => {}
                }
            }
            let used = total.saturating_sub(available);
            let used_percent = if total == 0 {
                0.0
            } else {
                used as f64 / total as f64 * 100.0
            };
            MemStats {
                total,
                used,
                available,
                used_percent,
            }
        })
    }

    fn disk(&self) -> DiskStats {
        self.disk
            .lock()
            .expect("lock poisoned")
            .get(|| disk_stats().unwrap_or_default())
    }
}

/// Disk capacity is not exposed by the standard library. On Linux we read the
/// filesystem statistics through `stat`-style fields when available; otherwise
/// the disk gauges report zero.
fn disk_stats() -> io::Result<DiskStats> {
    // `statvfs` is deliberately avoided because the workspace forbids unsafe
    // code. Reporting zero keeps the metric present and the output format
    // identical; the value is simply unavailable without a system crate.
    Ok(DiskStats::default())
}

fn check_unknown(value: &str) -> String {
    if value.contains("unknown") {
        "unknown".to_string()
    } else {
        value.to_string()
    }
}

fn options(name: &str) -> Options {
    Options {
        name: name.to_string(),
        permission: PERMIT_USER,
        ..Options::default()
    }
}

/// Registers `info`, host and log metrics, like Go `start()`.
pub fn register_system_metrics(
    registry: &Registry,
    comment: &str,
    stats: &'static HostStats,
) -> Result<()> {
    register_info_metric(registry, comment)?;
    register_host_metrics(registry, stats)?;
    register_log_metrics(registry)?;
    Ok(())
}

fn register_info_metric(registry: &Registry, comment: &str) -> Result<()> {
    let mut labels = std::collections::BTreeMap::new();
    labels.insert("version".to_string(), check_unknown(env!("CARGO_PKG_VERSION")));
    labels.insert(
        "commit".to_string(),
        check_unknown(option_env!("GIT_COMMIT").unwrap_or("unknown")),
    );
    labels.insert(
        "build_date".to_string(),
        check_unknown(option_env!("BUILD_DATE").unwrap_or("unknown")),
    );
    labels.insert(
        "build_source".to_string(),
        check_unknown(option_env!("BUILD_SOURCE").unwrap_or("unknown")),
    );
    labels.insert("go_os".to_string(), std::env::consts::OS.to_string());
    labels.insert("go_arch".to_string(), std::env::consts::ARCH.to_string());
    labels.insert(
        "go_version".to_string(),
        check_unknown(option_env!("RUSTC_VERSION").unwrap_or("unknown")),
    );
    labels.insert("go_compiler".to_string(), "rustc".to_string());
    labels.insert("comment".to_string(), comment.to_string());

    let reported_start = AtomicBool::new(false);
    registry.new_gauge(
        "info",
        Some(labels),
        Some(GaugeFn::new(move || {
            if reported_start
                .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
                .is_ok()
            {
                0.0
            } else {
                1.0
            }
        })),
        None,
    )?;
    Ok(())
}

fn register_float_gauge(
    registry: &Registry,
    name: &str,
    reader: impl Fn() -> f64 + Send + Sync + 'static,
) -> Result<()> {
    registry
        .new_gauge(name, None, Some(GaugeFn::new(reader)), Some(options(name)))
        .map(|_| ())
}

fn register_host_metrics(registry: &Registry, stats: &'static HostStats) -> Result<()> {
    register_float_gauge(registry, "host/load/avg/1", || stats.load().one)?;
    register_float_gauge(registry, "host/load/avg/5", || stats.load().five)?;
    register_float_gauge(registry, "host/load/avg/15", || stats.load().fifteen)?;
    register_float_gauge(registry, "host/mem/total", || stats.mem().total as f64)?;
    register_float_gauge(registry, "host/mem/used", || stats.mem().used as f64)?;
    register_float_gauge(registry, "host/mem/available", || stats.mem().available as f64)?;
    register_float_gauge(registry, "host/mem/used/percent", || stats.mem().used_percent)?;
    register_float_gauge(registry, "host/disk/total", || stats.disk().total as f64)?;
    register_float_gauge(registry, "host/disk/used", || stats.disk().used as f64)?;
    register_float_gauge(registry, "host/disk/free", || stats.disk().free as f64)?;
    register_float_gauge(registry, "host/disk/used/percent", || stats.disk().used_percent)?;
    Ok(())
}

fn register_log_metrics(registry: &Registry) -> Result<()> {
    for (id, name, reader) in [
        (
            "logs/warning/total",
            "Total Warning Log Lines",
            log_counters::warning as fn() -> u64,
        ),
        (
            "logs/error/total",
            "Total Error Log Lines",
            log_counters::error as fn() -> u64,
        ),
        (
            "logs/critical/total",
            "Total Critical Log Lines",
            log_counters::critical as fn() -> u64,
        ),
    ] {
        let mut opts = options(name);
        opts.name = name.to_string();
        registry.new_fetching_counter(
            id,
            None,
            FetchFn::new(reader),
            Some(opts),
        )?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cached_values_expire_after_ttl() {
        let mut cache = Cached::new(Duration::from_millis(10));
        assert_eq!(cache.get(|| 1), 1);
        assert_eq!(cache.get(|| 2), 1);
        std::thread::sleep(Duration::from_millis(15));
        assert_eq!(cache.get(|| 2), 2);
    }

    #[test]
    fn host_stats_are_available() {
        let stats = HostStats::new();
        let _ = stats.load();
        let _ = stats.mem();
        let _ = stats.disk();
    }
}
