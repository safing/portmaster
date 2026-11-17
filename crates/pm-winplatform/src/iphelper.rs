//! Windows `iphlpapi.dll` connection state tables.
//!
//! Port of Go `service/network/iphelper`. The `GetExtendedTcpTable` and
//! `GetExtendedUdpTable` APIs return a packed table whose first `u32` is the
//! number of entries, followed by fixed-size rows. The row layouts are
//! reproduced byte-for-byte from the Go structs (which mirror the MSDN docs).
//!
//! Parsing is separated from the FFI call: [`parse_table`] and its helpers are
//! pure functions over a byte buffer and are unit-tested on Linux. The actual
//! DLL call only exists under `#[cfg(windows)]`.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

use pm_netstate::{Address, BindInfo, ConnectionInfo, SocketInfo};

/// IP / protocol constants from Go `iphelper.go`.
pub const IPV4: u8 = 4;
pub const IPV6: u8 = 6;
pub const TCP: u8 = 6;
pub const UDP: u8 = 17;

// Windows API constants.
#[cfg(windows)]
const TCP_TABLE_OWNER_PID_ALL: u32 = 5;
#[cfg(windows)]
const UDP_TABLE_OWNER_PID: u32 = 1;
const TCP_STATE_LISTEN: u32 = 2;
#[cfg(windows)]
const WIN_ERR_INSUFFICIENT_BUFFER: u32 = 122; // ERROR_INSUFFICIENT_BUFFER
#[cfg(windows)]
const WIN_ERR_INVALID_PARAMETER: u32 = 87; // ERROR_INVALID_PARAMETER
#[cfg(windows)]
const NO_ERROR: u32 = 0;
#[cfg(windows)]
const AF_INET: u32 = 2;
#[cfg(windows)]
const AF_INET6: u32 = 23;

const MAX_STATE_TABLE_ENTRIES: usize = 65_535;
#[cfg(windows)]
const START_BUF_SIZE: usize = 1024;
#[cfg(windows)]
const MAX_BUF_SIZE: usize = 1_048_576;
#[cfg(windows)]
const BUF_SIZE_USAGE_TTL: usize = 100;
#[cfg(windows)]
const MAX_TRIES: usize = 5;

// Row sizes in bytes, derived from the Go structs.
const TCP4_ROW_LEN: usize = 24;
const TCP6_ROW_LEN: usize = 56;
const UDP4_ROW_LEN: usize = 12;
const UDP6_ROW_LEN: usize = 28;

/// Errors of the iphlpapi port.
#[derive(Debug, thiserror::Error)]
pub enum TableError {
    /// The given protocol/version combination is not supported.
    #[error("invalid protocol")]
    InvalidProtocol,
    /// The returned table was shorter than the entries it claims.
    #[error("truncated table: need {needed} bytes, got {len}")]
    TruncatedTable {
        /// Minimum number of bytes required.
        needed: usize,
        /// Actual number of bytes available.
        len: usize,
    },
    /// Windows rejected one of the arguments.
    #[error("invalid parameter: NT 0x{code:X}")]
    InvalidParameter {
        /// Windows NT status / error code.
        code: u32,
    },
    /// The buffer stayed too small after the maximum number of retries.
    #[error("insufficient buffer after {tries} tries: provided {provided} bytes, required {required} bytes - [NT 0x{code:X}]")]
    InsufficientBuffer {
        /// Number of attempts made.
        tries: usize,
        /// Buffer size that was provided.
        provided: usize,
        /// Buffer size Windows asked for.
        required: u32,
        /// Windows error code.
        code: u32,
    },
    /// Any other Windows error code.
    #[error("unexpected Windows error: NT 0x{code:X}")]
    Unexpected {
        /// Windows error code.
        code: u32,
    },
}

/// Result type of the iphlpapi port.
pub type Result<T> = std::result::Result<T, TableError>;

/// One parsed TCP connection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConnectionEntry {
    pub local: Address,
    pub remote: Address,
    pub pid: i32,
}

/// One parsed listening / bound socket.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BindEntry {
    pub local: Address,
    pub pid: i32,
}

/// The parsed contents of one system state table.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct TableData {
    pub connections: Vec<ConnectionEntry>,
    pub binds: Vec<BindEntry>,
}

impl TableData {
    /// Converts the parsed entries into `pm-netstate` socket objects.
    ///
    /// On Windows the owning PID comes straight from the table row, so it is
    /// written through the [`SocketInfo`] trait instead of being resolved from
    /// an inode like on Linux. `uid` and `inode` stay at their zero values.
    pub fn into_sockets(self) -> (Vec<ConnectionInfo>, Vec<BindInfo>) {
        let mut connections = Vec::with_capacity(self.connections.len());
        for entry in self.connections {
            let info = ConnectionInfo::new(entry.local, entry.remote, 0, 0);
            info.set_pid(entry.pid);
            connections.push(info);
        }

        let mut binds = Vec::with_capacity(self.binds.len());
        for entry in self.binds {
            let info = BindInfo::new(entry.local, 0, 0);
            info.set_pid(entry.pid);
            binds.push(info);
        }

        (connections, binds)
    }
}

/// Converts an `iphlpapi` IPv4 address (little-endian `u32`) to an `Ipv4Addr`.
///
/// Go writes the integer with `binary.LittleEndian.PutUint32` and wraps the
/// four bytes in a `net.IP`.
pub fn convert_ipv4(input: u32) -> Ipv4Addr {
    Ipv4Addr::from(input.to_le_bytes())
}

/// Converts an `iphlpapi` port (network byte order in the low 16 bits) to a
/// host-order port, matching Go's `convertPort`.
pub fn convert_port(input: u32) -> u16 {
    ((input >> 8) | (input << 8)) as u16
}

fn read_u32(data: &[u8], offset: usize) -> Result<u32> {
    let slice = data
        .get(offset..offset + 4)
        .ok_or(TableError::TruncatedTable {
            needed: offset + 4,
            len: data.len(),
        })?;
    let bytes: [u8; 4] = slice
        .try_into()
        .map_err(|_| TableError::TruncatedTable {
            needed: offset + 4,
            len: data.len(),
        })?;
    Ok(u32::from_le_bytes(bytes))
}

fn read_ipv6(data: &[u8], offset: usize) -> Result<Ipv6Addr> {
    let slice = data
        .get(offset..offset + 16)
        .ok_or(TableError::TruncatedTable {
            needed: offset + 16,
            len: data.len(),
        })?;
    let bytes: [u8; 16] = slice
        .try_into()
        .map_err(|_| TableError::TruncatedTable {
            needed: offset + 16,
            len: data.len(),
        })?;
    Ok(Ipv6Addr::from(bytes))
}

fn entry_count(data: &[u8]) -> Result<usize> {
    let count = read_u32(data, 0)? as usize;
    Ok(count.min(MAX_STATE_TABLE_ENTRIES))
}

fn ensure_row_bytes(data: &[u8], row_len: usize, count: usize) -> Result<()> {
    let needed = 4 + count.saturating_mul(row_len);
    if data.len() < needed {
        return Err(TableError::TruncatedTable {
            needed,
            len: data.len(),
        });
    }
    Ok(())
}

/// Parses an IPv4 TCP table, like the `protocol == TCP && ipVersion == IPv4`
/// branch of Go's `getTable`.
pub fn parse_tcp4(data: &[u8]) -> Result<TableData> {
    let count = entry_count(data)?;
    ensure_row_bytes(data, TCP4_ROW_LEN, count)?;
    let mut out = TableData::default();
    for index in 0..count {
        let off = 4 + index * TCP4_ROW_LEN;
        let state = read_u32(data, off)?;
        let local_addr = read_u32(data, off + 4)?;
        let local_port = read_u32(data, off + 8)?;
        let remote_addr = read_u32(data, off + 12)?;
        let remote_port = read_u32(data, off + 16)?;
        let pid = read_u32(data, off + 20)? as i32;
        let local = Address::new(
            IpAddr::V4(convert_ipv4(local_addr)),
            convert_port(local_port),
        );
        if state == TCP_STATE_LISTEN {
            out.binds.push(BindEntry { local, pid });
        } else {
            out.connections.push(ConnectionEntry {
                local,
                remote: Address::new(
                    IpAddr::V4(convert_ipv4(remote_addr)),
                    convert_port(remote_port),
                ),
                pid,
            });
        }
    }
    Ok(out)
}

/// Parses an IPv6 TCP table (MSDN `MIB_TCP6TABLE_OWNER_PID`).
pub fn parse_tcp6(data: &[u8]) -> Result<TableData> {
    let count = entry_count(data)?;
    ensure_row_bytes(data, TCP6_ROW_LEN, count)?;
    let mut out = TableData::default();
    for index in 0..count {
        let off = 4 + index * TCP6_ROW_LEN;
        let local_addr = read_ipv6(data, off)?;
        // off + 16 is localScopeId, skipped like the Go `_` field.
        let local_port = read_u32(data, off + 20)?;
        let remote_addr = read_ipv6(data, off + 24)?;
        // off + 40 is remoteScopeId.
        let remote_port = read_u32(data, off + 44)?;
        let state = read_u32(data, off + 48)?;
        let pid = read_u32(data, off + 52)? as i32;
        let local = Address::new(IpAddr::V6(local_addr), convert_port(local_port));
        if state == TCP_STATE_LISTEN {
            out.binds.push(BindEntry { local, pid });
        } else {
            out.connections.push(ConnectionEntry {
                local,
                remote: Address::new(IpAddr::V6(remote_addr), convert_port(remote_port)),
                pid,
            });
        }
    }
    Ok(out)
}

/// Parses an IPv4 UDP table (MSDN `MIB_UDPTABLE_OWNER_PID`).
pub fn parse_udp4(data: &[u8]) -> Result<TableData> {
    let count = entry_count(data)?;
    ensure_row_bytes(data, UDP4_ROW_LEN, count)?;
    let mut out = TableData::default();
    for index in 0..count {
        let off = 4 + index * UDP4_ROW_LEN;
        let local_addr = read_u32(data, off)?;
        let local_port = read_u32(data, off + 4)?;
        let pid = read_u32(data, off + 8)? as i32;
        out.binds.push(BindEntry {
            local: Address::new(
                IpAddr::V4(convert_ipv4(local_addr)),
                convert_port(local_port),
            ),
            pid,
        });
    }
    Ok(out)
}

/// Parses an IPv6 UDP table (MSDN `MIB_UDP6TABLE_OWNER_PID`).
pub fn parse_udp6(data: &[u8]) -> Result<TableData> {
    let count = entry_count(data)?;
    ensure_row_bytes(data, UDP6_ROW_LEN, count)?;
    let mut out = TableData::default();
    for index in 0..count {
        let off = 4 + index * UDP6_ROW_LEN;
        let local_addr = read_ipv6(data, off)?;
        // off + 16 is localScopeId.
        let local_port = read_u32(data, off + 20)?;
        let pid = read_u32(data, off + 24)? as i32;
        out.binds.push(BindEntry {
            local: Address::new(IpAddr::V6(local_addr), convert_port(local_port)),
            pid,
        });
    }
    Ok(out)
}

/// Dispatches a raw table buffer to the parser for the given version/protocol.
pub fn parse_table(data: &[u8], ip_version: u8, protocol: u8) -> Result<TableData> {
    match (ip_version, protocol) {
        (IPV4, TCP) => parse_tcp4(data),
        (IPV6, TCP) => parse_tcp6(data),
        (IPV4, UDP) => parse_udp4(data),
        (IPV6, UDP) => parse_udp6(data),
        _ => Err(TableError::InvalidProtocol),
    }
}

// ---------------------------------------------------------------------------
// Windows-only FFI
// ---------------------------------------------------------------------------

#[cfg(windows)]
mod ffi {
    use std::ffi::c_void;

    // The only `unsafe` in the crate: the two iphlpapi.dll imports.
    #[allow(unsafe_code)]
    #[link(name = "iphlpapi")]
    unsafe extern "system" {
        pub(crate) fn GetExtendedTcpTable(
            table: *mut c_void,
            size: *mut u32,
            order: i32,
            af: u32,
            table_class: u32,
            reserved: u32,
        ) -> u32;
        pub(crate) fn GetExtendedUdpTable(
            table: *mut c_void,
            size: *mut u32,
            order: i32,
            af: u32,
            table_class: u32,
            reserved: u32,
        ) -> u32;
    }
}

/// A buffer-size hint that shrinks after a number of uses, like Go's
/// `learningBufSize`.
#[cfg(windows)]
struct LearningBufSize {
    size: usize,
    uses_left: usize,
    use_for: usize,
    start: usize,
    max: usize,
}

#[cfg(windows)]
impl LearningBufSize {
    fn new(start: usize, max: usize, ttl: usize) -> Self {
        Self {
            size: start,
            uses_left: ttl,
            use_for: ttl,
            start,
            max,
        }
    }

    fn get(&mut self) -> usize {
        self.uses_left -= 1;
        if self.uses_left == 0 {
            self.size = (self.size / 2).max(self.start);
            self.uses_left = self.use_for;
        }
        self.size
    }

    fn increase(&mut self, min_size: usize) -> usize {
        self.size = self.size.saturating_mul(2);
        while self.size < min_size && self.size < self.max {
            self.size = self.size.saturating_mul(2);
        }
        self.size = self.size.min(self.max);
        self.uses_left = self.use_for;
        self.size
    }
}

#[cfg(windows)]
fn address_family(ip_version: u8) -> Result<u32> {
    match ip_version {
        IPV4 => Ok(AF_INET),
        IPV6 => Ok(AF_INET6),
        _ => Err(TableError::InvalidProtocol),
    }
}

/// Queries one system state table from `iphlpapi.dll`.
///
/// The DLL call is retried up to five times when Windows reports that the
/// buffer is too small, growing the buffer with a per-table size hint exactly
/// like Go's `learningBufSize` logic.
#[cfg(windows)]
#[allow(unsafe_code)]
pub fn get_table(ip_version: u8, protocol: u8) -> Result<TableData> {
    use std::sync::{Mutex, OnceLock};

    static TCP4_BUF: OnceLock<Mutex<LearningBufSize>> = OnceLock::new();
    static UDP4_BUF: OnceLock<Mutex<LearningBufSize>> = OnceLock::new();
    static TCP6_BUF: OnceLock<Mutex<LearningBufSize>> = OnceLock::new();
    static UDP6_BUF: OnceLock<Mutex<LearningBufSize>> = OnceLock::new();

    let af = address_family(ip_version)?;
    let hint = match (ip_version, protocol) {
        (IPV4, TCP) => TCP4_BUF.get_or_init(|| Mutex::new(LearningBufSize::new(START_BUF_SIZE, MAX_BUF_SIZE, BUF_SIZE_USAGE_TTL))),
        (IPV4, UDP) => UDP4_BUF.get_or_init(|| Mutex::new(LearningBufSize::new(START_BUF_SIZE, MAX_BUF_SIZE, BUF_SIZE_USAGE_TTL))),
        (IPV6, TCP) => TCP6_BUF.get_or_init(|| Mutex::new(LearningBufSize::new(START_BUF_SIZE, MAX_BUF_SIZE, BUF_SIZE_USAGE_TTL))),
        (IPV6, UDP) => UDP6_BUF.get_or_init(|| Mutex::new(LearningBufSize::new(START_BUF_SIZE, MAX_BUF_SIZE, BUF_SIZE_USAGE_TTL))),
        _ => return Err(TableError::InvalidProtocol),
    };

    let provided = hint.lock().expect("lock poisoned").get();
    let mut buf = vec![0u8; provided];

    for attempt in 1..=MAX_TRIES {
        let mut size = provided as u32;
        let code = unsafe {
            match protocol {
                TCP => ffi::GetExtendedTcpTable(
                    buf.as_mut_ptr().cast::<std::ffi::c_void>(),
                    &mut size,
                    0,
                    af,
                    TCP_TABLE_OWNER_PID_ALL,
                    0,
                ),
                UDP => ffi::GetExtendedUdpTable(
                    buf.as_mut_ptr().cast::<std::ffi::c_void>(),
                    &mut size,
                    0,
                    af,
                    UDP_TABLE_OWNER_PID,
                    0,
                ),
                _ => return Err(TableError::InvalidProtocol),
            }
        };

        match code {
            NO_ERROR => return parse_table(&buf, ip_version, protocol),
            WIN_ERR_INSUFFICIENT_BUFFER if attempt < MAX_TRIES => {
                let new_size = hint.lock().expect("lock poisoned").increase(size as usize);
                buf.resize(new_size, 0);
            }
            WIN_ERR_INSUFFICIENT_BUFFER => {
                return Err(TableError::InsufficientBuffer {
                    tries: attempt,
                    provided,
                    required: size,
                    code,
                });
            }
            WIN_ERR_INVALID_PARAMETER => {
                return Err(TableError::InvalidParameter { code });
            }
            other => return Err(TableError::Unexpected { code: other }),
        }
    }

    Err(TableError::InsufficientBuffer {
        tries: MAX_TRIES,
        provided,
        required: 0,
        code: WIN_ERR_INSUFFICIENT_BUFFER,
    })
}

/// Returns the IPv4 TCP table (connections and listeners).
#[cfg(windows)]
pub fn get_tcp4_table() -> Result<TableData> {
    get_table(IPV4, TCP)
}

/// Returns the IPv6 TCP table (connections and listeners).
#[cfg(windows)]
pub fn get_tcp6_table() -> Result<TableData> {
    get_table(IPV6, TCP)
}

/// Returns the IPv4 UDP bind table.
#[cfg(windows)]
pub fn get_udp4_table() -> Result<TableData> {
    get_table(IPV4, UDP)
}

/// Returns the IPv6 UDP bind table.
#[cfg(windows)]
pub fn get_udp6_table() -> Result<TableData> {
    get_table(IPV6, UDP)
}

/// Non-Windows stub: the tables can only be read from `iphlpapi.dll`.
#[cfg(not(windows))]
pub fn get_table(_ip_version: u8, _protocol: u8) -> Result<TableData> {
    Err(TableError::InvalidProtocol)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn push_u32(buf: &mut Vec<u8>, value: u32) {
        buf.extend_from_slice(&value.to_le_bytes());
    }

    /// Encodes a host-order port the way iphlpapi stores it (byte-swapped in
    /// the low 16 bits), so that `convert_port` recovers `port`.
    fn net_port(port: u16) -> u32 {
        ((port as u32 & 0xFF) << 8) | (port as u32 >> 8)
    }

    #[test]
    fn converts_ipv4_little_endian() {
        // 127.0.0.1 is 0x0100007F in memory order.
        assert_eq!(convert_ipv4(0x0100_007F), Ipv4Addr::new(127, 0, 0, 1));
        assert_eq!(convert_ipv4(0x0100_00C0), Ipv4Addr::new(192, 0, 0, 1));
    }

    #[test]
    fn converts_port_byte_order() {
        assert_eq!(convert_port(net_port(443)), 443);
        assert_eq!(convert_port(net_port(53)), 53);
        assert_eq!(convert_port(net_port(8080)), 8080);
    }

    #[test]
    fn parses_tcp4_connections_and_listeners() {
        let mut buf = Vec::new();
        push_u32(&mut buf, 2);
        // Listener on 0.0.0.0:8080, pid 4242.
        push_u32(&mut buf, TCP_STATE_LISTEN);
        push_u32(&mut buf, 0);
        push_u32(&mut buf, net_port(8080));
        push_u32(&mut buf, 0);
        push_u32(&mut buf, 0);
        push_u32(&mut buf, 4242);
        // Established 192.0.2.1:443 -> 198.51.100.7:5555, pid 99.
        push_u32(&mut buf, 5);
        push_u32(&mut buf, u32::from_le_bytes([192, 0, 2, 1]));
        push_u32(&mut buf, net_port(443));
        push_u32(&mut buf, u32::from_le_bytes([198, 51, 100, 7]));
        push_u32(&mut buf, net_port(5555));
        push_u32(&mut buf, 99);

        let table = parse_table(&buf, IPV4, TCP).unwrap();
        assert_eq!(table.binds.len(), 1);
        assert_eq!(table.connections.len(), 1);
        assert_eq!(
            table.binds[0].local,
            Address::new(IpAddr::V4(Ipv4Addr::UNSPECIFIED), 8080)
        );
        assert_eq!(table.binds[0].pid, 4242);
        assert_eq!(
            table.connections[0].remote,
            Address::new(IpAddr::V4(Ipv4Addr::new(198, 51, 100, 7)), 5555)
        );
        assert_eq!(table.connections[0].pid, 99);

        let (connections, binds) = table.into_sockets();
        assert_eq!(connections[0].pid(), 99);
        assert_eq!(binds[0].pid(), 4242);
    }

    #[test]
    fn parses_udp6_binds() {
        let mut buf = Vec::new();
        push_u32(&mut buf, 1);
        buf.extend_from_slice(&Ipv6Addr::LOCALHOST.octets());
        buf.extend_from_slice(&[
            0, 0, 0, 0, // localScopeId
        ]);
        push_u32(&mut buf, net_port(53));
        push_u32(&mut buf, 1234);
        let table = parse_table(&buf, IPV6, UDP).unwrap();
        assert_eq!(table.binds.len(), 1);
        assert_eq!(
            table.binds[0].local,
            Address::new(IpAddr::V6(Ipv6Addr::LOCALHOST), 53)
        );
        assert_eq!(table.binds[0].pid, 1234);
    }

    #[test]
    fn rejects_short_buffers_and_bad_protocol() {
        assert!(parse_tcp4(&[0, 0]).is_err());
        let mut buf = Vec::new();
        push_u32(&mut buf, 1);
        assert!(parse_udp4(&buf).is_err());
        assert!(parse_table(&buf, 42, TCP).is_err());
    }
}
