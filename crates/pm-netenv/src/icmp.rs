//! ICMP listener and probe support, port of Go `service/netenv/icmp_listener.go`
//! and the traceroute/timeout part of `service/netenv/location.go`.
//!
//! Go receives ICMP replies through a single-use listener that the firewall
//! feeds with packets; the only raw socket in that file is the echo probe on
//! `ip4:icmp`. Both sides are injected here: [`IcmpPacket`] is a packet handed
//! to the listener, [`IcmpSocket`] sends probe packets and receives replies.
//! [`SystemIcmpSocket`] is the Linux implementation using `nix`; all logic can
//! be tested with fakes without root privileges.

use std::net::{IpAddr, Ipv4Addr, SocketAddrV4};
use std::sync::{Arc, Mutex};
use std::time::Duration;

#[cfg(unix)]
use nix::net::if_::if_nametoindex;
#[cfg(unix)]
use nix::sys::socket::{
    recvfrom, sendto, socket, AddressFamily, MsgFlags, SockFlag, SockProtocol, SockType, SockaddrIn,
};
#[cfg(unix)]
use std::os::fd::AsRawFd;
use tokio::sync::mpsc::{self, Sender};

/// Capacity of the Go listener channel.
pub const ICMP_CHANNEL_CAPACITY: usize = 100;
/// Number of attempts per hop, like Go's `j` loop.
pub const ICMP_PROBE_ATTEMPTS_PER_HOP: usize = 2;
/// Number of sends per attempt, like Go's `sendICMP` loop.
pub const ICMP_SEND_RETRIES: usize = 3;
/// Delay between send retries.
pub const ICMP_SEND_RETRY_DELAY: Duration = Duration::from_millis(30);
/// Maximum hop count, incremented for every non-global reply.
pub const ICMP_INITIAL_MAX_HOPS: usize = 4;

/// Base receive timeout in milliseconds, like Go `currentHop*20+100`.
pub fn recv_timeout(current_hop: usize) -> Duration {
    Duration::from_millis((current_hop * 20 + 100) as u64)
}

/// A packet handed to the ICMP listener.
///
/// Mirrors the `packet.Packet` methods used by Go: `LoadPacketData`,
/// `IsOutbound` and `Info().Dst` / `Info().RemoteIP()`.
pub trait IcmpPacket: Send {
    /// Makes the packet payload available; returns an error if it cannot be
    /// loaded, like Go `LoadPacketData`.
    fn load_packet_data(&mut self) -> Result<(), String>;
    /// Returns whether the packet is outbound.
    fn is_outbound(&self) -> bool;
    /// Returns the destination address, like `pkt.Info().Dst`.
    fn destination(&self) -> IpAddr;
    /// Returns the remote address, like `pkt.Info().RemoteIP()`.
    fn remote_ip(&self) -> IpAddr;
    /// Returns the parsed ICMPv4 layer, if this is one.
    fn icmp_v4(&self) -> Option<&IcmpV4Packet>;
}

/// The parts of an ICMPv4 header the Go traceroute logic inspects.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IcmpV4Packet {
    /// ICMP message type and code combined, like
    /// `layers.ICMPv4.TypeCode.Type()`.
    pub type_code: IcmpV4Type,
    /// Echo identifier, like `layers.ICMPv4.Id`.
    pub id: u16,
    /// Echo sequence number, like `layers.ICMPv4.Seq`.
    pub seq: u16,
    /// Inner packet, for time-exceeded/unreachable messages.
    pub payload: Vec<u8>,
}

/// ICMPv4 message types relevant to the traceroute probe.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IcmpV4Type {
    EchoReply,
    DestinationUnreachable,
    TimeExceeded,
    Other,
}

impl IcmpV4Packet {
    /// Returns the type of the message, ignoring the code.
    pub fn message_type(&self) -> IcmpV4Type {
        self.type_code
    }
}

/// Receives a reply from an injected socket.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IcmpReply {
    /// Address the reply came from.
    pub remote_ip: IpAddr,
    /// Parsed ICMPv4 layer.
    pub packet: IcmpV4Packet,
}

/// Parses the ICMPv4 header of a raw reply, like Go's gopacket layer.
pub fn parse_icmp_v4(data: &[u8]) -> Option<IcmpV4Packet> {
    if data.len() < 8 {
        return None;
    }
    let type_code = match data[0] {
        0 => IcmpV4Type::EchoReply,
        3 => IcmpV4Type::DestinationUnreachable,
        11 => IcmpV4Type::TimeExceeded,
        _ => IcmpV4Type::Other,
    };
    Some(IcmpV4Packet {
        type_code,
        id: u16::from_be_bytes([data[4], data[5]]),
        seq: u16::from_be_bytes([data[6], data[7]]),
        payload: data[8..].to_vec(),
    })
}

/// Reads the echo ID and sequence from the original packet embedded in an
/// ICMP error, like Go's `icmp.ParseMessage` on `Payload[ipv4.HeaderLen:]`.
pub fn embedded_echo_ids(packet: &IcmpV4Packet) -> Option<(u16, u16)> {
    let original = packet.payload.get(20..)?;
    if original.first() != Some(&8) || original.len() < 8 {
        return None;
    }
    Some((
        u16::from_be_bytes([original[4], original[5]]),
        u16::from_be_bytes([original[6], original[7]]),
    ))
}

/// Sends ICMP probes and receives replies, injected so tests need no root.
pub trait IcmpSocket: Send + Sync {
    /// Sets the IPv4 TTL of outgoing probe packets.
    fn set_ttl(&self, ttl: u8) -> Result<(), String>;
    /// Sends one probe packet to the target.
    fn send_to(&self, data: &[u8], target: IpAddr) -> Result<(), String>;
    /// Receives a reply within `timeout`, or returns `None` on timeout.
    fn recv_reply(&self, timeout: Duration) -> Result<Option<IcmpReply>, String>;
}

/// `ip4:icmp` datagram socket, the Unix system implementation.
///
/// Opening a datagram ICMP socket may require privileges depending on the
/// `net.ipv4.ping_group_range` sysctl; failures are returned to the caller
/// instead of panicking.
#[cfg(unix)]
pub struct SystemIcmpSocket {
    fd: std::os::fd::OwnedFd,
}

#[cfg(unix)]
impl SystemIcmpSocket {
    /// Opens a non-blocking ICMP datagram socket.
    pub fn new() -> Result<SystemIcmpSocket, String> {
        let fd = socket(
            AddressFamily::Inet,
            SockType::Datagram,
            SockFlag::SOCK_NONBLOCK,
            SockProtocol::Icmp,
        )
        .map_err(|error| format!("failed to open icmp conn: {error}"))?;
        Ok(SystemIcmpSocket { fd })
    }

    /// Returns the raw file descriptor for tests and diagnostics.
    pub fn as_raw_fd(&self) -> std::os::fd::RawFd {
        self.fd.as_raw_fd()
    }
}

#[cfg(unix)]
impl IcmpSocket for SystemIcmpSocket {
    fn set_ttl(&self, ttl: u8) -> Result<(), String> {
        nix::sys::socket::setsockopt(
            &self.fd,
            nix::sys::socket::sockopt::Ipv4Ttl,
            &i32::from(ttl),
        )
        .map_err(|error| format!("failed to set icmp packet TTL: {error}"))
    }

    fn send_to(&self, data: &[u8], target: IpAddr) -> Result<(), String> {
        let IpAddr::V4(target) = target else {
            return Err("icmp probe requires an IPv4 target".to_string());
        };
        let octets = target.octets();
        let address = SockaddrIn::new(octets[0], octets[1], octets[2], octets[3], 0);
        sendto(self.fd.as_raw_fd(), data, &address, MsgFlags::empty())
            .map_err(|error| format!("failed to send icmp packet: {error}"))?;
        Ok(())
    }

    fn recv_reply(&self, timeout: Duration) -> Result<Option<IcmpReply>, String> {
        let deadline = std::time::Instant::now() + timeout;
        loop {
            let mut buffer = [0u8; 1500];
            match recvfrom::<SockaddrIn>(self.fd.as_raw_fd(), &mut buffer) {
                Ok((len, address)) => {
                    let remote = address
                        .map(|address| IpAddr::V4(address.ip()))
                        .unwrap_or(IpAddr::V4(Ipv4Addr::UNSPECIFIED));
                    let Some(packet) = parse_icmp_v4(&buffer[..len]) else {
                        continue;
                    };
                    return Ok(Some(IcmpReply {
                        remote_ip: remote,
                        packet,
                    }));
                }
                Err(nix::errno::Errno::EAGAIN) => {
                    if std::time::Instant::now() >= deadline {
                        return Ok(None);
                    }
                    std::thread::sleep(Duration::from_millis(5));
                }
                Err(error) => return Err(format!("failed to receive icmp packet: {error}")),
            }
        }
    }
}

/// Inert ICMP probe socket for non-Unix platforms (notably Windows).
///
/// Go's Windows path uses `SIO_RCVALL` on a raw socket to receive ICMP errors;
/// that requires platform-specific unsafe setup and is not ported. The probe
/// logic and listener in this module are still fully usable with injected
/// [`IcmpSocket`] implementations.
#[cfg(not(unix))]
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemIcmpSocket;

#[cfg(not(unix))]
impl SystemIcmpSocket {
    /// Always fails off Unix: no raw ICMP socket is available.
    pub fn new() -> Result<SystemIcmpSocket, String> {
        Err("ICMP system sockets are only available on Unix".to_string())
    }
}

#[cfg(not(unix))]
impl IcmpSocket for SystemIcmpSocket {
    fn set_ttl(&self, _ttl: u8) -> Result<(), String> {
        Err("ICMP system sockets are only available on Unix".to_string())
    }

    fn send_to(&self, _data: &[u8], _target: IpAddr) -> Result<(), String> {
        Err("ICMP system sockets are only available on Unix".to_string())
    }

    fn recv_reply(&self, _timeout: Duration) -> Result<Option<IcmpReply>, String> {
        Err("ICMP system sockets are only available on Unix".to_string())
    }
}

/// A handle that releases the single-use ICMP listener when dropped, like the
/// `done` function returned by Go `ListenToICMP`.
pub struct IcmpListenerGuard {
    owner: Arc<IcmpListener>,
    state: Arc<Mutex<ListenerState>>,
    active: bool,
}

impl std::fmt::Debug for IcmpListenerGuard {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("IcmpListenerGuard")
            .field("active", &self.active)
            .finish()
    }
}

impl Drop for IcmpListenerGuard {
    fn drop(&mut self) {
        if self.active {
            release_listener(&self.state);
            self.owner.release();
            self.active = false;
        }
    }
}

#[derive(Default)]
struct ListenerState {
    enabled: bool,
    target_ip: Option<IpAddr>,
    sender: Option<Sender<Box<dyn IcmpPacket>>>,
}

/// The packet channel returned to a listener's caller.
pub type PacketReceiver = mpsc::Receiver<Box<dyn IcmpPacket>>;

/// Single-use ICMP listener, like the package-level variables in Go.
///
/// Calls to [`IcmpListener::listen`] wait for the previous listener to be done;
/// [`IcmpListener::try_listen`] returns `None` while one is active.
pub struct IcmpListener {
    busy: Mutex<bool>,
    ready: std::sync::Condvar,
    state: Mutex<Option<Arc<Mutex<ListenerState>>>>,
}

impl Default for IcmpListener {
    fn default() -> Self {
        IcmpListener {
            busy: Mutex::new(false),
            ready: std::sync::Condvar::new(),
            state: Mutex::new(None),
        }
    }
}

impl IcmpListener {
    /// Creates an idle listener registry.
    pub fn new() -> IcmpListener {
        IcmpListener::default()
    }

    /// Starts listening to ICMP packets for `target_ip`, like Go
    /// `ListenToICMP`.
    ///
    /// Only one listener may be active. The returned receiver gets all
    /// submitted packets; filtering is the caller's responsibility. The guard
    /// must be kept alive for the duration of the listener.
    pub fn listen(self: &Arc<Self>, target_ip: IpAddr) -> (PacketReceiver, IcmpListenerGuard) {
        self.acquire();
        self.install(target_ip)
    }

    /// Starts listening only if no other listener is active.
    pub fn try_listen(
        self: &Arc<Self>,
        target_ip: IpAddr,
    ) -> Option<(PacketReceiver, IcmpListenerGuard)> {
        {
            let mut busy = self.busy.lock().unwrap_or_else(|error| error.into_inner());
            if *busy {
                return None;
            }
            *busy = true;
        }
        Some(self.install(target_ip))
    }

    fn acquire(&self) {
        let mut busy = self.busy.lock().unwrap_or_else(|error| error.into_inner());
        while *busy {
            busy = self
                .ready
                .wait(busy)
                .unwrap_or_else(|error| error.into_inner());
        }
        *busy = true;
    }

    fn release(&self) {
        let mut busy = self.busy.lock().unwrap_or_else(|error| error.into_inner());
        *busy = false;
        self.ready.notify_all();
    }

    fn install(self: &Arc<Self>, target_ip: IpAddr) -> (PacketReceiver, IcmpListenerGuard) {
        let (sender, receiver) = mpsc::channel(ICMP_CHANNEL_CAPACITY);
        let state = Arc::new(Mutex::new(ListenerState {
            enabled: true,
            target_ip: Some(target_ip),
            sender: Some(sender),
        }));
        let mut registry = self.state.lock().unwrap_or_else(|error| error.into_inner());
        *registry = Some(Arc::clone(&state));
        (
            receiver,
            IcmpListenerGuard {
                owner: Arc::clone(self),
                state,
                active: true,
            },
        )
    }

    /// Submits a packet, like Go `SubmitPacketToICMPListener`.
    ///
    /// Returns whether the packet was accepted. A full channel logs a warning
    /// and still counts as submitted, like Go.
    pub fn submit_packet(&self, packet: Box<dyn IcmpPacket>) -> bool {
        // Hot path: no listener means nothing to do.
        let active = self
            .state
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .clone();
        let Some(active) = active else {
            return false;
        };
        if !active
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .enabled
        {
            return false;
        }
        self.submit_packet_slow(packet, &active)
    }

    fn submit_packet_slow(
        &self,
        mut packet: Box<dyn IcmpPacket>,
        active: &Mutex<ListenerState>,
    ) -> bool {
        if let Err(error) = packet.load_packet_data() {
            tracing::warn!("netenv: failed to get payload for ICMP listener: {error}");
            return false;
        }

        let state = active.lock().unwrap_or_else(|error| error.into_inner());
        if !state.enabled {
            return false;
        }
        if packet.is_outbound() {
            if let Some(target) = state.target_ip {
                if packet.destination() != target {
                    return false;
                }
            }
        }

        let Some(sender) = state.sender.as_ref() else {
            return false;
        };
        match sender.try_send(packet) {
            Ok(()) => true,
            Err(mpsc::error::TrySendError::Full(_)) => {
                tracing::warn!(
                    "netenv: failed to send packet payload to ICMP listener: channel full"
                );
                true
            }
            Err(mpsc::error::TrySendError::Closed(_)) => false,
        }
    }
}

fn release_listener(state: &Arc<Mutex<ListenerState>>) {
    let mut state = state.lock().unwrap_or_else(|error| error.into_inner());
    state.enabled = false;
    state.target_ip = None;
    state.sender = None;
}

/// Outcome of waiting for an ICMP reply, like Go `recvICMP`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RecvOutcome {
    /// A reply from the given address.
    Reply {
        remote_ip: IpAddr,
        packet: IcmpV4Packet,
    },
    /// No reply arrived before the per-hop timeout.
    Timeout,
    /// No usable ICMP layer was present; keep waiting.
    Skipped,
}

/// Waits for an ICMP reply, like Go `recvICMP`: only inbound packets with an
/// ICMPv4 layer are accepted and the timeout grows with the hop count.
pub async fn recv_icmp(current_hop: usize, receiver: &mut PacketReceiver) -> RecvOutcome {
    match tokio::time::timeout(recv_timeout(current_hop), receiver.recv()).await {
        Ok(Some(packet)) => {
            if packet.is_outbound() {
                return RecvOutcome::Skipped;
            }
            match packet.icmp_v4() {
                Some(layer) => RecvOutcome::Reply {
                    remote_ip: packet.remote_ip(),
                    packet: layer.clone(),
                },
                None => RecvOutcome::Skipped,
            }
        }
        Ok(None) => RecvOutcome::Skipped,
        Err(_) => RecvOutcome::Timeout,
    }
}

/// Result of a probe sequence, like the errors returned by Go
/// `getLocationFromTraceroute`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProbeOutcome {
    /// A global router answered a time-exceeded message.
    FoundRouter(IpAddr),
    /// The target answered directly or reported destination unreachable.
    Failed(String),
    /// No actionable reply arrived.
    NoReply,
}

/// Runs one traceroute-style probe sequence, like Go
/// `getLocationFromTraceroute`.
///
/// Two attempts are made per hop; each one sends the echo packet up to three
/// times and waits with the Go per-hop timeout. `classify` reports whether a
/// responding router IP is global; the host-side logic supplies it so the
/// probe stays independent of the IP scope table.
pub fn run_probe(
    socket: &dyn IcmpSocket,
    target: IpAddr,
    id: u16,
    classify: impl Fn(IpAddr) -> bool,
) -> Result<ProbeOutcome, String> {
    let mut sequence = 0u16;
    let mut max_hops = ICMP_INITIAL_MAX_HOPS;
    let mut hop = 1usize;

    while hop <= max_hops {
        let min_sequence = sequence.wrapping_add(1);
        for _ in 0..ICMP_PROBE_ATTEMPTS_PER_HOP {
            sequence = sequence.wrapping_add(1);
            let mut sent = false;
            for _ in 0..ICMP_SEND_RETRIES {
                let packet = build_echo(id, sequence);
                socket.set_ttl(hop as u8)?;
                if socket.send_to(&packet, target).is_ok() {
                    sent = true;
                    break;
                }
                std::thread::sleep(ICMP_SEND_RETRY_DELAY);
            }
            if !sent {
                return Err("failed to send icmp packet".to_string());
            }

            loop {
                let Some(reply) = socket.recv_reply(recv_timeout(hop))? else {
                    // Timed out: try the next attempt for this hop.
                    break;
                };
                let (reply_id, reply_seq) = match reply.packet.message_type() {
                    IcmpV4Type::EchoReply => (reply.packet.id, reply.packet.seq),
                    IcmpV4Type::DestinationUnreachable | IcmpV4Type::TimeExceeded => {
                        let Some(ids) = embedded_echo_ids(&reply.packet) else {
                            continue;
                        };
                        ids
                    }
                    IcmpV4Type::Other => continue,
                };
                if reply_id != id || reply_seq < min_sequence {
                    continue;
                }
                match reply.packet.message_type() {
                    IcmpV4Type::EchoReply => {
                        return Ok(ProbeOutcome::Failed(
                            "received final echo reply without time exceeded messages".to_string(),
                        ));
                    }
                    IcmpV4Type::DestinationUnreachable => {
                        return Ok(ProbeOutcome::Failed("destination unreachable".to_string()));
                    }
                    IcmpV4Type::TimeExceeded => {
                        if classify(reply.remote_ip) {
                            return Ok(ProbeOutcome::FoundRouter(reply.remote_ip));
                        }
                        max_hops += 1;
                        break;
                    }
                    IcmpV4Type::Other => continue,
                }
            }
        }
        hop += 1;
    }

    Ok(ProbeOutcome::NoReply)
}

/// Builds an ICMP echo request, like Go `icmp.Message.Marshal`.
pub fn build_echo(id: u16, sequence: u16) -> Vec<u8> {
    // ICMPv4 echo request: type, code, checksum (filled by the kernel), ID,
    // sequence. The Go `icmp.Echo` body has no data in the traceroute probe.
    let mut packet = vec![8, 0, 0, 0];
    packet.extend_from_slice(&id.to_be_bytes());
    packet.extend_from_slice(&sequence.to_be_bytes());
    packet
}

/// Socket address of the target used by Go's raw ICMP probe.
pub fn target_socket_address(target: Ipv4Addr) -> SocketAddrV4 {
    SocketAddrV4::new(target, 0)
}

/// Resolves an interface name to its index, for diagnostics.
#[cfg(unix)]
pub fn interface_index(name: &str) -> Option<u32> {
    if_nametoindex(name).ok()
}

/// Interface-name lookup is not available off Unix.
#[cfg(not(unix))]
pub fn interface_index(_name: &str) -> Option<u32> {
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn timeout_matches_go() {
        assert_eq!(recv_timeout(1), Duration::from_millis(120));
        assert_eq!(recv_timeout(4), Duration::from_millis(180));
    }

    #[test]
    fn echo_packet_layout_matches_go() {
        let packet = build_echo(0x1234, 0x5678);
        assert_eq!(packet.len(), 8);
        assert_eq!(packet[..4], [8, 0, 0, 0]);
        assert_eq!(&packet[4..6], &0x1234u16.to_be_bytes());
        assert_eq!(&packet[6..8], &0x5678u16.to_be_bytes());
        assert_eq!(target_socket_address(Ipv4Addr::new(1, 2, 3, 4)).port(), 0);
    }
}
