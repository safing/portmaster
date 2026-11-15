//! Port of Go `service/network/state`: cached socket tables and lookups.
//!
//! The tables are refreshed from [`ProcFs`] at most every 10ms and are used to
//! map a packet to the owning process.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant, SystemTime};

use crate::packet::{IpProtocol, IpVersion, PacketInfo};
use crate::proc::{ProcFs, Stack};
use crate::scope::is_local_multicast;
use crate::socket::{Address, BindInfo, ConnectionInfo, SocketInfo, UNDEFINED_PROCESS_ID};

/// Duration after which an unseen UDP connection counts as ended.
pub const UDP_CONNECTION_TTL: Duration = Duration::from_secs(10 * 60);
/// Maximum time a UDP connection state is kept.
pub const UDP_CONN_STATE_TTL: Duration = Duration::from_secs(72 * 3600);
/// Shortened maximum time when a socket has many states.
pub const UDP_CONN_STATE_SHORTENED_TTL: Duration = Duration::from_secs(3 * 3600);
/// Soft limit of UDP connection states per socket.
pub const AGGRESSIVE_CLEANING_THRESHOLD: usize = 256;

const MIN_DURATION_BETWEEN_TABLE_UPDATES: Duration = Duration::from_millis(10);
const LOOKUP_TRIES: usize = 5;
const FAST_LOOKUP_TRIES: usize = 2;
const CHECK_PID_TRIES: usize = 5;
const CHECK_PID_BASE_WAIT: Duration = Duration::from_millis(5);

/// Errors returned by the state lookups.
#[derive(Debug, thiserror::Error)]
pub enum LookupError {
    #[error("could not find connection in system state tables")]
    ConnectionNotFound,
    #[error("unsupported protocol for finding process")]
    UnsupportedProtocol,
}

/// Result type of the state lookups.
pub type Result<T> = std::result::Result<T, LookupError>;

/// The result of a successful lookup.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LookupResult {
    /// PID of the owning process, or [`UNDEFINED_PROCESS_ID`].
    pub pid: i32,
    /// Whether the packet belongs to an inbound connection.
    pub inbound: bool,
}

/// All system state tables, like Go `state.Info`.
#[derive(Debug, Default)]
pub struct Info {
    pub tcp4_connections: Vec<Arc<ConnectionInfo>>,
    pub tcp4_listeners: Vec<Arc<BindInfo>>,
    pub tcp6_connections: Vec<Arc<ConnectionInfo>>,
    pub tcp6_listeners: Vec<Arc<BindInfo>>,
    pub udp4_binds: Vec<Arc<BindInfo>>,
    pub udp6_binds: Vec<Arc<BindInfo>>,
}

struct TcpTable {
    stack: Stack,
    connections: RwLock<Vec<Arc<ConnectionInfo>>>,
    listeners: RwLock<Vec<Arc<BindInfo>>>,
    last_update: Mutex<Option<SystemTime>>,
    limiter: Mutex<Option<Instant>>,
}

impl TcpTable {
    fn new(stack: Stack) -> TcpTable {
        TcpTable {
            stack,
            connections: RwLock::new(Vec::new()),
            listeners: RwLock::new(Vec::new()),
            last_update: Mutex::new(None),
            limiter: Mutex::new(None),
        }
    }

    fn current(&self) -> (Vec<Arc<ConnectionInfo>>, Vec<Arc<BindInfo>>) {
        (
            self.connections.read().expect("lock poisoned").clone(),
            self.listeners.read().expect("lock poisoned").clone(),
        )
    }

    fn up_to_date(&self, packet: &PacketInfo) -> bool {
        self.last_update
            .lock()
            .expect("lock poisoned")
            .is_some_and(|updated| updated >= packet.seen_at)
    }

    fn update(&self, procfs: &ProcFs) -> (Vec<Arc<ConnectionInfo>>, Vec<Arc<BindInfo>>) {
        {
            let mut limiter = self.limiter.lock().expect("lock poisoned");
            let should_fetch =
                limiter.is_none_or(|last| last.elapsed() >= MIN_DURATION_BETWEEN_TABLE_UPDATES);
            if should_fetch {
                *limiter = Some(Instant::now());
            }
            drop(limiter);

            if should_fetch {
                match procfs.table(self.stack) {
                    Ok((connections, listeners)) => {
                        *self.connections.write().expect("lock poisoned") =
                            connections.into_iter().map(Arc::new).collect();
                        *self.listeners.write().expect("lock poisoned") =
                            listeners.into_iter().map(Arc::new).collect();
                        *self.last_update.lock().expect("lock poisoned") = Some(SystemTime::now());
                    }
                    Err(error) => {
                        tracing::warn!(stack = ?self.stack, %error, "state: failed to get socket table");
                    }
                }
            }
        }
        self.current()
    }
}

#[derive(Debug, Clone, Copy)]
struct UdpState {
    inbound: bool,
    last_seen: SystemTime,
}

/// Connection states of one local bind: remote address to state.
type UdpStates = HashMap<(IpAddr, u16), HashMap<(IpAddr, u16), UdpState>>;

struct UdpTable {
    stack: Stack,
    binds: RwLock<Vec<Arc<BindInfo>>>,
    last_update: Mutex<Option<SystemTime>>,
    limiter: Mutex<Option<Instant>>,
    states: Mutex<UdpStates>,
}

impl UdpTable {
    fn new(stack: Stack) -> UdpTable {
        UdpTable {
            stack,
            binds: RwLock::new(Vec::new()),
            last_update: Mutex::new(None),
            limiter: Mutex::new(None),
            states: Mutex::new(HashMap::new()),
        }
    }

    fn current(&self) -> Vec<Arc<BindInfo>> {
        self.binds.read().expect("lock poisoned").clone()
    }

    fn up_to_date(&self, packet: &PacketInfo) -> bool {
        self.last_update
            .lock()
            .expect("lock poisoned")
            .is_some_and(|updated| updated >= packet.seen_at)
    }

    fn update(&self, procfs: &ProcFs) -> Vec<Arc<BindInfo>> {
        {
            let mut limiter = self.limiter.lock().expect("lock poisoned");
            let should_fetch =
                limiter.is_none_or(|last| last.elapsed() >= MIN_DURATION_BETWEEN_TABLE_UPDATES);
            if should_fetch {
                *limiter = Some(Instant::now());
            }
            drop(limiter);

            if should_fetch {
                match procfs.table(self.stack) {
                    Ok((_, binds)) => {
                        *self.binds.write().expect("lock poisoned") =
                            binds.into_iter().map(Arc::new).collect();
                        *self.last_update.lock().expect("lock poisoned") = Some(SystemTime::now());
                    }
                    Err(error) => {
                        tracing::warn!(stack = ?self.stack, %error, "state: failed to get socket table");
                    }
                }
            }
        }
        self.current()
    }

    fn get_conn_state(&self, bind: &BindInfo, remote: Address) -> Option<UdpState> {
        self.states
            .lock()
            .expect("lock poisoned")
            .get(&(bind.local.ip, bind.local.port))
            .and_then(|remotes| remotes.get(&(remote.ip, remote.port)))
            .copied()
    }

    fn get_direction(&self, bind: &BindInfo, packet: &PacketInfo) -> bool {
        let mut states = self.states.lock().expect("lock poisoned");
        let remotes = states.entry((bind.local.ip, bind.local.port)).or_default();
        let remote = (packet.remote_ip(), packet.remote_port());
        match remotes.get_mut(&remote) {
            Some(state) => {
                state.last_seen = SystemTime::now();
                state.inbound
            }
            None => {
                remotes.insert(
                    remote,
                    UdpState {
                        inbound: packet.inbound,
                        last_seen: SystemTime::now(),
                    },
                );
                packet.inbound
            }
        }
    }

    fn clean_states(&self, now: SystemTime) {
        let threshold = now - UDP_CONN_STATE_TTL;
        let short_threshold = now - UDP_CONN_STATE_SHORTENED_TTL;

        let active_binds: Vec<(IpAddr, u16)> = self
            .binds
            .read()
            .expect("lock poisoned")
            .iter()
            .map(|bind| (bind.local.ip, bind.local.port))
            .collect();

        let mut states = self.states.lock().expect("lock poisoned");
        states.retain(|local, remotes| {
            if !active_binds.contains(local) {
                return false;
            }
            remotes.retain(|_, state| state.last_seen >= threshold);
            if remotes.len() > AGGRESSIVE_CLEANING_THRESHOLD {
                remotes.retain(|_, state| state.last_seen >= short_threshold);
            }
            true
        });
    }
}

/// The cached system state tables and the lookup API.
pub struct NetState {
    procfs: ProcFs,
    tcp4: TcpTable,
    tcp6: TcpTable,
    udp4: UdpTable,
    udp6: UdpTable,
    tcp_dual_stack: bool,
    udp_dual_stack: bool,
}

impl NetState {
    /// Creates a state cache for the given proc file system.
    pub fn new(procfs: ProcFs) -> NetState {
        NetState {
            procfs,
            tcp4: TcpTable::new(Stack::Tcp4),
            tcp6: TcpTable::new(Stack::Tcp6),
            udp4: UdpTable::new(Stack::Udp4),
            udp6: UdpTable::new(Stack::Udp6),
            tcp_dual_stack: false,
            udp_dual_stack: false,
        }
    }

    /// Adds the IPv6 tables to the IPv4 lookups as a dual stack.
    pub fn enable_tcp_dual_stack(&mut self) {
        self.tcp_dual_stack = true;
    }

    /// Adds the IPv6 UDP table to the IPv4 lookups as a dual stack.
    pub fn enable_udp_dual_stack(&mut self) {
        self.udp_dual_stack = true;
    }

    pub fn procfs(&self) -> &ProcFs {
        &self.procfs
    }

    /// Returns the current tables, like Go `state.GetInfo`.
    pub fn info(&self, ipv6: bool) -> Info {
        let (tcp4_connections, tcp4_listeners) = self.tcp4.update(&self.procfs);
        let udp4_binds = self.udp4.update(&self.procfs);
        let mut info = Info {
            tcp4_connections,
            tcp4_listeners,
            udp4_binds,
            ..Info::default()
        };
        if ipv6 {
            let (tcp6_connections, tcp6_listeners) = self.tcp6.update(&self.procfs);
            info.tcp6_connections = tcp6_connections;
            info.tcp6_listeners = tcp6_listeners;
            info.udp6_binds = self.udp6.update(&self.procfs);
        }
        info
    }

    /// Looks up the owning process of the connection a packet belongs to.
    pub fn lookup(&self, packet: &PacketInfo, fast: bool) -> Result<LookupResult> {
        match (packet.version, packet.protocol) {
            (IpVersion::V4, IpProtocol::TCP) => {
                self.lookup_tcp(&self.tcp4, &self.tcp6, packet, fast)
            }
            (IpVersion::V6, IpProtocol::TCP) => {
                self.lookup_tcp(&self.tcp6, &self.tcp4, packet, fast)
            }
            (IpVersion::V4, IpProtocol::UDP) => {
                self.lookup_udp(&self.udp4, &self.udp6, packet, fast)
            }
            (IpVersion::V6, IpProtocol::UDP) => {
                self.lookup_udp(&self.udp6, &self.udp4, packet, fast)
            }
            _ => Err(LookupError::UnsupportedProtocol),
        }
    }

    /// Checks whether the connection is present in the system state tables.
    pub fn exists(&self, packet: &PacketInfo, now: SystemTime) -> bool {
        match (packet.version, packet.protocol) {
            (IpVersion::V4, IpProtocol::TCP) => self.tcp_exists(&self.tcp4, packet),
            (IpVersion::V6, IpProtocol::TCP) => self.tcp_exists(&self.tcp6, packet),
            (IpVersion::V4, IpProtocol::UDP) => self.udp_exists(&self.udp4, packet, now),
            (IpVersion::V6, IpProtocol::UDP) => self.udp_exists(&self.udp6, packet, now),
            _ => false,
        }
    }

    /// Cleans old UDP connection states.
    pub fn clean_udp_states(&self) {
        let now = SystemTime::now();
        self.udp4.update(&self.procfs);
        self.udp4.clean_states(now);
        self.udp6.update(&self.procfs);
        self.udp6.clean_states(now);
    }

    fn lookup_tcp(
        &self,
        table: &TcpTable,
        dual_table: &TcpTable,
        packet: &PacketInfo,
        fast: bool,
    ) -> Result<LookupResult> {
        for attempt in 1..=LOOKUP_TRIES {
            let (connections, listeners) = if attempt == 1 && table.up_to_date(packet) {
                table.current()
            } else {
                table.update(&self.procfs)
            };
            if let Some((socket, inbound)) = find_tcp_socket(packet, &connections, &listeners) {
                return Ok(LookupResult {
                    pid: self.check_pid(socket.as_ref()),
                    inbound,
                });
            }

            if self.tcp_dual_stack {
                let (connections, listeners) = if attempt == 1 && dual_table.up_to_date(packet) {
                    dual_table.current()
                } else {
                    dual_table.update(&self.procfs)
                };
                if let Some((socket, inbound)) = find_tcp_socket(packet, &connections, &listeners) {
                    return Ok(LookupResult {
                        pid: self.check_pid(socket.as_ref()),
                        inbound,
                    });
                }
            }

            if fast && attempt >= FAST_LOOKUP_TRIES {
                break;
            }
        }

        Err(LookupError::ConnectionNotFound)
    }

    fn lookup_udp(
        &self,
        table: &UdpTable,
        dual_table: &UdpTable,
        packet: &PacketInfo,
        fast: bool,
    ) -> Result<LookupResult> {
        let inbound_multicast = packet.inbound && is_local_multicast(packet.local_ip());

        for attempt in 1..=LOOKUP_TRIES {
            let binds = if attempt == 1 && table.up_to_date(packet) {
                table.current()
            } else {
                table.update(&self.procfs)
            };
            if let Some(bind) = find_udp_socket(packet, &binds, inbound_multicast) {
                return Ok(self.udp_result(table, bind, packet));
            }

            if self.udp_dual_stack {
                let binds = if attempt == 1 && dual_table.up_to_date(packet) {
                    dual_table.current()
                } else {
                    dual_table.update(&self.procfs)
                };
                if let Some(bind) = find_udp_socket(packet, &binds, inbound_multicast) {
                    return Ok(self.udp_result(dual_table, bind, packet));
                }
            }

            if fast && attempt >= FAST_LOOKUP_TRIES {
                break;
            }
        }

        Err(LookupError::ConnectionNotFound)
    }

    fn udp_result(
        &self,
        table: &UdpTable,
        bind: Arc<BindInfo>,
        packet: &PacketInfo,
    ) -> LookupResult {
        // Without a remote port the caller only checks, so the direction of the
        // connection is not changed.
        let inbound = if packet.remote_port() == 0 {
            packet.inbound
        } else {
            table.get_direction(&bind, packet)
        };
        LookupResult {
            pid: self.check_pid(bind.as_ref()),
            inbound,
        }
    }

    fn tcp_exists(&self, table: &TcpTable, packet: &PacketInfo) -> bool {
        let (connections, _) = if table.up_to_date(packet) {
            table.current()
        } else {
            table.update(&self.procfs)
        };

        connections.iter().any(|connection| {
            packet.local_port() == connection.local.port
                && packet.remote_port() == connection.remote.port
                && packet.remote_ip() == connection.remote.ip
                && packet.local_ip() == connection.local.ip
        })
    }

    fn udp_exists(&self, table: &UdpTable, packet: &PacketInfo, now: SystemTime) -> bool {
        let binds = if table.up_to_date(packet) {
            table.current()
        } else {
            table.update(&self.procfs)
        };

        let threshold = now - UDP_CONNECTION_TTL;
        for bind in &binds {
            if packet.local_port() == bind.local.port
                && (bind.listens_any || packet.local_ip() == bind.local.ip)
            {
                let remote = Address::new(packet.remote_ip(), packet.remote_port());
                return match table.get_conn_state(bind, remote) {
                    Some(state) => state.last_seen > threshold,
                    None => false,
                };
            }
        }

        false
    }

    fn check_pid(&self, socket: &dyn SocketInfo) -> i32 {
        let mut pid = socket.pid();
        for attempt in 1..=CHECK_PID_TRIES {
            pid = self.procfs.pid_for(socket);
            if pid != UNDEFINED_PROCESS_ID {
                break;
            }
            if attempt < CHECK_PID_TRIES {
                // The kernel may not have created the fd yet; back off.
                std::thread::sleep(CHECK_PID_BASE_WAIT * attempt as u32);
            }
        }
        pid
    }
}

fn find_tcp_socket(
    packet: &PacketInfo,
    connections: &[Arc<ConnectionInfo>],
    listeners: &[Arc<BindInfo>],
) -> Option<(Arc<dyn SocketInfo>, bool)> {
    // Always search listeners first.
    for listener in listeners {
        if packet.local_port() == listener.local.port
            && (listener.listens_any || packet.local_ip() == listener.local.ip)
        {
            return Some((Arc::clone(listener) as Arc<dyn SocketInfo>, true));
        }
    }

    for connection in connections {
        if packet.local_port() == connection.local.port
            && packet.remote_port() == connection.remote.port
            && packet.remote_ip() == connection.remote.ip
            && packet.local_ip() == connection.local.ip
        {
            return Some((Arc::clone(connection) as Arc<dyn SocketInfo>, false));
        }
    }

    None
}

fn find_udp_socket(
    packet: &PacketInfo,
    binds: &[Arc<BindInfo>],
    inbound_multicast: bool,
) -> Option<Arc<BindInfo>> {
    binds
        .iter()
        .find(|bind| {
            packet.local_port() == bind.local.port
                && (bind.listens_any || inbound_multicast || packet.local_ip() == bind.local.ip)
        })
        .cloned()
}
