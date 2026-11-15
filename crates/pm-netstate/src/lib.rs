//! Compatible implementation of Go `service/network/{proc,state,socket,packet}`
//! for Linux.
//!
//! The crate maps packets to the owning process: it parses the kernel socket
//! tables from `/proc/net/{tcp,tcp6,udp,udp6}` and resolves socket inodes to
//! PIDs by scanning `/proc/<pid>/fd`.
//!
//! # Compatibility notes
//!
//! * The proc root is injectable ([`ProcFs::new`]), which the Go package does
//!   not support; `/proc` is the default. Tests use a fake tree.
//! * Socket owner scans are not single-flighted like in Go: concurrent lookups
//!   for the same inode may scan `/proc` twice. The owner and PID caches are
//!   implemented with the same generations and limits.
//! * `Exists` for UDP compares the bind address with `listens_any` (zero IP)
//!   instead of Go's first-byte check.

pub mod packet;
pub mod proc;
pub mod scope;
pub mod socket;
pub mod state;

pub use packet::{IpProtocol, IpVersion, PacketInfo};
pub use proc::{ProcFs, Stack};
pub use scope::{ip_scope, IpScope};
pub use socket::{Address, BindInfo, ConnectionInfo, SocketInfo, UNDEFINED_PROCESS_ID};
pub use state::{Info, LookupError, LookupResult, NetState};
