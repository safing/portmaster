use alloc::collections::BTreeMap;
use smoltcp::wire::{IpAddress, IpProtocol, IpVersion};
use wdk::rw_spin_lock::RwSpinLock;

/// Maximum allowed age for a bind redirect record in cache
const MAX_RECORD_AGE_MS: u64 = 60_000; // 60 seconds

/// Key used for bind redirect cache entries
#[derive(Debug)]
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct BindRedirectKey {
    pub process_id: u64,
    pub protocol: IpProtocol,
    pub local_address: IpAddress,   // usually zeroed at ALE_BIND_REDIRECT stage
    pub local_port: u16,            // usually zeroed at ALE_BIND_REDIRECT stage
}

impl BindRedirectKey {
    pub fn new(process_id: u64, protocol: IpProtocol, local_address: IpAddress, local_port: u16) -> Self {
        Self { process_id, protocol, local_address, local_port }
    }
}

/// Value stored for each bind redirect cache entry
#[derive(Debug, Clone, Copy)]
pub struct BindRedirectValue {
    /// Timestamp when the entry was added, just for internal use (cleanup)    
    timestamp: u64, 
    /// Verdict. Local address used in the bind redirect.    
    local_address: Option<IpAddress>,
}

impl BindRedirectValue {
    /// Creates a new bind redirect verdict.
    /// 
    /// # Arguments
    /// * `addr` - Redirect decision:
    ///     - Unspecified (0.0.0.0 or ::) - Allow original bind without redirect
    ///     - Specific address - Redirect bind to this address
    pub fn new(addr: IpAddress) -> Self {
        let local_address = if addr.is_unspecified() { None } else { Some(addr) };
        
        Self {
            timestamp: wdk::utils::get_system_timestamp_ms(),
            local_address,
        }
    }

    /// Returns the redirect address for the specified IP version.
    /// 
    /// # Arguments
    /// * `ip_ver_expected` - Expected IP version
    /// 
    /// # Returns
    /// - `None` - Allow original bind (no redirect)
    /// - `Some(specific_ip)` - Redirect to specific IPv4 address
    pub fn get_address(&self, is_ip_v6_expected: bool) -> Option<IpAddress> {
        let ip_ver_expected = if is_ip_v6_expected { IpVersion::Ipv6 } else { IpVersion::Ipv4 };
        self.local_address.filter(|addr| addr.version() == ip_ver_expected)
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