use alloc::collections::BTreeMap;
use smoltcp::wire::{IpAddress, Ipv4Address, Ipv6Address};
use wdk::rw_spin_lock::RwSpinLock;

/// Maximum allowed age for a bind redirect record in cache
const MAX_RECORD_AGE_MS: u64 = 60_000; // 60 seconds

/// Key used for bind redirect cache entries
#[derive(Debug)]
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct BindRedirectKey {
    pub process_id: u64,
}

impl BindRedirectKey {
    pub fn new(process_id: u64) -> Self {
        Self { process_id }
    }
}

/// Value stored for each bind redirect cache entry
#[derive(Debug, Clone, Copy)]
pub struct BindRedirectValue {
    /// Timestamp when the entry was added, just for internal use (cleanup)    
    timestamp: u64, 
    /// Verdict. Local address used in the bind redirect.
    local_address_ipv4: Option<Ipv4Address>,
    local_address_ipv6: Option<Ipv6Address>,
}

impl BindRedirectValue {
    /// Creates a new bind redirect verdict.
    /// 
    /// # Arguments
    /// * `addr_ipv4` - IPv4 redirect decision:
    ///     - Unspecified (0.0.0.0) - Allow original bind without redirect
    ///     - Specific address - Redirect bind to this IPv4 address
    /// 
    /// * `addr_ipv6` - IPv6 redirect decision (same semantics as IPv4)
    pub fn new(addr_ipv4: Ipv4Address, addr_ipv6: Ipv6Address) -> Self {
        let local_address_ipv4 = if addr_ipv4.is_unspecified() { None } else { Some(addr_ipv4) };
        let local_address_ipv6 = if addr_ipv6.is_unspecified() { None } else { Some(addr_ipv6) };
        Self {
            timestamp: wdk::utils::get_system_timestamp_ms(),
            local_address_ipv4,
            local_address_ipv6,
        }
    }

    /// Returns the redirect address for the specified IP version.
    /// 
    /// # Arguments
    /// * `ipv6` - `true` for IPv6, `false` for IPv4
    /// 
    /// # Returns
    /// - `None` - Allow original bind (no redirect)
    /// - `Some(specific_ip)` - Redirect to specific IPv4 address
    pub fn get_address(&self, ipv6: bool) -> Option<IpAddress> {
        if ipv6 {
            self.local_address_ipv6.map(|addr| IpAddress::Ipv6(addr))
        } else {
            self.local_address_ipv4.map(|addr| IpAddress::Ipv4(addr))
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
    pub fn add(&mut self, key: BindRedirectKey, val: BindRedirectValue) {
        let _guard = self.lock.write_lock();
        self.map.insert(key, val);
    }

    /// Returns Some(BindRedirectValue) if entry was found and valid.
    pub fn get(&self, info: BindRedirectKey) -> Option<BindRedirectValue> {
        let _guard = self.lock.read_lock();
        
        let val = self.map.get(&info)?;
        
        // Check if entry is stale
        let min_timestamp = wdk::utils::get_system_timestamp_ms() - MAX_RECORD_AGE_MS;
        if val.timestamp < min_timestamp {
            return None; // Entry is stale
        }
        
        // Entry is fresh
        Some(*val)
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