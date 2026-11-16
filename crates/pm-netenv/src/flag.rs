//! Network change notification flag, port of the flag part of Go
//! `service/netenv/network-change.go` and `base/utils.Flag`.
//!
//! Go hands out independent flags that share one broadcast counter: a flag is
//! "set" when a notification happened after its last refresh.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, LazyLock};

/// Source of network change notifications.
#[derive(Debug, Clone, Default)]
pub struct NetworkChangeSource {
    generation: Arc<AtomicU64>,
}

impl NetworkChangeSource {
    /// Creates an empty source.
    pub fn new() -> NetworkChangeSource {
        NetworkChangeSource::default()
    }

    /// Creates a flag that observes this source.
    pub fn new_flag(&self) -> NetworkChangeFlag {
        NetworkChangeFlag {
            generation: Arc::clone(&self.generation),
            seen: Arc::new(AtomicU64::new(self.generation.load(Ordering::SeqCst))),
        }
    }

    /// Notifies all flags, like `NotifyAndReset`.
    pub fn notify(&self) {
        self.generation.fetch_add(1, Ordering::SeqCst);
    }

    /// Returns the current generation.
    pub fn generation(&self) -> u64 {
        self.generation.load(Ordering::SeqCst)
    }
}

static GLOBAL_SOURCE: LazyLock<NetworkChangeSource> = LazyLock::new(NetworkChangeSource::new);

/// Returns the process-wide network change source.
pub fn global_source() -> &'static NetworkChangeSource {
    &GLOBAL_SOURCE
}

/// Returns a flag on the process-wide network change source, like Go
/// `GetNetworkChangedFlag`.
pub fn network_changed_flag() -> NetworkChangeFlag {
    global_source().new_flag()
}

/// Notifies the process-wide network change source.
pub fn notify_network_change() {
    global_source().notify();
}

/// A flag that observes a [`NetworkChangeSource`].
#[derive(Debug, Clone)]
pub struct NetworkChangeFlag {
    generation: Arc<AtomicU64>,
    seen: Arc<AtomicU64>,
}

impl NetworkChangeFlag {
    /// Creates a standalone flag that can be notified directly.
    pub fn standalone() -> NetworkChangeFlag {
        NetworkChangeSource::new().new_flag()
    }

    /// Returns whether the network changed since the last refresh.
    pub fn is_set(&self) -> bool {
        self.generation.load(Ordering::SeqCst) != self.seen.load(Ordering::SeqCst)
    }

    /// Marks the current generation as seen.
    pub fn refresh(&self) {
        self.seen
            .store(self.generation.load(Ordering::SeqCst), Ordering::SeqCst);
    }
}

/// A source with one flag, for tests.
#[derive(Debug, Clone, Default)]
pub struct FlagPair {
    source: NetworkChangeSource,
    flag: NetworkChangeFlag,
}

impl FlagPair {
    /// Creates a source and a flag that observes it.
    pub fn new() -> FlagPair {
        let source = NetworkChangeSource::new();
        let flag = source.new_flag();
        FlagPair { source, flag }
    }

    /// Returns the source.
    pub fn source(&self) -> &NetworkChangeSource {
        &self.source
    }

    /// Returns the flag.
    pub fn flag(&self) -> NetworkChangeFlag {
        self.flag.clone()
    }
}

impl Default for NetworkChangeFlag {
    fn default() -> Self {
        NetworkChangeFlag::standalone()
    }
}
