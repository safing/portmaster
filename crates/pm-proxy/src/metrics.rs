use std::sync::atomic::{AtomicU64, AtomicUsize, Ordering};

/// A 64-byte cacheline-padded atomic u64 to avoid false sharing.
#[repr(align(64))]
#[derive(Debug, Default)]
pub struct PaddedAtomicU64(AtomicU64);

impl std::ops::Deref for PaddedAtomicU64 {
    type Target = AtomicU64;
    #[inline(always)]
    fn deref(&self) -> &AtomicU64 {
        &self.0
    }
}

/// A 64-byte cacheline-padded atomic usize to avoid false sharing.
#[repr(align(64))]
#[derive(Debug, Default)]
pub struct PaddedAtomicUsize(AtomicUsize);

impl std::ops::Deref for PaddedAtomicUsize {
    type Target = AtomicUsize;
    #[inline(always)]
    fn deref(&self) -> &AtomicUsize {
        &self.0
    }
}

/// ProxyMetrics tracks live and historical connection metrics with cacheline-padded
/// atomics to eliminate false sharing between high-frequency transfer bytes and session counters.
#[derive(Debug, Default)]
pub struct ProxyMetrics {
    pub active_sessions: PaddedAtomicUsize,
    pub total_sessions: PaddedAtomicU64,
    pub bytes_tx: PaddedAtomicU64,
    pub bytes_rx: PaddedAtomicU64,
    pub errors: PaddedAtomicU64,
}

impl ProxyMetrics {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn session_started(&self) {
        self.active_sessions.fetch_add(1, Ordering::Relaxed);
        self.total_sessions.fetch_add(1, Ordering::Relaxed);
    }

    pub fn session_finished(&self) {
        self.active_sessions.fetch_sub(1, Ordering::Relaxed);
    }

    pub fn add_bytes(&self, tx: u64, rx: u64) {
        self.bytes_tx.fetch_add(tx, Ordering::Relaxed);
        self.bytes_rx.fetch_add(rx, Ordering::Relaxed);
    }

    pub fn inc_errors(&self) {
        self.errors.fetch_add(1, Ordering::Relaxed);
    }

    pub fn snapshot(&self) -> MetricsSnapshot {
        MetricsSnapshot {
            active_sessions: self.active_sessions.load(Ordering::Relaxed),
            total_sessions: self.total_sessions.load(Ordering::Relaxed),
            bytes_tx: self.bytes_tx.load(Ordering::Relaxed),
            bytes_rx: self.bytes_rx.load(Ordering::Relaxed),
            errors: self.errors.load(Ordering::Relaxed),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MetricsSnapshot {
    pub active_sessions: usize,
    pub total_sessions: u64,
    pub bytes_tx: u64,
    pub bytes_rx: u64,
    pub errors: u64,
}
