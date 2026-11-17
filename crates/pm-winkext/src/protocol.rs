//! Wire protocol shared with the Portmaster Windows kernel extension.
//!
//! Everything in this module is platform independent pure data: it describes
//! the ioctl codes, the packed control structures and the bidirectional
//! command/info byte streams used by the two Go implementations that this
//! crate ports:
//!
//! * `service/firewall/interception/windowskext` (v1, raw ioctls), and
//! * `service/firewall/interception/windowskext2` together with
//!   `windows_kext/kextinterface` (v2, command/info byte stream).
//!
//! Field order, widths and constants mirror the Go/`#[repr(C, packed)]`
//! definitions exactly. Keeping the definitions here (instead of inside the
//! Windows-only FFI modules) lets the Linux build exercise the parsers and
//! serialization in unit tests without touching a driver.

use crate::Error;

// ---------------------------------------------------------------------------
// v1: ioctl control codes (Go: windowskext/syscall.go).
// ---------------------------------------------------------------------------

/// `SIOCTL_TYPE` from the Go driver interface.
pub const SIOCTL_TYPE: u32 = 40_000;

/// METHOD_BUFFERED — the only transfer method the kext uses.
pub const METHOD_BUFFERED: u32 = 0;

/// Access mask used for every kext ioctl: `FILE_READ_DATA | FILE_WRITE_DATA`.
pub const IOCTL_ACCESS: u32 = 3;

/// Go `ctlCode`: `(device_type << 16) | (access << 14) | (function << 2) | method`.
#[must_use]
pub const fn ctl_code(device_type: u32, function: u32, method: u32, access: u32) -> u32 {
    (device_type << 16) | (access << 14) | (function << 2) | method
}

/// `IOCTL_VERSION` — query the 4-byte driver version.
pub const IOCTL_VERSION: u32 = ctl_code(SIOCTL_TYPE, 0x800, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_SHUTDOWN_REQUEST` — tell the driver to prepare for shutdown.
pub const IOCTL_SHUTDOWN_REQUEST: u32 =
    ctl_code(SIOCTL_TYPE, 0x801, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_RECV_VERDICT_REQ` — blocking read of the next verdict request.
pub const IOCTL_RECV_VERDICT_REQ: u32 =
    ctl_code(SIOCTL_TYPE, 0x802, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_SET_VERDICT` — apply a verdict to a packet.
pub const IOCTL_SET_VERDICT: u32 = ctl_code(SIOCTL_TYPE, 0x803, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_GET_PAYLOAD` — read the raw packet bytes for a packet ID.
pub const IOCTL_GET_PAYLOAD: u32 = ctl_code(SIOCTL_TYPE, 0x804, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_CLEAR_CACHE` — drop all cached connection verdicts.
pub const IOCTL_CLEAR_CACHE: u32 = ctl_code(SIOCTL_TYPE, 0x805, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_UPDATE_VERDICT` — update the verdict of an existing connection.
pub const IOCTL_UPDATE_VERDICT: u32 =
    ctl_code(SIOCTL_TYPE, 0x806, METHOD_BUFFERED, IOCTL_ACCESS);
/// `IOCTL_GET_CONNECTIONS_STAT` — read per-connection bandwidth statistics.
pub const IOCTL_GET_CONNECTIONS_STAT: u32 =
    ctl_code(SIOCTL_TYPE, 0x807, METHOD_BUFFERED, IOCTL_ACCESS);

// ---------------------------------------------------------------------------
// v1: control structures (Go: windowskext/handler.go).
// ---------------------------------------------------------------------------

/// `VerdictRequestFlagFastTrackPermitted` — packet was already permitted by
/// the kernel extension; the verdict request is informational only.
pub const VERDICT_REQUEST_FLAG_FAST_TRACK_PERMITTED: u8 = 1;
/// `VerdictRequestFlagSocketAuth` — ALE-layer request without packet data.
pub const VERDICT_REQUEST_FLAG_SOCKET_AUTH: u8 = 2;
/// `VerdictRequestFlagExpectSocketAuth` — next request is a socket-auth info.
pub const VERDICT_REQUEST_FLAG_EXPECT_SOCKET_AUTH: u8 = 4;

/// Name of the kernel driver service (`driverName` in Go).
pub const DRIVER_NAME: &str = "PortmasterKext";
/// Device path used to open the driver: `\\.\PortmasterKext`.
pub const DRIVER_DEVICE_PATH: &str = r"\\.\PortmasterKext";

/// `VerdictRequest` from the kext. Field order must not change.
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VerdictRequest {
    /// ID from `RegisterPacket`.
    pub id: u32,
    /// Process ID (informational packets only).
    pub pid: u64,
    /// 0 = outbound, >0 = inbound.
    pub direction: u8,
    /// 1 = IPv6, 0 = IPv4.
    pub ip_v6: u8,
    /// IP protocol number.
    pub protocol: u8,
    /// Combination of the `VERDICT_REQUEST_FLAG_*` bits.
    pub flags: u8,
    /// Source address (only `local_ip[0]` is used for IPv4).
    pub local_ip: [u32; 4],
    /// Destination address (only `remote_ip[0]` is used for IPv4).
    pub remote_ip: [u32; 4],
    pub local_port: u16,
    pub remote_port: u16,
    /// compartmentID — unused by Portmaster, present for ABI compatibility.
    pub compartment_id: u32,
    /// interfaceIndex — unused by Portmaster, present for ABI compatibility.
    pub interface_index: u32,
    /// subInterfaceIndex — unused by Portmaster, present for ABI compatibility.
    pub sub_interface_index: u32,
    /// Size of the raw packet that must be fetched with `IOCTL_GET_PAYLOAD`.
    pub packet_size: u32,
}

impl VerdictRequest {
    /// Whether the packet was fast-tracked by the OS integration.
    #[must_use]
    pub fn fast_tracked_by_integration(&self) -> bool {
        self.flags & VERDICT_REQUEST_FLAG_FAST_TRACK_PERMITTED > 0
    }

    /// Whether this is an informational-only (socket auth) request.
    #[must_use]
    pub fn info_only(&self) -> bool {
        self.flags & VERDICT_REQUEST_FLAG_SOCKET_AUTH > 0
    }

    /// Whether the next request is expected to be informational only.
    #[must_use]
    pub fn expect_info(&self) -> bool {
        self.flags & VERDICT_REQUEST_FLAG_EXPECT_SOCKET_AUTH > 0
    }

    /// Serializes the structure the way Go's `asByteArray` exposes it to the
    /// ioctl: little-endian field order with no additional padding.
    #[must_use]
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(size_of::<Self>());
        out.extend_from_slice(&self.id.to_le_bytes());
        // 4 bytes of alignment padding before the u64 `pid`, matching the
        // Go struct memory layout that the driver reads directly.
        out.extend_from_slice(&[0, 0, 0, 0]);
        out.extend_from_slice(&self.pid.to_le_bytes());
        out.push(self.direction);
        out.push(self.ip_v6);
        out.push(self.protocol);
        out.push(self.flags);
        for ip in self.local_ip {
            out.extend_from_slice(&ip.to_le_bytes());
        }
        for ip in self.remote_ip {
            out.extend_from_slice(&ip.to_le_bytes());
        }
        out.extend_from_slice(&self.local_port.to_le_bytes());
        out.extend_from_slice(&self.remote_port.to_le_bytes());
        out.extend_from_slice(&self.compartment_id.to_le_bytes());
        out.extend_from_slice(&self.interface_index.to_le_bytes());
        out.extend_from_slice(&self.sub_interface_index.to_le_bytes());
        out.extend_from_slice(&self.packet_size.to_le_bytes());
        out
    }
}

/// Verdict numbers used by the v1 ioctl interface (Go `network.Verdict`,
/// which is an `int8`).
#[repr(i8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NetworkVerdict {
    Undecided = 0,
    Undeterminable = 1,
    Accept = 2,
    Block = 3,
    Drop = 4,
    RerouteToNameserver = 5,
    RerouteToTunnel = 6,
    Failed = 7,
    RerouteToSplitTun = 8,
}

impl NetworkVerdict {
    /// Returns the wire value (Go casts the verdict directly to `uint8`).
    #[must_use]
    pub const fn as_i8(self) -> i8 {
        self as i8
    }
}

/// `VerdictInfo` — input for `IOCTL_SET_VERDICT`.
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VerdictInfo {
    pub id: u32,
    /// `network.Verdict` is an `int8` in Go.
    pub verdict: i8,
}

impl VerdictInfo {
    /// Bytes sent to the driver.
    #[must_use]
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(size_of::<Self>());
        out.extend_from_slice(&self.id.to_le_bytes());
        out.push(self.verdict as u8);
        out
    }
}

/// `VerdictUpdateInfo` — input for `IOCTL_UPDATE_VERDICT`.
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VerdictUpdateInfo {
    pub local_ip: [u32; 4],
    pub remote_ip: [u32; 4],
    pub local_port: u16,
    pub remote_port: u16,
    /// 1 = IPv6, 0 = IPv4.
    pub ip_v6: u8,
    pub protocol: u8,
    pub verdict: u8,
}

impl VerdictUpdateInfo {
    /// Bytes sent to the driver.
    #[must_use]
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(size_of::<Self>());
        for ip in self.local_ip {
            out.extend_from_slice(&ip.to_le_bytes());
        }
        for ip in self.remote_ip {
            out.extend_from_slice(&ip.to_le_bytes());
        }
        out.extend_from_slice(&self.local_port.to_le_bytes());
        out.extend_from_slice(&self.remote_port.to_le_bytes());
        out.push(self.ip_v6);
        out.push(self.protocol);
        out.push(self.verdict);
        out
    }
}

/// `ConnectionStat` — one entry returned by `IOCTL_GET_CONNECTIONS_STAT`.
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ConnectionStat {
    pub local_ip: [u32; 4],
    pub remote_ip: [u32; 4],
    pub local_port: u16,
    pub remote_port: u16,
    pub received_bytes: u64,
    pub transmitted_bytes: u64,
    /// 1 = IPv6, 0 = IPv4.
    pub ip_v6: u8,
    pub protocol: u8,
}

/// 4-byte version returned by `IOCTL_VERSION` / `ReadVersion`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Version {
    pub major: u8,
    pub minor: u8,
    pub revision: u8,
    pub build: u8,
}

impl Version {
    /// Parses the little-endian byte order used by the driver.
    #[must_use]
    pub fn from_bytes(bytes: [u8; 4]) -> Self {
        Self {
            major: bytes[0],
            minor: bytes[1],
            revision: bytes[2],
            build: bytes[3],
        }
    }
}

impl core::fmt::Display for Version {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(f, "{}.{}.{}.{}", self.major, self.minor, self.revision, self.build)
    }
}

/// Converts a v1 `[u32; 4]` address array to bytes (big-endian per word, as
/// Go's `convertArrayToIP` with `binary.BigEndian.PutUint32` does).
#[must_use]
pub fn ip_array_to_bytes(input: [u32; 4], ipv6: bool) -> Vec<u8> {
    let mut out = Vec::with_capacity(if ipv6 { 16 } else { 4 });
    let words = if ipv6 { 4 } else { 1 };
    for word in input.iter().take(words) {
        out.extend_from_slice(&word.to_be_bytes());
    }
    out
}

// ---------------------------------------------------------------------------
// v2: command stream (Go: windows_kext/kextinterface/command.go).
// ---------------------------------------------------------------------------

/// Command IDs written to the kext (single byte, followed by optional data).
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Command {
    Shutdown = 0,
    Verdict = 1,
    UpdateV4 = 2,
    UpdateV6 = 3,
    ClearCache = 4,
    GetLogs = 5,
    BandwidthStats = 6,
    PrintMemoryStats = 7,
    CleanEndedConnections = 8,
}

impl Command {
    /// The single command byte.
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        self as u8
    }
}

/// Verdict IDs understood by the v2 kext (Go `KextVerdict`). Keep in sync with
/// `windows_kext/protocol/src/command.rs` on the driver side.
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum KextVerdict {
    Undecided = 0,
    Undeterminable = 1,
    Accept = 2,
    PermanentAccept = 3,
    Block = 4,
    PermanentBlock = 5,
    Drop = 6,
    PermanentDrop = 7,
    RerouteToNameserver = 8,
    RerouteToTunnel = 9,
    Failed = 10,
    RerouteToSplitTun = 11,
}

impl KextVerdict {
    /// The single verdict byte on the wire.
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        self as u8
    }
}

/// Port of Go `getKextVerdictFromConnection`: maps a firewall verdict and its
/// `VerdictPermanent` flag to the verdict ID expected by the kext.
#[must_use]
pub const fn kext_verdict_from(verdict: NetworkVerdict, permanent: bool) -> KextVerdict {
    match verdict {
        NetworkVerdict::Undecided => KextVerdict::Undecided,
        NetworkVerdict::Undeterminable => KextVerdict::Undeterminable,
        NetworkVerdict::Accept => {
            if permanent {
                KextVerdict::PermanentAccept
            } else {
                KextVerdict::Accept
            }
        }
        NetworkVerdict::Block => {
            if permanent {
                KextVerdict::PermanentBlock
            } else {
                KextVerdict::Block
            }
        }
        NetworkVerdict::Drop => {
            if permanent {
                KextVerdict::PermanentDrop
            } else {
                KextVerdict::Drop
            }
        }
        NetworkVerdict::RerouteToNameserver => KextVerdict::RerouteToNameserver,
        NetworkVerdict::RerouteToTunnel => KextVerdict::RerouteToTunnel,
        NetworkVerdict::RerouteToSplitTun => KextVerdict::RerouteToSplitTun,
        NetworkVerdict::Failed => KextVerdict::Failed,
    }
}

/// Serializes a command with no payload (shutdown, clear cache, log request,
/// bandwidth request, memory stats, clean ended connections).
#[must_use]
pub const fn simple_command(command: Command) -> [u8; 1] {
    [command.as_u8()]
}

/// Serializes a `Verdict` command: `[command:u8][id:u64 LE][verdict:u8]`.
#[must_use]
pub fn verdict_command(id: u64, verdict: KextVerdict) -> [u8; 10] {
    let mut out = [0_u8; 10];
    out[0] = Command::Verdict.as_u8();
    out[1..9].copy_from_slice(&id.to_le_bytes());
    out[9] = verdict.as_u8();
    out
}

/// Serializes an `UpdateV4` command. Layout is the Go struct field order with
/// no padding: `[cmd][protocol][local:4][lport:2][remote:4][rport:2][verdict]`.
#[must_use]
pub fn update_v4_command(
    protocol: u8,
    local_ip: [u8; 4],
    local_port: u16,
    remote_ip: [u8; 4],
    remote_port: u16,
    verdict: KextVerdict,
) -> [u8; 15] {
    let mut out = [0_u8; 15];
    out[0] = Command::UpdateV4.as_u8();
    out[1] = protocol;
    out[2..6].copy_from_slice(&local_ip);
    out[6..8].copy_from_slice(&local_port.to_le_bytes());
    out[8..12].copy_from_slice(&remote_ip);
    out[12..14].copy_from_slice(&remote_port.to_le_bytes());
    out[14] = verdict.as_u8();
    out
}

/// Serializes an `UpdateV6` command: `[cmd][protocol][local:16][lport:2]
/// [remote:16][rport:2][verdict]`.
#[must_use]
pub fn update_v6_command(
    protocol: u8,
    local_ip: [u8; 16],
    local_port: u16,
    remote_ip: [u8; 16],
    remote_port: u16,
    verdict: KextVerdict,
) -> [u8; 39] {
    let mut out = [0_u8; 39];
    out[0] = Command::UpdateV6.as_u8();
    out[1] = protocol;
    out[2..18].copy_from_slice(&local_ip);
    out[18..20].copy_from_slice(&local_port.to_le_bytes());
    out[20..36].copy_from_slice(&remote_ip);
    out[36..38].copy_from_slice(&remote_port.to_le_bytes());
    out[38] = verdict.as_u8();
    out
}

// ---------------------------------------------------------------------------
// v2: info stream (Go: windows_kext/kextinterface/info.go).
// ---------------------------------------------------------------------------

/// Info frame type byte.
#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InfoType {
    LogLine = 0,
    ConnectionV4 = 1,
    ConnectionV6 = 2,
    ConnectionEndV4 = 3,
    ConnectionEndV6 = 4,
    BandwidthStatsV4 = 5,
    BandwidthStatsV6 = 6,
}

impl TryFrom<u8> for InfoType {
    type Error = Error;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(Self::LogLine),
            1 => Ok(Self::ConnectionV4),
            2 => Ok(Self::ConnectionV6),
            3 => Ok(Self::ConnectionEndV4),
            4 => Ok(Self::ConnectionEndV6),
            5 => Ok(Self::BandwidthStatsV4),
            6 => Ok(Self::BandwidthStatsV6),
            other => Err(Error::UnknownInfoType(other)),
        }
    }
}

/// Fixed-size prefix of a v4 connection info (`connectionV4Internal`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConnectionV4 {
    pub id: u64,
    pub process_id: u64,
    pub direction: u8,
    pub protocol: u8,
    pub local_ip: [u8; 4],
    pub remote_ip: [u8; 4],
    pub local_port: u16,
    pub remote_port: u16,
    pub payload_layer: u8,
    /// Payload following the fixed-size prefix.
    pub payload: Vec<u8>,
}

/// Fixed-size prefix of a v6 connection info (`connectionV6Internal`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConnectionV6 {
    pub id: u64,
    pub process_id: u64,
    pub direction: u8,
    pub protocol: u8,
    pub local_ip: [u8; 16],
    pub remote_ip: [u8; 16],
    pub local_port: u16,
    pub remote_port: u16,
    pub payload_layer: u8,
    pub payload: Vec<u8>,
}

/// `ConnectionEndV4` info.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ConnectionEndV4 {
    pub process_id: u64,
    pub direction: u8,
    pub protocol: u8,
    pub local_ip: [u8; 4],
    pub remote_ip: [u8; 4],
    pub local_port: u16,
    pub remote_port: u16,
}

/// `ConnectionEndV6` info.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ConnectionEndV6 {
    pub process_id: u64,
    pub direction: u8,
    pub protocol: u8,
    pub local_ip: [u8; 16],
    pub remote_ip: [u8; 16],
    pub local_port: u16,
    pub remote_port: u16,
}

/// `LogLine` info.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LogLine {
    /// Severity as emitted by the kext (matches the Go `log` level bytes).
    pub severity: u8,
    pub line: String,
}

/// One v4 bandwidth statistics entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct BandwidthValueV4 {
    pub local_ip: [u8; 4],
    pub local_port: u16,
    pub remote_ip: [u8; 4],
    pub remote_port: u16,
    pub transmitted_bytes: u64,
    pub received_bytes: u64,
}

/// One v6 bandwidth statistics entry.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct BandwidthValueV6 {
    pub local_ip: [u8; 16],
    pub local_port: u16,
    pub remote_ip: [u8; 16],
    pub remote_port: u16,
    pub transmitted_bytes: u64,
    pub received_bytes: u64,
}

/// Parsed bandwidth statistics array (one info frame carries either v4 or v6
/// values, but the Go `BandwidthStatsArray` struct holds both fields).
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct BandwidthStatsArray {
    pub protocol: u8,
    pub values_v4: Vec<BandwidthValueV4>,
    pub values_v6: Vec<BandwidthValueV6>,
}

/// A parsed info frame from the kext.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Info {
    LogLine(LogLine),
    ConnectionV4(ConnectionV4),
    ConnectionV6(ConnectionV6),
    ConnectionEndV4(ConnectionEndV4),
    ConnectionEndV6(ConnectionEndV6),
    BandwidthStats(BandwidthStatsArray),
}

/// Cursor over one info frame payload. All integers are little-endian, which
/// matches both Go's `binary.LittleEndian` and the Rust driver protocol.
struct Reader<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Reader<'a> {
    fn new(data: &'a [u8]) -> Self {
        Self { data, pos: 0 }
    }

    fn take(&mut self, len: usize) -> Result<&'a [u8], Error> {
        let end = self
            .pos
            .checked_add(len)
            .ok_or(Error::UnexpectedInfoSize)?;
        let slice = self
            .data
            .get(self.pos..end)
            .ok_or(Error::UnexpectedInfoSize)?;
        self.pos = end;
        Ok(slice)
    }

    fn u8(&mut self) -> Result<u8, Error> {
        Ok(self.take(1)?[0])
    }

    fn u16(&mut self) -> Result<u16, Error> {
        let bytes: [u8; 2] = self.take(2)?.try_into().map_err(|_| Error::UnexpectedInfoSize)?;
        Ok(u16::from_le_bytes(bytes))
    }

    fn u32(&mut self) -> Result<u32, Error> {
        let bytes: [u8; 4] = self.take(4)?.try_into().map_err(|_| Error::UnexpectedInfoSize)?;
        Ok(u32::from_le_bytes(bytes))
    }

    fn u64(&mut self) -> Result<u64, Error> {
        let bytes: [u8; 8] = self.take(8)?.try_into().map_err(|_| Error::UnexpectedInfoSize)?;
        Ok(u64::from_le_bytes(bytes))
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], Error> {
        self.take(N)?
            .try_into()
            .map_err(|_| Error::UnexpectedInfoSize)
    }
}

/// Maximum accepted info payload. The v2 driver caps its log lines at 500
/// bytes; this guard also prevents a corrupt size field from allocating GiBs.
const MAX_INFO_SIZE: usize = 1024 * 1024;

/// Parses one complete info frame from `buf`.
///
/// Returns the parsed info together with the number of bytes consumed
/// (5-byte header + payload), so callers can process multiple frames that
/// arrived in a single driver read. This is the safe, platform-independent
/// equivalent of Go's `RecvInfo`/`readHelper`.
pub fn parse_info(buf: &[u8]) -> Result<(Info, usize), Error> {
    let header: [u8; 5] = buf
        .get(..5)
        .ok_or(Error::UnexpectedRead("short info header".into()))?
        .try_into()
        .map_err(|_| Error::UnexpectedRead("short info header".into()))?;

    let info_type = InfoType::try_from(header[0])?;
    let size = u32::from_le_bytes([header[1], header[2], header[3], header[4]]) as usize;
    if size > MAX_INFO_SIZE {
        return Err(Error::UnexpectedInfoSize);
    }
    let payload = buf
        .get(5..5 + size)
        .ok_or(Error::UnexpectedInfoSize)?;

    let mut reader = Reader::new(payload);
    let info = match info_type {
        InfoType::ConnectionV4 => {
            let id = reader.u64()?;
            let process_id = reader.u64()?;
            let direction = reader.u8()?;
            let protocol = reader.u8()?;
            let local_ip = reader.array()?;
            let remote_ip = reader.array()?;
            let local_port = reader.u16()?;
            let remote_port = reader.u16()?;
            let payload_layer = reader.u8()?;
            let payload_size = reader.u32()? as usize;
            let payload_bytes = reader.take(payload_size)?.to_vec();
            Info::ConnectionV4(ConnectionV4 {
                id,
                process_id,
                direction,
                protocol,
                local_ip,
                remote_ip,
                local_port,
                remote_port,
                payload_layer,
                payload: payload_bytes,
            })
        }
        InfoType::ConnectionV6 => {
            let id = reader.u64()?;
            let process_id = reader.u64()?;
            let direction = reader.u8()?;
            let protocol = reader.u8()?;
            let local_ip = reader.array()?;
            let remote_ip = reader.array()?;
            let local_port = reader.u16()?;
            let remote_port = reader.u16()?;
            let payload_layer = reader.u8()?;
            let payload_size = reader.u32()? as usize;
            let payload_bytes = reader.take(payload_size)?.to_vec();
            Info::ConnectionV6(ConnectionV6 {
                id,
                process_id,
                direction,
                protocol,
                local_ip,
                remote_ip,
                local_port,
                remote_port,
                payload_layer,
                payload: payload_bytes,
            })
        }
        InfoType::ConnectionEndV4 => Info::ConnectionEndV4(ConnectionEndV4 {
            process_id: reader.u64()?,
            direction: reader.u8()?,
            protocol: reader.u8()?,
            local_ip: reader.array()?,
            remote_ip: reader.array()?,
            local_port: reader.u16()?,
            remote_port: reader.u16()?,
        }),
        InfoType::ConnectionEndV6 => Info::ConnectionEndV6(ConnectionEndV6 {
            process_id: reader.u64()?,
            direction: reader.u8()?,
            protocol: reader.u8()?,
            local_ip: reader.array()?,
            remote_ip: reader.array()?,
            local_port: reader.u16()?,
            remote_port: reader.u16()?,
        }),
        InfoType::LogLine => {
            let severity = reader.u8()?;
            let line = String::from_utf8_lossy(reader.take(payload.len() - reader.pos)?).into_owned();
            Info::LogLine(LogLine { severity, line })
        }
        InfoType::BandwidthStatsV4 | InfoType::BandwidthStatsV6 => {
            let protocol = reader.u8()?;
            let count = reader.u32()? as usize;
            // Guard against an absurd element count with a corrupt size.
            if count > MAX_INFO_SIZE / 16 {
                return Err(Error::UnexpectedInfoSize);
            }
            let mut stats = BandwidthStatsArray {
                protocol,
                ..BandwidthStatsArray::default()
            };
            if info_type == InfoType::BandwidthStatsV4 {
                for _ in 0..count {
                    stats.values_v4.push(BandwidthValueV4 {
                        local_ip: reader.array()?,
                        local_port: reader.u16()?,
                        remote_ip: reader.array()?,
                        remote_port: reader.u16()?,
                        transmitted_bytes: reader.u64()?,
                        received_bytes: reader.u64()?,
                    });
                }
            } else {
                for _ in 0..count {
                    stats.values_v6.push(BandwidthValueV6 {
                        local_ip: reader.array()?,
                        local_port: reader.u16()?,
                        remote_ip: reader.array()?,
                        remote_port: reader.u16()?,
                        transmitted_bytes: reader.u64()?,
                        received_bytes: reader.u64()?,
                    });
                }
            }
            Info::BandwidthStats(stats)
        }
    };

    // Go's readHelper reads the whole command; reject trailing garbage only
    // when it exceeds the announced size (we validate fields against `size`).
    Ok((info, 5 + size))
}
