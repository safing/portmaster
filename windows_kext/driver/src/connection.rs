use alloc::{
    boxed::Box,
    string::{String, ToString},
};
use core::{
    fmt::{Debug, Display},
    sync::atomic::{AtomicU64, Ordering},
};
use num_derive::FromPrimitive;
use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address, Ipv6Address};

use crate::connection_map::Key;

pub static PM_DNS_PORT: u16 = 53;
pub static PM_SPN_PORT: u16 = 717;

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
    RedirectNameServer = 8,
    RedirectTunnel     = 9,
    Failed             = 10,
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
            Verdict::Failed             => write!(f, "Failed"),
        }
    }
}

#[allow(dead_code)]
impl Verdict {
    /// Returns true if the verdict is a redirect.
    pub fn is_redirect(&self) -> bool {
        matches!(self, Verdict::RedirectNameServer | Verdict::RedirectTunnel)
    }

    /// Returns true if the verdict is a permanent verdict.
    pub fn is_permanent(&self) -> bool {
        matches!(
            self,
            Verdict::PermanentAccept
                | Verdict::PermanentBlock
                | Verdict::PermanentDrop
                | Verdict::RedirectNameServer
                | Verdict::RedirectTunnel
        )
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
}

pub trait Connection {
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

    /// Returns true if the connection is equal to the given key. The key is considered equal if the remote port and address are equal.
    fn remote_equals(&self, key: &Key) -> bool;
    /// Returns true if the connection is equal to the given key for redirecting. The key is considered equal if the remote port and address are equal.
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
    /// Returns the remote port of the connection.
    fn get_remote_port(&self) -> u16;
    /// Returns true if the connection is an IPv6 connection.
    fn is_ipv6(&self) -> bool;
    /// Returns the direction of the connection.
    fn get_direction(&self) -> Direction;
    // Returns the process id of the connection.
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
}

pub struct ConnectionV4 {
    pub(crate) protocol: IpProtocol,
    pub(crate) local_address: Ipv4Address,
    pub(crate) local_port: u16,
    pub(crate) remote_address: Ipv4Address,
    pub(crate) remote_port: u16,
    pub(crate) verdict: Verdict,
    pub(crate) process_id: u64,
    pub(crate) last_accessed_timestamp: AtomicU64,
    pub(crate) extra: Box<ConnectionExtra>,
}

pub struct ConnectionV6 {
    pub(crate) protocol: IpProtocol,
    pub(crate) local_address: Ipv6Address,
    pub(crate) local_port: u16,
    pub(crate) remote_address: Ipv6Address,
    pub(crate) remote_port: u16,
    pub(crate) verdict: Verdict,
    pub(crate) process_id: u64,
    pub(crate) last_accessed_timestamp: AtomicU64,
    pub(crate) extra: Box<ConnectionExtra>,
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
        let IpAddress::Ipv4(local_address) = key.local_address else {
            return Err("wrong ip address version".to_string());
        };

        let IpAddress::Ipv4(remote_address) = key.remote_address else {
            return Err("wrong ip address version".to_string());
        };

        let timestamp = wdk::utils::get_system_timestamp_ms();

        Ok(Self {
            protocol: key.protocol,
            local_address,
            local_port: key.local_port,
            remote_address,
            remote_port: key.remote_port,
            verdict: Verdict::Undecided,
            process_id,
            last_accessed_timestamp: AtomicU64::new(timestamp),
            extra: Box::new(ConnectionExtra {
                direction,
                end_timestamp: 0,
            }),
        })
    }
}

impl Connection for ConnectionV4 {
    fn remote_equals(&self, key: &Key) -> bool {
        if self.remote_port != key.remote_port {
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

    fn get_remote_port(&self) -> u16 {
        self.remote_port
    }

    fn is_ipv6(&self) -> bool {
        false
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
        let IpAddress::Ipv6(local_address) = key.local_address else {
            return Err("wrong ip address version".to_string());
        };

        let IpAddress::Ipv6(remote_address) = key.remote_address else {
            return Err("wrong ip address version".to_string());
        };
        let timestamp = wdk::utils::get_system_timestamp_ms();

        Ok(Self {
            protocol: key.protocol,
            local_address,
            local_port: key.local_port,
            remote_address,
            remote_port: key.remote_port,
            verdict: Verdict::Undecided,
            process_id,
            last_accessed_timestamp: AtomicU64::new(timestamp),
            extra: Box::new(ConnectionExtra {
                direction,
                end_timestamp: 0,
            }),
        })
    }
}

impl Connection for ConnectionV6 {
    fn remote_equals(&self, key: &Key) -> bool {
        if self.remote_port != key.remote_port {
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

    fn get_remote_port(&self) -> u16 {
        self.remote_port
    }

    fn is_ipv6(&self) -> bool {
        true
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
            last_accessed_timestamp: AtomicU64::new(
                self.last_accessed_timestamp.load(Ordering::Relaxed),
            ),
            extra: self.extra.clone(),
        }
    }
}
