//! Socket and address types, matching Go `service/network/socket`.

use std::net::IpAddr;
use std::sync::atomic::{AtomicI32, Ordering};

/// Marks an unknown process ID; must match the Go constant.
pub const UNDEFINED_PROCESS_ID: i32 = -1;

/// An IP and port pair.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct Address {
    pub ip: IpAddr,
    pub port: u16,
}

impl Address {
    pub fn new(ip: IpAddr, port: u16) -> Address {
        Address { ip, port }
    }

    /// Returns whether the address listens on all interfaces (zero IP).
    pub fn listens_any(&self) -> bool {
        self.ip.is_unspecified()
    }
}

impl std::fmt::Display for Address {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self.ip {
            IpAddr::V4(ip) => write!(f, "{ip}:{}", self.port),
            IpAddr::V6(ip) => write!(f, "[{ip}]:{}", self.port),
        }
    }
}

/// A socket that may be assigned to a process.
pub trait SocketInfo: Send + Sync {
    fn pid(&self) -> i32;
    fn set_pid(&self, pid: i32);
    fn uid(&self) -> i32;
    fn uid_and_inode(&self) -> (i32, i64);
}

/// Information about an established connection.
#[derive(Debug)]
pub struct ConnectionInfo {
    pub local: Address,
    pub remote: Address,
    pub uid: i32,
    pub inode: i64,
    pid: AtomicI32,
}

impl ConnectionInfo {
    pub fn new(local: Address, remote: Address, uid: i32, inode: i64) -> ConnectionInfo {
        ConnectionInfo {
            local,
            remote,
            uid,
            inode,
            pid: AtomicI32::new(UNDEFINED_PROCESS_ID),
        }
    }
}

impl SocketInfo for ConnectionInfo {
    fn pid(&self) -> i32 {
        self.pid.load(Ordering::SeqCst)
    }

    fn set_pid(&self, pid: i32) {
        self.pid.store(pid, Ordering::SeqCst);
    }

    fn uid(&self) -> i32 {
        self.uid
    }

    fn uid_and_inode(&self) -> (i32, i64) {
        (self.uid, self.inode)
    }
}

/// Information about a bound socket.
#[derive(Debug)]
pub struct BindInfo {
    pub local: Address,
    pub uid: i32,
    pub inode: i64,
    /// Whether the socket listens on all interfaces.
    pub listens_any: bool,
    pid: AtomicI32,
}

impl BindInfo {
    pub fn new(local: Address, uid: i32, inode: i64) -> BindInfo {
        BindInfo {
            listens_any: local.listens_any(),
            local,
            uid,
            inode,
            pid: AtomicI32::new(UNDEFINED_PROCESS_ID),
        }
    }
}

impl SocketInfo for BindInfo {
    fn pid(&self) -> i32 {
        self.pid.load(Ordering::SeqCst)
    }

    fn set_pid(&self, pid: i32) {
        self.pid.store(pid, Ordering::SeqCst);
    }

    fn uid(&self) -> i32 {
        self.uid
    }

    fn uid_and_inode(&self) -> (i32, i64) {
        (self.uid, self.inode)
    }
}
