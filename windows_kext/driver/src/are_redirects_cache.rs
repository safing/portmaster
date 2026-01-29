use alloc::collections::btree_map::Entry;
use alloc::collections::BTreeMap;
use smoltcp::wire::{IpAddress, IpProtocol};
use wdk::rw_spin_lock::RwSpinLock;

/// Maximum allowed age for a bind redirect record in cache (before cleanup)
const MAX_RECORD_AGE_MS: u64 = 60_000; // 60 seconds

/// Key used for bind redirect cache entries
#[derive(Debug)]
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct BindRedirectKey {
    pub process_id: u64,
    pub protocol: IpProtocol,
    pub is_ipv6: bool,
}

impl BindRedirectKey {
    pub fn new(process_id: u64, protocol: IpProtocol, is_ipv6: bool) -> Self {
        Self { process_id, protocol, is_ipv6 }
    }
}

/// Value stored for each bind redirect cache entry
#[derive(Debug, Clone, Copy)]
pub struct BindRedirectValue {
    /// Timestamp when the entry was added, just for internal use (cleanup)    
    timestamp: u64, 
    /// Verdict. Local address used in the bind redirect.
    pub local_address: IpAddress,
}

impl BindRedirectValue {
    pub fn new(local_address: IpAddress) -> Self {
        Self {
            timestamp: wdk::utils::get_system_timestamp_ms(),
            local_address,
        }
    }
}

/// Cache for bind redirect verdicts.
/// Used to avoid repeated user-mode notifications for same bind operations.
pub struct BindRedirectCache {
    map: BTreeMap<BindRedirectKey, BindRedirectValue>,
    lock: RwSpinLock,
}

impl BindRedirectCache {
    pub fn new() -> Self {
        Self {
            map: BTreeMap::new(),
            lock: RwSpinLock::default(),
        }
    }

    /// Add entry (add verdict) to the bind redirect cache
    pub fn add(&mut self, key: BindRedirectKey, local_address: IpAddress) {
        let _guard = self.lock.write_lock();
        self.map.insert(key, BindRedirectValue::new(local_address));
    }

    /// Returns Some(BindRedirectValue) if entry was found and valid.
    pub fn get(&mut self, info: BindRedirectKey) -> Option<BindRedirectValue> {
        let _guard = self.lock.write_lock();
        match self.map.entry(info) {
            Entry::Occupied(entry) => {                
                let min_timestamp = wdk::utils::get_system_timestamp_ms() - MAX_RECORD_AGE_MS;
                let val = entry.get();
                if val.timestamp < min_timestamp {                    
                    entry.remove(); // Entry is stale - remove and return None
                    return None;
                }
                
                // Entry is fresh
                Some(*val)
            }
            Entry::Vacant(_) => None,
        }
    }

    pub fn cleanup_old_entries(&mut self) -> usize {
        let _guard = self.lock.write_lock();

        let min_timestamp = wdk::utils::get_system_timestamp_ms() - MAX_RECORD_AGE_MS;
        
        let initial_len = self.map.len();
        self.map.retain(|_, extra_info| extra_info.timestamp >= min_timestamp);

        initial_len - self.map.len()
    }

    #[allow(dead_code)]
    pub fn size(&self) -> usize {
        let _guard = self.lock.read_lock();
        self.map.len()
    }
}