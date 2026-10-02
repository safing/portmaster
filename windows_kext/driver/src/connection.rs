use alloc::string::{String, ToString};
use core::{
    fmt::{Debug, Display},
    sync::atomic::{AtomicU64, Ordering},
};
use num_derive::FromPrimitive;
use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address, Ipv6Address};

use crate::connection_map::Key;

pub const PM_DNS_PORT: u16 = 53;
pub const PM_SPN_PORT: u16 = 717;
pub const PM_SPLIT_TUN_PORT: u16 = 719;

static NEXT_CONNECTION_INSTANCE_ID: AtomicU64 = AtomicU64::new(1);

fn next_connection_instance_id() -> u64 {
    loop {
        let id = NEXT_CONNECTION_INSTANCE_ID.fetch_add(1, Ordering::Relaxed);
        if id != 0 {
            return id;
        }
    }
}

#[inline]
fn ipv6_address_key(address: Ipv6Address) -> (u64, u64) {
    let bytes = address.0;
    (
        u64::from_be_bytes([
            bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
        ]),
        u64::from_be_bytes([
            bytes[8], bytes[9], bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15],
        ]),
    )
}

#[inline]
fn get_monotonic_timestamp_ms() -> u64 {
    #[cfg(not(test))]
    {
        wdk::utils::get_monotonic_timestamp_ms()
    }

    #[cfg(test)]
    {
        // Kernel time is unavailable to the user-mode unit-test executable.
        0
    }
}

/// Returns true if `remote_port` is a port that `redirect_equals` can match on.
///
/// Every arm of `redirect_equals` rejects the key unless its remote port equals
/// the port belonging to that redirect verdict, so a key with any other remote
/// port cannot match a redirected connection at all. `ConnectionMap::read` uses
/// this to skip the linear redirect scan entirely, which is what keeps the
/// binary-searched lookup path from degrading to O(n) on every miss.
///
/// Keep in sync with `redirect_equals`: a new redirect verdict needs its port
/// added here, or connections carrying it will never be found.
pub fn is_redirect_port(remote_port: u16) -> bool {
    remote_port == PM_DNS_PORT || remote_port == PM_SPN_PORT || remote_port == PM_SPLIT_TUN_PORT
}

// Make sure this in sync with the Go version
#[derive(Copy, Clone, FromPrimitive)]
#[repr(u8)]
#[rustfmt::skip]
pub enum Verdict {
    Undecided          = 0, // Undecided is the default status of new connections.
    Undeterminable     = 1,
    Accept             = 2,
    PermanentAccept    = 3,
    Block              = 4,
    PermanentBlock     = 5,
    Drop               = 6,
    PermanentDrop      = 7,
    RedirectNameServer = 8,  // redirect to PM_DNS_PORT port
    RedirectTunnel     = 9,  // redirect to PM_SPN_PORT port
    Failed             = 10,
    RedirectSplitTunnel= 11, // redirect to PM_SPLIT_TUN_PORT port
    // RedirectSplitTunnel must stay last: older Portmaster versions only know verdicts 0–10 and would never send this value.
}

impl Display for Verdict {
    #[rustfmt::skip]
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Verdict::Undecided          => write!(f, "Undecided"),
            Verdict::Undeterminable     => write!(f, "Undeterminable"),
            Verdict::Accept             => write!(f, "Accept"),
            Verdict::PermanentAccept    => write!(f, "PermanentAccept"),
            Verdict::Block              => write!(f, "Block"),
            Verdict::PermanentBlock     => write!(f, "PermanentBlock"),
            Verdict::Drop               => write!(f, "Drop"),
            Verdict::PermanentDrop      => write!(f, "PermanentDrop"),
            Verdict::RedirectNameServer => write!(f, "RedirectNameServer"),
            Verdict::RedirectTunnel     => write!(f, "RedirectTunnel"),
            Verdict::RedirectSplitTunnel=> write!(f, "RedirectSplitTunnel"),
            Verdict::Failed             => write!(f, "Failed"),
        }
    }
}

/// Direction of the connection.
#[derive(Copy, Clone, FromPrimitive)]
#[repr(u8)]
pub enum Direction {
    Outbound = 0,
    Inbound = 1,
}

impl Display for Direction {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Direction::Outbound => write!(f, "Outbound"),
            Direction::Inbound => write!(f, "Inbound"),
        }
    }
}

impl Debug for Direction {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(f, "{}", self)
    }
}

#[derive(Clone)]
pub struct ConnectionExtra {
    pub(crate) end_timestamp: u64,
    pub(crate) direction: Direction,
    /// True when a native WFP endpoint or flow can identify this exact cache
    /// generation and report when it ends. Outbound packet-layer fallback entries
    /// have no such identity and are expired by periodic cleanup instead.
    pub(crate) native_lifecycle: bool,
}

pub trait Connection {
    /// Compact address representation used by the connection-cache index.
    /// IPv4 and IPv6 live in separate maps, so the index does not need the
    /// discriminant and storage overhead of `IpAddress`.
    type RemoteAddressKey: Copy + Ord;

    fn redirect_info(&self) -> Option<RedirectInfo> {
        let redirect_address = if self.is_ipv6() {
            IpAddress::Ipv6(Ipv6Address::LOOPBACK)
        } else {
            IpAddress::Ipv4(Ipv4Address::new(127, 0, 0, 1))
        };

        match self.get_verdict() {
            Verdict::RedirectNameServer => Some(RedirectInfo {
                local_address: self.get_local_address(),
                remote_address: self.get_remote_address(),
                remote_port: self.get_remote_port(),
                redirect_port: PM_DNS_PORT,
                unify: false,
                redirect_address,
            }),
            Verdict::RedirectTunnel => Some(RedirectInfo {
                local_address: self.get_local_address(),
                remote_address: self.get_remote_address(),
                remote_port: self.get_remote_port(),
                redirect_port: PM_SPN_PORT,
                unify: true,
                redirect_address,
            }),
            Verdict::RedirectSplitTunnel => Some(RedirectInfo {
                local_address: self.get_local_address(),
                remote_address: self.get_remote_address(),
                remote_port: self.get_remote_port(),
                redirect_port: PM_SPLIT_TUN_PORT,
                unify: true,
                redirect_address,
            }),
            _ => None,
        }
    }

    /// Returns the key of the connection.
    fn get_key(&self) -> Key {
        Key {
            protocol: self.get_protocol(),
            local_address: self.get_local_address(),
            local_port: self.get_local_port(),
            remote_address: self.get_remote_address(),
            remote_port: self.get_remote_port(),
        }
    }

    /// Returns true if the connection has the same local and remote endpoint as
    /// the given key. The map already groups by protocol and local port, but the
    /// local address still has to be checked here: two local addresses can use
    /// the same port and remote endpoint at the same time.
    fn remote_equals(&self, key: &Key) -> bool;
    /// Returns true if the connection is equal to the given key for redirecting.
    /// The key is considered equal if it matches the redirect endpoint.
    fn redirect_equals(&self, key: &Key) -> bool;
    /// Returns the protocol of the connection.
    fn get_protocol(&self) -> IpProtocol;
    /// Returns the verdict of the connection.
    fn get_verdict(&self) -> Verdict;
    /// Returns the local address of the connection.
    fn get_local_address(&self) -> IpAddress;
    /// Returns the local port of the connection.
    fn get_local_port(&self) -> u16;
    /// Returns the remote address of the connection.
    fn get_remote_address(&self) -> IpAddress;
    /// Returns the compact remote-address index key.
    fn get_remote_address_key(&self) -> Self::RemoteAddressKey;
    /// Converts a tuple's remote address to the family-specific index key.
    fn remote_address_key(key: &Key) -> Option<Self::RemoteAddressKey>;
    /// Returns the remote port of the connection.
    fn get_remote_port(&self) -> u16;
    /// Returns true if the connection is an IPv6 connection.
    fn is_ipv6(&self) -> bool;
    /// Returns the direction of the connection.
    fn get_direction(&self) -> Direction;
    /// Returns the unique cache-instance ID of the connection.
    fn get_instance_id(&self) -> u64;
    /// Returns the process ID of the connection.
    fn get_process_id(&self) -> u64;
    /// Ends the connection.
    fn end(&mut self, timestamp: u64);
    /// Returns true if the connection has ended.
    fn has_ended(&self) -> bool {
        self.get_end_time() > 0
    }
    /// Returns the timestamp when the connection ended.
    fn get_end_time(&self) -> u64;
    /// Returns the timestamp when the connection was last accessed.
    fn get_last_accessed_time(&self) -> u64;
    /// Sets the timestamp when the connection was last accessed.
    fn set_last_accessed_time(&self, timestamp: u64);
    /// Returns whether native WFP state can report this connection's end.
    fn has_native_lifecycle(&self) -> bool;
    /// Marks a fallback entry as owned by a native WFP lifecycle association.
    fn mark_native_lifecycle(&mut self);
}

pub struct ConnectionV4 {
    pub(crate) protocol: IpProtocol,
    pub(crate) local_address: Ipv4Address,
    pub(crate) local_port: u16,
    pub(crate) remote_address: Ipv4Address,
    pub(crate) remote_port: u16,
    pub(crate) verdict: Verdict,
    pub(crate) process_id: u64,
    pub(crate) instance_id: u64,
    pub(crate) last_accessed_timestamp: AtomicU64,
    pub(crate) extra: ConnectionExtra,
}

pub struct ConnectionV6 {
    pub(crate) protocol: IpProtocol,
    pub(crate) local_address: Ipv6Address,
    pub(crate) local_port: u16,
    pub(crate) remote_address: Ipv6Address,
    pub(crate) remote_port: u16,
    pub(crate) verdict: Verdict,
    pub(crate) process_id: u64,
    pub(crate) instance_id: u64,
    pub(crate) last_accessed_timestamp: AtomicU64,
    pub(crate) extra: ConnectionExtra,
}

#[derive(Debug)]
pub struct RedirectInfo {
    pub(crate) local_address: IpAddress,
    pub(crate) remote_address: IpAddress,
    pub(crate) remote_port: u16,
    pub(crate) redirect_port: u16,
    pub(crate) unify: bool,
    pub(crate) redirect_address: IpAddress,
}

impl ConnectionV4 {
    /// Creates a new ipv4 connection from the given key.
    pub fn from_key(key: &Key, process_id: u64, direction: Direction) -> Result<Self, String> {
        Self::from_key_with_lifecycle(key, process_id, direction, true)
    }

    pub(crate) fn from_untracked_key(
        key: &Key,
        process_id: u64,
        direction: Direction,
    ) -> Result<Self, String> {
        Self::from_key_with_lifecycle(key, process_id, direction, false)
    }

    fn from_key_with_lifecycle(
        key: &Key,
        process_id: u64,
        direction: Direction,
        native_lifecycle: bool,
    ) -> Result<Self, String> {
        let IpAddress::Ipv4(local_address) = key.local_address else {
            return Err("wrong ip address version".to_string());
        };

        let IpAddress::Ipv4(remote_address) = key.remote_address else {
            return Err("wrong ip address version".to_string());
        };

        let timestamp = get_monotonic_timestamp_ms();

        Ok(Self {
            protocol: key.protocol,
            local_address,
            local_port: key.local_port,
            remote_address,
            remote_port: key.remote_port,
            verdict: Verdict::Undecided,
            process_id,
            instance_id: next_connection_instance_id(),
            last_accessed_timestamp: AtomicU64::new(timestamp),
            extra: ConnectionExtra {
                direction,
                end_timestamp: 0,
                native_lifecycle,
            },
        })
    }
}

impl Connection for ConnectionV4 {
    type RemoteAddressKey = u32;

    fn remote_equals(&self, key: &Key) -> bool {
        if self.protocol != key.protocol
            || self.local_port != key.local_port
            || self.remote_port != key.remote_port
        {
            return false;
        }
        if let IpAddress::Ipv4(local_address) = &key.local_address {
            if self.local_address != *local_address {
                return false;
            }
        } else {
            return false;
        }
        if let IpAddress::Ipv4(remote_address) = &key.remote_address {
            return self.remote_address.eq(remote_address);
        }
        false
    }

    fn get_key(&self) -> Key {
        Key {
            protocol: self.protocol,
            local_address: IpAddress::Ipv4(self.local_address),
            local_port: self.local_port,
            remote_address: IpAddress::Ipv4(self.remote_address),
            remote_port: self.remote_port,
        }
    }

    fn redirect_equals(&self, key: &Key) -> bool {
        match self.verdict {
            Verdict::RedirectNameServer => {
                if key.remote_port != PM_DNS_PORT {
                    return false;
                }

                match key.remote_address {
                    IpAddress::Ipv4(a) => a.is_loopback(),
                    IpAddress::Ipv6(_) => false,
                }
            }
            Verdict::RedirectTunnel => {
                if key.remote_port != PM_SPN_PORT {
                    return false;
                }
                key.local_address.eq(&key.remote_address)
            }
            Verdict::RedirectSplitTunnel => {
                if key.remote_port != PM_SPLIT_TUN_PORT {
                    return false;
                }
                key.local_address.eq(&key.remote_address)
            }
            _ => false,
        }
    }

    fn get_protocol(&self) -> IpProtocol {
        self.protocol
    }

    fn get_verdict(&self) -> Verdict {
        self.verdict
    }

    fn get_local_address(&self) -> IpAddress {
        IpAddress::Ipv4(self.local_address)
    }

    fn get_local_port(&self) -> u16 {
        self.local_port
    }

    fn get_remote_address(&self) -> IpAddress {
        IpAddress::Ipv4(self.remote_address)
    }

    #[inline]
    fn get_remote_address_key(&self) -> Self::RemoteAddressKey {
        u32::from_be_bytes(self.remote_address.0)
    }

    #[inline]
    fn remote_address_key(key: &Key) -> Option<Self::RemoteAddressKey> {
        match key.remote_address {
            IpAddress::Ipv4(address) => Some(u32::from_be_bytes(address.0)),
            IpAddress::Ipv6(_) => None,
        }
    }

    fn get_remote_port(&self) -> u16 {
        self.remote_port
    }

    fn is_ipv6(&self) -> bool {
        false
    }

    fn get_instance_id(&self) -> u64 {
        self.instance_id
    }

    fn get_process_id(&self) -> u64 {
        self.process_id
    }

    fn get_direction(&self) -> Direction {
        self.extra.direction
    }

    fn end(&mut self, timestamp: u64) {
        self.extra.end_timestamp = timestamp;
    }

    fn get_end_time(&self) -> u64 {
        self.extra.end_timestamp
    }

    fn get_last_accessed_time(&self) -> u64 {
        self.last_accessed_timestamp.load(Ordering::Relaxed)
    }

    fn set_last_accessed_time(&self, timestamp: u64) {
        self.last_accessed_timestamp
            .store(timestamp, Ordering::Relaxed);
    }

    fn has_native_lifecycle(&self) -> bool {
        self.extra.native_lifecycle
    }

    fn mark_native_lifecycle(&mut self) {
        self.extra.native_lifecycle = true;
    }
}

impl Clone for ConnectionV4 {
    fn clone(&self) -> Self {
        Self {
            protocol: self.protocol,
            local_address: self.local_address,
            local_port: self.local_port,
            remote_address: self.remote_address,
            remote_port: self.remote_port,
            verdict: self.verdict,
            process_id: self.process_id,
            instance_id: self.instance_id,
            last_accessed_timestamp: AtomicU64::new(
                self.last_accessed_timestamp.load(Ordering::Relaxed),
            ),
            extra: self.extra.clone(),
        }
    }
}

impl ConnectionV6 {
    /// Creates a new ipv6 connection from the given key.
    pub fn from_key(key: &Key, process_id: u64, direction: Direction) -> Result<Self, String> {
        Self::from_key_with_lifecycle(key, process_id, direction, true)
    }

    pub(crate) fn from_untracked_key(
        key: &Key,
        process_id: u64,
        direction: Direction,
    ) -> Result<Self, String> {
        Self::from_key_with_lifecycle(key, process_id, direction, false)
    }

    fn from_key_with_lifecycle(
        key: &Key,
        process_id: u64,
        direction: Direction,
        native_lifecycle: bool,
    ) -> Result<Self, String> {
        let IpAddress::Ipv6(local_address) = key.local_address else {
            return Err("wrong ip address version".to_string());
        };

        let IpAddress::Ipv6(remote_address) = key.remote_address else {
            return Err("wrong ip address version".to_string());
        };
        let timestamp = get_monotonic_timestamp_ms();

        Ok(Self {
            protocol: key.protocol,
            local_address,
            local_port: key.local_port,
            remote_address,
            remote_port: key.remote_port,
            verdict: Verdict::Undecided,
            process_id,
            instance_id: next_connection_instance_id(),
            last_accessed_timestamp: AtomicU64::new(timestamp),
            extra: ConnectionExtra {
                direction,
                end_timestamp: 0,
                native_lifecycle,
            },
        })
    }
}

impl Connection for ConnectionV6 {
    type RemoteAddressKey = (u64, u64);

    fn remote_equals(&self, key: &Key) -> bool {
        if self.protocol != key.protocol
            || self.local_port != key.local_port
            || self.remote_port != key.remote_port
        {
            return false;
        }
        if let IpAddress::Ipv6(local_address) = &key.local_address {
            if self.local_address != *local_address {
                return false;
            }
        } else {
            return false;
        }
        if let IpAddress::Ipv6(remote_address) = &key.remote_address {
            return self.remote_address.eq(remote_address);
        }
        false
    }
    fn get_key(&self) -> Key {
        Key {
            protocol: self.protocol,
            local_address: IpAddress::Ipv6(self.local_address),
            local_port: self.local_port,
            remote_address: IpAddress::Ipv6(self.remote_address),
            remote_port: self.remote_port,
        }
    }

    fn redirect_equals(&self, key: &Key) -> bool {
        match self.verdict {
            Verdict::RedirectNameServer => {
                if key.remote_port != PM_DNS_PORT {
                    return false;
                }

                match key.remote_address {
                    IpAddress::Ipv4(_) => false,
                    IpAddress::Ipv6(a) => a.is_loopback(),
                }
            }
            Verdict::RedirectTunnel => {
                if key.remote_port != PM_SPN_PORT {
                    return false;
                }
                key.local_address.eq(&key.remote_address)
            }
            Verdict::RedirectSplitTunnel => {
                if key.remote_port != PM_SPLIT_TUN_PORT {
                    return false;
                }
                key.local_address.eq(&key.remote_address)
            }
            _ => false,
        }
    }

    fn get_protocol(&self) -> IpProtocol {
        self.protocol
    }

    fn get_verdict(&self) -> Verdict {
        self.verdict
    }

    fn get_local_address(&self) -> IpAddress {
        IpAddress::Ipv6(self.local_address)
    }

    fn get_local_port(&self) -> u16 {
        self.local_port
    }

    fn get_remote_address(&self) -> IpAddress {
        IpAddress::Ipv6(self.remote_address)
    }

    #[inline]
    fn get_remote_address_key(&self) -> Self::RemoteAddressKey {
        ipv6_address_key(self.remote_address)
    }

    #[inline]
    fn remote_address_key(key: &Key) -> Option<Self::RemoteAddressKey> {
        match key.remote_address {
            IpAddress::Ipv4(_) => None,
            IpAddress::Ipv6(address) => Some(ipv6_address_key(address)),
        }
    }

    fn get_remote_port(&self) -> u16 {
        self.remote_port
    }

    fn is_ipv6(&self) -> bool {
        true
    }

    fn get_instance_id(&self) -> u64 {
        self.instance_id
    }

    fn get_process_id(&self) -> u64 {
        self.process_id
    }

    fn get_direction(&self) -> Direction {
        self.extra.direction
    }

    fn end(&mut self, timestamp: u64) {
        self.extra.end_timestamp = timestamp;
    }

    fn get_end_time(&self) -> u64 {
        self.extra.end_timestamp
    }

    fn get_last_accessed_time(&self) -> u64 {
        self.last_accessed_timestamp.load(Ordering::Relaxed)
    }

    fn set_last_accessed_time(&self, timestamp: u64) {
        self.last_accessed_timestamp
            .store(timestamp, Ordering::Relaxed);
    }

    fn has_native_lifecycle(&self) -> bool {
        self.extra.native_lifecycle
    }

    fn mark_native_lifecycle(&mut self) {
        self.extra.native_lifecycle = true;
    }
}

impl Clone for ConnectionV6 {
    fn clone(&self) -> Self {
        Self {
            protocol: self.protocol,
            local_address: self.local_address,
            local_port: self.local_port,
            remote_address: self.remote_address,
            remote_port: self.remote_port,
            verdict: self.verdict,
            process_id: self.process_id,
            instance_id: self.instance_id,
            last_accessed_timestamp: AtomicU64::new(
                self.last_accessed_timestamp.load(Ordering::Relaxed),
            ),
            extra: self.extra.clone(),
        }
    }
}
