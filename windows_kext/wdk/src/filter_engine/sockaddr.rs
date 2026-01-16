//! SOCKADDR_STORAGE wrapper for WFP redirect operations.
//!
//! This type provides a safe interface to read and write socket addresses
//! from WFP structures like FWPS_CONNECT_REQUEST0.

use smoltcp::wire::{IpAddress, Ipv4Address, Ipv6Address};

// Socket address family constants
pub const AF_INET: u16 = 2;
pub const AF_INET6: u16 = 23;

/// Windows SOCKADDR_STORAGE structure wrapper.
/// 
/// This is a 128-byte buffer that can hold either IPv4 (SOCKADDR_IN) 
/// or IPv6 (SOCKADDR_IN6) socket addresses.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct SOCKADDR_STORAGE {
    data: [u8; 128],
}

// Internal structures for casting
#[repr(C)]
struct SockAddrIn {
    family: u16,
    port: u16,       // Network byte order (big-endian)
    addr: [u8; 4],   // Network byte order
    zero: [u8; 8],
}

#[repr(C)]
struct SockAddrIn6 {
    family: u16,
    port: u16,        // Network byte order (big-endian)
    flowinfo: u32,
    addr: [u8; 16],   // Network byte order
    scope_id: u32,
}

impl SOCKADDR_STORAGE {
    /// Create a new zeroed SOCKADDR_STORAGE
    pub const fn new() -> Self {
        Self { data: [0u8; 128] }
    }

    /// Get the address family (AF_INET or AF_INET6)
    pub fn family(&self) -> u16 {
        u16::from_ne_bytes([self.data[0], self.data[1]])
    }

    /// Check if this is an IPv4 address
    pub fn is_ipv4(&self) -> bool {
        self.family() == AF_INET
    }

    /// Check if this is an IPv6 address
    pub fn is_ipv6(&self) -> bool {
        self.family() == AF_INET6
    }

    /// Get the IP address
    pub fn ip(&self) -> Option<IpAddress> {
        match self.family() {
            AF_INET => {
                let sock = unsafe { &*(self.data.as_ptr() as *const SockAddrIn) };
                Some(IpAddress::Ipv4(Ipv4Address(sock.addr)))
            }
            AF_INET6 => {
                let sock = unsafe { &*(self.data.as_ptr() as *const SockAddrIn6) };
                Some(IpAddress::Ipv6(Ipv6Address(sock.addr)))
            }
            _ => None,
        }
    }

    /// Get the port (host byte order)
    pub fn port(&self) -> u16 {
        match self.family() {
            AF_INET => {
                let sock = unsafe { &*(self.data.as_ptr() as *const SockAddrIn) };
                u16::from_be(sock.port)
            }
            AF_INET6 => {
                let sock = unsafe { &*(self.data.as_ptr() as *const SockAddrIn6) };
                u16::from_be(sock.port)
            }
            _ => 0,
        }
    }

    /// Get IP address and port as a tuple
    pub fn ip_and_port(&self) -> Option<(IpAddress, u16)> {
        self.ip().map(|ip| (ip, self.port()))
    }

    /// Set the IP address (must match current address family)
    pub fn set_ip(&mut self, ip: &IpAddress) -> Result<(), &'static str> {
        match (self.family(), ip) {
            (AF_INET, IpAddress::Ipv4(ipv4)) => {
                let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn) };
                sock.addr = ipv4.0;
                Ok(())
            }
            (AF_INET6, IpAddress::Ipv6(ipv6)) => {
                let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn6) };
                sock.addr = ipv6.0;
                Ok(())
            }
            (AF_INET, IpAddress::Ipv6(_)) => {
                Err("cannot set IPv6 address on IPv4 socket")
            }
            (AF_INET6, IpAddress::Ipv4(_)) => {
                Err("cannot set IPv4 address on IPv6 socket")
            }
            _ => Err("unsupported address family"),
        }
    }

    /// Set the port (host byte order, will be converted to network order)
    pub fn set_port(&mut self, port: u16) -> Result<(), &'static str> {
        match self.family() {
            AF_INET => {
                let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn) };
                sock.port = port.to_be();
                Ok(())
            }
            AF_INET6 => {
                let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn6) };
                sock.port = port.to_be();
                Ok(())
            }
            _ => Err("unsupported address family"),
        }
    }

    /// Initialize as IPv4 address
    pub fn init_ipv4(&mut self, ip: Ipv4Address, port: u16) {
        self.data = [0u8; 128];
        let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn) };
        sock.family = AF_INET;
        sock.port = port.to_be();
        sock.addr = ip.0;
    }

    /// Initialize as IPv6 address
    pub fn init_ipv6(&mut self, ip: Ipv6Address, port: u16) {
        self.data = [0u8; 128];
        let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn6) };
        sock.family = AF_INET6;
        sock.port = port.to_be();
        sock.addr = ip.0;
    }

    /// Get scope ID (IPv6 only)
    pub fn scope_id(&self) -> Option<u32> {
        if self.family() == AF_INET6 {
            let sock = unsafe { &*(self.data.as_ptr() as *const SockAddrIn6) };
            Some(sock.scope_id)
        } else {
            None
        }
    }

    /// Set scope ID (IPv6 only)
    pub fn set_scope_id(&mut self, scope_id: u32) -> Result<(), &'static str> {
        if self.family() == AF_INET6 {
            let sock = unsafe { &mut *(self.data.as_mut_ptr() as *mut SockAddrIn6) };
            sock.scope_id = scope_id;
            Ok(())
        } else {
            Err("scope_id is only valid for IPv6")
        }
    }

    /// Get raw bytes (for FFI)
    pub fn as_bytes(&self) -> &[u8; 128] {
        &self.data
    }

    /// Get mutable raw bytes (for FFI)
    pub fn as_bytes_mut(&mut self) -> &mut [u8; 128] {
        &mut self.data
    }
}

impl Default for SOCKADDR_STORAGE {
    fn default() -> Self {
        Self::new()
    }
}

impl core::fmt::Debug for SOCKADDR_STORAGE {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self.ip_and_port() {
            Some((ip, port)) => write!(f, "{}:{}", ip, port),
            None => write!(f, "SockAddrStorage(unknown family: {})", self.family()),
        }
    }
}

impl core::fmt::Display for SOCKADDR_STORAGE {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self.ip_and_port() {
            Some((IpAddress::Ipv4(ip), port)) => write!(f, "{}:{}", ip, port),
            Some((IpAddress::Ipv6(ip), port)) => write!(f, "[{}]:{}", ip, port),
            None => write!(f, "invalid"),
        }
    }
}
