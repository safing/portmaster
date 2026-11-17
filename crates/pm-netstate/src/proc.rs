//! Port of Go `service/network/proc` (Linux): `/proc/net` tables and socket
//! owners via `/proc/<pid>/fd`.
//!
//! # Platform support
//!
//! The `/proc` tables and `/proc/<pid>/fd` owner scans only exist on Linux.
//! On other platforms the parser helpers remain usable with synthetic data,
//! but [`ProcFs`] cannot discover live sockets or their owners: the table
//! reads fail with an I/O error and [`ProcFs::find_pid`] returns
//! [`UNDEFINED_PROCESS_ID`]. Windows has no POSIX user IDs, so socket owners
//! are reported with uid 0 (see [`owner_uid`]).

use std::collections::hash_map::Entry;
use std::collections::HashMap;
use std::fs;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Condvar, Mutex, RwLock};
use std::time::{Duration, Instant};

use crate::socket::{Address, BindInfo, ConnectionInfo, SocketInfo, UNDEFINED_PROCESS_ID};

/// TCP state value of a listening socket.
pub const TCP_LISTEN_STATE_HEX: &str = "0A";

const MIN_DURATION_BETWEEN_PID_UPDATES: Duration = Duration::from_millis(10);
const SOCKET_OWNER_GENERATION: Duration = Duration::from_secs(1);
const SOCKET_OWNER_MAX_ENTRIES: usize = 20_000;

/// The four `/proc/net` tables this crate reads.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Stack {
    Tcp4,
    Udp4,
    Tcp6,
    Udp6,
}

impl Stack {
    /// Returns the file path below the proc root.
    pub fn file(self) -> &'static str {
        match self {
            Stack::Tcp4 => "net/tcp",
            Stack::Udp4 => "net/udp",
            Stack::Tcp6 => "net/tcp6",
            Stack::Udp6 => "net/udp6",
        }
    }

    pub fn is_tcp(self) -> bool {
        matches!(self, Stack::Tcp4 | Stack::Tcp6)
    }

    pub fn version(self) -> u8 {
        match self {
            Stack::Tcp4 | Stack::Udp4 => 4,
            Stack::Tcp6 | Stack::Udp6 => 6,
        }
    }
}

/// Errors of the proc package.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
}

/// Result type of this module.
pub type Result<T> = std::result::Result<T, Error>;

/// Returns the POSIX uid of a `/proc/<pid>` entry on Unix.
///
/// On non-Unix platforms (notably Windows) there is no numeric uid. The
/// fallback returns 0 so that all sockets appear to belong to the same user,
/// which keeps the owner cache consistent without any `/proc` lookups.
#[cfg(unix)]
fn owner_uid(metadata: &fs::Metadata) -> i32 {
    use std::os::unix::fs::MetadataExt;
    metadata.uid() as i32
}

#[cfg(not(unix))]
fn owner_uid(_metadata: &fs::Metadata) -> i32 {
    0
}

#[derive(Debug, Clone, Copy)]
struct SocketOwner {
    uid: i32,
    pid: i32,
}

struct OwnerCache {
    current: HashMap<i64, SocketOwner>,
    previous: HashMap<i64, SocketOwner>,
    rotated_at: Instant,
}

impl OwnerCache {
    fn new() -> OwnerCache {
        OwnerCache {
            current: HashMap::new(),
            previous: HashMap::new(),
            rotated_at: Instant::now(),
        }
    }

    fn rotate(&mut self) {
        match self.rotated_at.elapsed() {
            since if since >= 2 * SOCKET_OWNER_GENERATION => {
                self.previous.clear();
                self.current.clear();
            }
            since if since >= SOCKET_OWNER_GENERATION => {
                self.previous = std::mem::take(&mut self.current);
            }
            _ => return,
        }
        self.rotated_at = Instant::now();
    }

    fn get(&mut self, uid: i32, inode: i64, pids: &[i32]) -> Option<i32> {
        self.rotate();
        let owner = self
            .current
            .get(&inode)
            .or_else(|| self.previous.get(&inode))
            .copied()?;

        // The owner exited, or the inode now belongs to another user's socket.
        if owner.uid != uid || !pids.contains(&owner.pid) {
            self.current.remove(&inode);
            self.previous.remove(&inode);
            return None;
        }
        Some(owner.pid)
    }

    fn insert(&mut self, uid: i32, inode: i64, pid: i32) {
        self.rotate();
        if self.current.len() >= SOCKET_OWNER_MAX_ENTRIES {
            self.previous = std::mem::take(&mut self.current);
            self.rotated_at = Instant::now();
        }
        self.current.insert(inode, SocketOwner { uid, pid });
    }
}

/// State shared by all callers waiting on one in-flight owner scan.
struct FlightState {
    done: bool,
    result: i32,
}

/// A single-flight group for one socket inode: one caller scans `/proc` while
/// the others wait for the published result instead of repeating the scan.
struct Flight {
    state: Mutex<FlightState>,
    ready: Condvar,
}

impl Flight {
    fn new() -> Flight {
        Flight {
            state: Mutex::new(FlightState {
                done: false,
                result: UNDEFINED_PROCESS_ID,
            }),
            ready: Condvar::new(),
        }
    }

    fn resolve(&self, result: i32) {
        let mut state = self.state.lock().unwrap_or_else(|e| e.into_inner());
        state.result = result;
        state.done = true;
        self.ready.notify_all();
    }

    fn wait(&self) -> i32 {
        let mut state = self.state.lock().unwrap_or_else(|e| e.into_inner());
        while !state.done {
            state = self
                .ready
                .wait(state)
                .unwrap_or_else(|e| e.into_inner());
        }
        state.result
    }
}

/// Makes sure the leader always resolves and deregisters its flight, even if
/// the `/proc` scan panics, so waiting callers cannot block forever.
struct FlightGuard<'a> {
    flights: &'a Mutex<HashMap<i64, Arc<Flight>>>,
    inode: i64,
    flight: Arc<Flight>,
    resolved: bool,
}

impl Drop for FlightGuard<'_> {
    fn drop(&mut self) {
        if !self.resolved {
            self.flight.resolve(UNDEFINED_PROCESS_ID);
        }
        self.flights
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .remove(&self.inode);
    }
}

/// Access to the proc file system, with the same caches as the Go package.
pub struct ProcFs {
    root: PathBuf,
    pids_by_user: RwLock<HashMap<i32, Vec<i32>>>,
    pids_updated_at: Mutex<Option<Instant>>,
    owners: Mutex<OwnerCache>,
    /// In-flight owner scans, keyed by inode, for single-flight deduplication.
    flights: Mutex<HashMap<i64, Arc<Flight>>>,
}

impl Default for ProcFs {
    fn default() -> Self {
        ProcFs::new("/proc")
    }
}

impl ProcFs {
    /// Creates a handle for the given proc root (usually `/proc`).
    pub fn new(root: impl Into<PathBuf>) -> ProcFs {
        ProcFs {
            root: root.into(),
            pids_by_user: RwLock::new(HashMap::new()),
            pids_updated_at: Mutex::new(None),
            owners: Mutex::new(OwnerCache::new()),
            flights: Mutex::new(HashMap::new()),
        }
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    /// Returns the IPv4 TCP table.
    pub fn tcp4_table(&self) -> Result<(Vec<ConnectionInfo>, Vec<BindInfo>)> {
        self.table(Stack::Tcp4)
    }

    /// Returns the IPv6 TCP table.
    pub fn tcp6_table(&self) -> Result<(Vec<ConnectionInfo>, Vec<BindInfo>)> {
        self.table(Stack::Tcp6)
    }

    /// Returns the IPv4 UDP table.
    pub fn udp4_table(&self) -> Result<(Vec<ConnectionInfo>, Vec<BindInfo>)> {
        self.table(Stack::Udp4)
    }

    /// Returns the IPv6 UDP table.
    pub fn udp6_table(&self) -> Result<(Vec<ConnectionInfo>, Vec<BindInfo>)> {
        self.table(Stack::Udp6)
    }

    /// Reads and parses one of the tables.
    pub fn table(&self, stack: Stack) -> Result<(Vec<ConnectionInfo>, Vec<BindInfo>)> {
        let data = fs::read_to_string(self.root.join(stack.file()))?;
        Ok(parse_table(stack, &data))
    }

    /// Returns the PID of the socket's owner, caching the result like Go
    /// `proc.GetPID`.
    pub fn pid_for(&self, socket: &dyn SocketInfo) -> i32 {
        let current = socket.pid();
        if current != UNDEFINED_PROCESS_ID {
            return current;
        }
        let (_, inode) = socket.uid_and_inode();
        let pid = self.find_pid(socket.uid(), inode);
        socket.set_pid(pid);
        pid
    }

    /// Returns the PID that owns the socket with the given UID and inode.
    pub fn find_pid(&self, uid: i32, inode: i64) -> i32 {
        self.update_pids();

        let pids = match self
            .pids_by_user
            .read()
            .expect("lock poisoned")
            .get(&uid)
            .cloned()
        {
            Some(pids) => pids,
            None => return UNDEFINED_PROCESS_ID,
        };

        if let Some(pid) = self
            .owners
            .lock()
            .expect("lock poisoned")
            .get(uid, inode, &pids)
        {
            return pid;
        }

        // Scanning every /proc/<pid>/fd is expensive. When several callers look
        // up the same inode concurrently, let one of them do the scan and have
        // the rest wait for its result (Go single-flight style). The owner cache
        // above still serves all later lookups.
        let (flight, leader) = {
            let mut flights = self.flights.lock().expect("lock poisoned");
            match flights.entry(inode) {
                Entry::Occupied(entry) => (Arc::clone(entry.get()), false),
                Entry::Vacant(entry) => {
                    let flight = Arc::new(Flight::new());
                    entry.insert(Arc::clone(&flight));
                    (flight, true)
                }
            }
        };

        if !leader {
            return flight.wait();
        }

        let mut guard = FlightGuard {
            flights: &self.flights,
            inode,
            flight: Arc::clone(&flight),
            resolved: false,
        };

        // Higher PIDs are more likely to be the ones we are looking for.
        let mut result = UNDEFINED_PROCESS_ID;
        for pid in pids.iter().rev() {
            if self.pid_has_socket(*pid, inode) {
                self.owners
                    .lock()
                    .expect("lock poisoned")
                    .insert(uid, inode, *pid);
                result = *pid;
                break;
            }
        }

        flight.resolve(result);
        guard.resolved = true;
        result
    }

    /// Refreshes the PID table at most every 10ms, like `proc.updatePids`.
    fn update_pids(&self) {
        {
            let mut updated_at = self.pids_updated_at.lock().expect("lock poisoned");
            if updated_at.is_some_and(|at| at.elapsed() < MIN_DURATION_BETWEEN_PID_UPDATES) {
                return;
            }
            *updated_at = Some(Instant::now());
        }

        let mut pids_by_user: HashMap<i32, Vec<i32>> = HashMap::new();
        let Ok(entries) = fs::read_dir(&self.root) else {
            return;
        };
        for entry in entries.flatten() {
            let Ok(name) = entry.file_name().into_string() else {
                continue;
            };
            let Ok(pid) = name.parse::<i32>() else {
                continue;
            };
            let Ok(metadata) = entry.metadata() else {
                continue;
            };
            pids_by_user
                .entry(owner_uid(&metadata))
                .or_default()
                .push(pid);
        }

        *self.pids_by_user.write().expect("lock poisoned") = pids_by_user;
    }

    fn pid_has_socket(&self, pid: i32, inode: i64) -> bool {
        let fd_dir = self.root.join(pid.to_string()).join("fd");
        let Ok(entries) = fs::read_dir(&fd_dir) else {
            return false;
        };

        let target = format!("socket:[{inode}]");
        let mut paths: Vec<PathBuf> = entries.flatten().map(|entry| entry.path()).collect();
        // Higher descriptors are more likely to be the ones we are looking for.
        paths.reverse();
        for path in paths {
            if let Ok(link) = fs::read_link(&path) {
                if link.to_string_lossy() == target {
                    return true;
                }
            }
        }
        false
    }
}

/// Parses a proc net table, like Go `getTableFromSource`.
pub fn parse_table(stack: Stack, data: &str) -> (Vec<ConnectionInfo>, Vec<BindInfo>) {
    let mut connections = Vec::new();
    let mut binds = Vec::new();

    let converter: fn(&str) -> Option<IpAddr> = match stack {
        Stack::Tcp4 | Stack::Udp4 => convert_ipv4,
        Stack::Tcp6 | Stack::Udp6 => convert_ipv6,
    };

    for line in data.lines().skip(1) {
        let fields = split_fields(line);
        if fields.len() < 14 {
            continue;
        }

        let Some(local_ip) = converter(fields[1]) else {
            continue;
        };
        let Ok(local_port) = u16::from_str_radix(fields[2], 16) else {
            continue;
        };
        let Ok(uid) = fields[11].parse::<i32>() else {
            continue;
        };
        let Ok(inode) = fields[13].parse::<i64>() else {
            continue;
        };

        let local = Address::new(local_ip, local_port);
        if stack.is_tcp() && fields[5] != TCP_LISTEN_STATE_HEX {
            let Some(remote_ip) = converter(fields[3]) else {
                continue;
            };
            let Ok(remote_port) = u16::from_str_radix(fields[4], 16) else {
                continue;
            };
            connections.push(ConnectionInfo::new(
                local,
                Address::new(remote_ip, remote_port),
                uid,
                inode,
            ));
        } else {
            binds.push(BindInfo::new(local, uid, inode));
        }
    }

    (connections, binds)
}

/// Splits a line on whitespace and colons, like Go's `FieldsFunc`.
fn split_fields(line: &str) -> Vec<&str> {
    line.split(|c: char| c.is_whitespace() || c == ':')
        .filter(|field| !field.is_empty())
        .collect()
}

/// Converts a proc hex IPv4 address (reversed byte order).
pub fn convert_ipv4(data: &str) -> Option<IpAddr> {
    let decoded = hex_decode(data)?;
    if decoded.len() != 4 {
        return None;
    }
    Some(IpAddr::V4(Ipv4Addr::new(
        decoded[3], decoded[2], decoded[1], decoded[0],
    )))
}

/// Converts a proc hex IPv6 address (groups of four bytes reversed).
pub fn convert_ipv6(data: &str) -> Option<IpAddr> {
    let mut decoded = hex_decode(data)?;
    if decoded.len() != 16 {
        return None;
    }
    for group in decoded.chunks_exact_mut(4) {
        group.reverse();
    }
    let mut octets = [0u8; 16];
    octets.copy_from_slice(&decoded);
    Some(IpAddr::V6(Ipv6Addr::from(octets)))
}

fn hex_decode(data: &str) -> Option<Vec<u8>> {
    let bytes = data.as_bytes();
    if !bytes.len().is_multiple_of(2) {
        return None;
    }
    let mut decoded = Vec::with_capacity(bytes.len() / 2);
    for pair in bytes.chunks_exact(2) {
        let high = (pair[0] as char).to_digit(16)?;
        let low = (pair[1] as char).to_digit(16)?;
        decoded.push((high * 16 + low) as u8);
    }
    Some(decoded)
}

#[cfg(test)]
mod tests {
    use super::*;

    const HEADER: &str = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode";

    #[test]
    fn ip_conversion() {
        assert_eq!(
            convert_ipv4("0100007F").unwrap(),
            IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1))
        );
        assert_eq!(
            convert_ipv4("00000000").unwrap(),
            IpAddr::V4(Ipv4Addr::UNSPECIFIED)
        );
        assert_eq!(convert_ipv4("0100007F00"), None);
        assert_eq!(convert_ipv4("zz"), None);

        // ::1 in the kernel's group-reversed order.
        assert_eq!(
            convert_ipv6("00000000000000000000000001000000").unwrap(),
            IpAddr::V6(Ipv6Addr::LOCALHOST)
        );
        assert_eq!(convert_ipv6("00"), None);
    }

    #[test]
    fn parses_tcp_table() {
        let data = format!(
            "{HEADER}
   0: 0100007F:1F90 0500007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 20 4 30 10 -1
   1: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 999 1 0000000000000000 100 0 0 10 0
   2: too short
"
        );
        let (connections, binds) = parse_table(Stack::Tcp4, &data);
        assert_eq!(connections.len(), 1);
        assert_eq!(
            connections[0].local,
            Address::new("127.0.0.1".parse().unwrap(), 8080)
        );
        assert_eq!(
            connections[0].remote,
            Address::new("127.0.0.5".parse().unwrap(), 80)
        );
        assert_eq!(connections[0].uid, 1000);
        assert_eq!(connections[0].inode, 12345);

        assert_eq!(binds.len(), 1);
        assert_eq!(binds[0].local, Address::new("0.0.0.0".parse().unwrap(), 22));
        assert!(binds[0].listens_any);
        assert_eq!(binds[0].uid, 0);
        assert_eq!(binds[0].inode, 999);
    }

    #[test]
    fn parses_udp_table_as_binds() {
        let data = format!(
            "{HEADER}
   0: 0100007F:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000   997        0 4242 2 0000000000000000 0"
        );
        let (connections, binds) = parse_table(Stack::Udp4, &data);
        assert!(connections.is_empty());
        assert_eq!(binds.len(), 1);
        assert_eq!(binds[0].local.port, 53);
        assert_eq!(binds[0].inode, 4242);
    }

    #[test]
    fn fields_split_on_whitespace_and_colons() {
        assert_eq!(
            split_fields("  0: 0100007F:1F90  0A "),
            vec!["0", "0100007F", "1F90", "0A"]
        );
    }

    #[cfg(unix)]
    #[test]
    fn concurrent_find_pid_scans_are_deduplicated() {
        use std::os::unix::fs::MetadataExt;
        use std::thread;

        let mut root = std::env::temp_dir();
        root.push(format!(
            "pm-netstate-flight-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .subsec_nanos()
        ));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();

        // update_pids groups PIDs by the owner of their /proc/<pid> entry, so
        // the fake process must belong to the current user.
        let uid = fs::metadata(&root).unwrap().uid() as i32;
        let inode = 4242_i64;
        let pid = 2345_i32;
        let fd_dir = root.join(pid.to_string()).join("fd");
        fs::create_dir_all(&fd_dir).unwrap();
        std::os::unix::fs::symlink(format!("socket:[{inode}]"), fd_dir.join("7")).unwrap();

        let procfs = std::sync::Arc::new(ProcFs::new(&root));
        // Prime the PID map so concurrent callers skip update_pids and reach
        // the single-flight path with an empty owner cache.
        procfs.update_pids();
        let mut handles = Vec::new();
        for _ in 0..8 {
            let procfs = std::sync::Arc::clone(&procfs);
            handles.push(thread::spawn(move || procfs.find_pid(uid, inode)));
        }
        for handle in handles {
            assert_eq!(handle.join().unwrap(), pid);
        }

        assert!(
            procfs.flights.lock().unwrap().is_empty(),
            "all in-flight scans must be deregistered"
        );

        let _ = fs::remove_dir_all(&root);
    }
}
