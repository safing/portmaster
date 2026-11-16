//! Authentication of local API clients by the requesting process.
//!
//! Port of Go `service/firewall/api.go`: the process that owns the client end
//! of the TCP connection is looked up, and access is granted when the process
//! (or one of its two parents) is a trusted binary.

use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::path::{Component, Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use pm_api::{AuthError, AuthProcess, AuthToken, Authenticator, Request, PERMIT_SELF};
use pm_netstate::{ip_scope, IpProtocol, IpScope, NetState, PacketInfo, ProcFs};
use pm_process::{
    get_pid_of_connection, ProcReader, ProcessCache, SYSTEM_PROCESS_ID, UNIDENTIFIED_PROCESS_ID,
};

const DENIED_MSG_UNIDENTIFIED: &str =
    "Failed to identify the requesting process. Reload to try again.";

const DENIED_MSG_SYSTEM: &str = "System access to the Portmaster API is not permitted.
You can enable the Development Mode to disable API authentication for development purposes.";

const DENIED_MSG_MISCONFIGURED: &str = "The authentication system is misconfigured.";

/// Attempts to identify the requesting process: every 500 ms for 2.5 s.
const AUTH_TRIES: usize = 5;
const AUTH_RETRY_DELAY: Duration = Duration::from_millis(500);

/// Parent levels that are checked in addition to the requesting process.
const CHECK_LEVELS: usize = 2;

fn denied_unauthorized(checked: &[String], root: &str) -> String {
    format!(
        "The requesting process is not authorized to access the Portmaster API.
Checked process paths:
{}

The authorized root path is {}.
You can enable the Development Mode to disable API authentication for development purposes.
For production use please create an API key in the settings.",
        checked.join("\n"),
        root
    )
}

/// Process details needed for authentication.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProcessInfo {
    pub pid: i32,
    pub parent_pid: i32,
    pub path: String,
}

/// System lookups of the authenticator; replaceable in tests.
pub trait ProcessLookup: Send + Sync {
    /// Whether the IP belongs to this machine; `None` when it cannot be told.
    fn is_my_ip(&self, ip: IpAddr) -> Option<bool>;
    /// PID of the process that owns the TCP socket `client` connected to
    /// `server` with; negative when it is not known.
    fn pid_of_connection(&self, client: SocketAddr, server: SocketAddr) -> i32;
    /// Loads a process; `None` when it cannot be read.
    fn process(&self, pid: i32) -> Option<ProcessInfo>;
}

/// Authenticates API requests by the requesting process.
pub struct ApiAuthenticator {
    lookup: Arc<dyn ProcessLookup>,
    bin_dir: PathBuf,
    allowed_clients: Vec<PathBuf>,
    server_addr: SocketAddr,
    retry_delay: Duration,
}

/// Access is denied; `retry` asks for another attempt.
struct Denial {
    message: String,
    retry: bool,
}

impl ApiAuthenticator {
    /// Creates an authenticator for the API listening on `server_addr`.
    pub fn new(
        lookup: Arc<dyn ProcessLookup>,
        bin_dir: PathBuf,
        allowed_clients: Vec<PathBuf>,
        server_addr: SocketAddr,
    ) -> ApiAuthenticator {
        // Correct 0.0.0.0 to 127.0.0.1 to fix local process-based
        // authentication, if 0.0.0.0 is used as the API listen address.
        let server_addr = match server_addr.ip() {
            IpAddr::V4(ip) if ip.is_unspecified() => {
                SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), server_addr.port())
            }
            _ => server_addr,
        };
        ApiAuthenticator {
            lookup,
            bin_dir,
            allowed_clients,
            server_addr,
            retry_delay: AUTH_RETRY_DELAY,
        }
    }

    /// Overrides the delay between attempts.
    pub fn with_retry_delay(mut self, delay: Duration) -> ApiAuthenticator {
        self.retry_delay = delay;
        self
    }

    fn authenticate_process(&self, client: SocketAddr) -> Result<AuthProcess, Denial> {
        let denied = |message: &str, retry: bool| Denial {
            message: message.to_string(),
            retry,
        };

        let mut checked: Vec<String> = Vec::new();

        // Get the authenticated path.
        if self.bin_dir.as_os_str().is_empty() {
            return Err(denied(DENIED_MSG_MISCONFIGURED, false));
        }
        let real_root = std::fs::canonicalize(&self.bin_dir)
            .map_err(|_| denied(DENIED_MSG_UNIDENTIFIED, false))?;
        // Add the separator to confine to the directory.
        let root = format!("{}/", real_root.to_string_lossy());

        // Get the process of the request.
        let pid = self.lookup.pid_of_connection(client, self.server_addr);
        if pid < 0 {
            return Err(denied(DENIED_MSG_UNIDENTIFIED, false));
        }

        let original_pid;
        match self.lookup.process(pid) {
            None => {
                tracing::debug!("api auth: failed to get process of api request");
                original_pid = UNIDENTIFIED_PROCESS_ID;
            }
            Some(first) => {
                original_pid = first.pid;
                let original_path = first.path.clone();
                let mut proc = first;
                // Check the process and its parents for up to two levels.
                for level in 0..=CHECK_LEVELS {
                    if proc.pid == UNIDENTIFIED_PROCESS_ID || proc.pid == SYSTEM_PROCESS_ID {
                        break;
                    }

                    if let Ok(real_path) = std::fs::canonicalize(&proc.path) {
                        let allowed = self.allowed_clients.contains(&real_path)
                            || real_path.to_string_lossy().starts_with(&root);
                        if allowed {
                            return Ok(AuthProcess {
                                pid: original_pid,
                                path: original_path,
                            });
                        }
                    }
                    checked.push(proc.path.clone());

                    if level < CHECK_LEVELS {
                        let previous_pid = proc.pid;
                        match self.lookup.process(proc.parent_pid) {
                            Some(parent) => proc = parent,
                            None => {
                                tracing::debug!("api auth: failed to get parent process");
                                break;
                            }
                        }
                        // Abort if we are looping.
                        if proc.pid == previous_pid {
                            break;
                        }
                    }
                }
            }
        }

        match original_pid {
            UNIDENTIFIED_PROCESS_ID => {
                tracing::warn!("api auth: denying access: failed to identify process");
                Err(denied(DENIED_MSG_UNIDENTIFIED, true))
            }
            SYSTEM_PROCESS_ID => {
                tracing::warn!("api auth: denying access: request by system");
                Err(denied(DENIED_MSG_SYSTEM, false))
            }
            _ => {
                tracing::warn!(
                    first = checked.first().map(String::as_str).unwrap_or(""),
                    "api auth: denying access"
                );
                Err(Denial {
                    message: denied_unauthorized(&checked, &root),
                    retry: false,
                })
            }
        }
    }
}

impl Authenticator for ApiAuthenticator {
    fn authenticate(&self, request: &Request) -> Result<Option<AuthToken>, AuthError> {
        let client: SocketAddr = request
            .remote_addr
            .parse()
            .map_err(|_| AuthError::Internal("failed to get remote IP/Port".to_string()))?;

        // Check if the request is even local. A failed check does not reject.
        if self.lookup.is_my_ip(client.ip()) == Some(false) {
            // Return to the caller that the request was not handled.
            return Ok(None);
        }

        // It is important that this works: up to 5 attempts, 500 ms after each
        // retry-flagged one (2.5 s in total).
        let mut result: Result<AuthProcess, Denial> = Ok(AuthProcess { pid: -1, path: String::new() });
        for _ in 0..AUTH_TRIES {
            result = self.authenticate_process(client);
            match &result {
                // Like Go, sleep after every retry-flagged attempt.
                Err(Denial { retry: true, .. }) => std::thread::sleep(self.retry_delay),
                _ => break,
            }
        }

        match result {
            Ok(process) => Ok(Some(
                AuthToken::new(PERMIT_SELF, PERMIT_SELF).with_process(process),
            )),
            Err(Denial { message, .. }) => Err(AuthError::Denied(message)),
        }
    }
}

/// Lookups backed by `/proc`.
pub struct SystemLookup {
    cache: Arc<ProcessCache>,
    state: NetState,
}

impl SystemLookup {
    pub fn new() -> SystemLookup {
        SystemLookup {
            cache: ProcessCache::new(ProcReader::new(ProcFs::default())),
            state: NetState::new(ProcFs::default()),
        }
    }
}

impl Default for SystemLookup {
    fn default() -> Self {
        SystemLookup::new()
    }
}

impl ProcessLookup for SystemLookup {
    fn is_my_ip(&self, ip: IpAddr) -> Option<bool> {
        match ip_scope(ip) {
            IpScope::HostLocal => return Some(true),
            IpScope::LocalMulticast | IpScope::GlobalMulticast => return Some(false),
            _ => {}
        }
        // An address can only be bound when it is assigned to this machine.
        match std::net::TcpListener::bind(SocketAddr::new(ip, 0)) {
            Ok(_) => Some(true),
            Err(error) if error.kind() == std::io::ErrorKind::AddrNotAvailable => Some(false),
            Err(_) => None,
        }
    }

    fn pid_of_connection(&self, client: SocketAddr, server: SocketAddr) -> i32 {
        // Outbound, as we are looking for the process of the source address.
        let mut packet = PacketInfo::new(client.ip(), server.ip(), IpProtocol::TCP, false);
        packet.src_port = client.port();
        packet.dst_port = server.port();
        get_pid_of_connection(&self.cache, &self.state, &mut packet, false).0
    }

    fn process(&self, pid: i32) -> Option<ProcessInfo> {
        let process = self.cache.get_or_find(pid).ok()?;
        Some(ProcessInfo {
            pid: process.pid,
            parent_pid: process.parent_pid,
            path: process.path.clone(),
        })
    }
}

/// Lexically cleans a path like Go `filepath.Clean`.
pub fn clean_path(path: &str) -> PathBuf {
    let mut cleaned = PathBuf::new();
    for component in Path::new(path).components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                if !cleaned.pop() && !path.starts_with('/') {
                    cleaned.push("..");
                }
            }
            other => cleaned.push(other),
        }
    }
    if cleaned.as_os_str().is_empty() {
        PathBuf::from(".")
    } else {
        cleaned
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;

    struct FakeLookup {
        my_ip: Option<bool>,
        /// PIDs returned by consecutive lookups; the last one repeats.
        pids: Mutex<Vec<i32>>,
        processes: HashMap<i32, ProcessInfo>,
        pid_calls: Mutex<usize>,
    }

    impl FakeLookup {
        fn new(pid: i32, processes: Vec<ProcessInfo>) -> FakeLookup {
            FakeLookup {
                my_ip: Some(true),
                pids: Mutex::new(vec![pid]),
                processes: processes.into_iter().map(|p| (p.pid, p)).collect(),
                pid_calls: Mutex::new(0),
            }
        }
    }

    impl ProcessLookup for FakeLookup {
        fn is_my_ip(&self, _ip: IpAddr) -> Option<bool> {
            self.my_ip
        }
        fn pid_of_connection(&self, _client: SocketAddr, _server: SocketAddr) -> i32 {
            *self.pid_calls.lock().unwrap() += 1;
            let mut pids = self.pids.lock().unwrap();
            if pids.len() > 1 {
                pids.remove(0)
            } else {
                pids[0]
            }
        }
        fn process(&self, pid: i32) -> Option<ProcessInfo> {
            self.processes.get(&pid).cloned()
        }
    }

    fn proc(pid: i32, parent_pid: i32, path: &Path) -> ProcessInfo {
        ProcessInfo {
            pid,
            parent_pid,
            path: path.to_string_lossy().into_owned(),
        }
    }

    struct Env {
        dir: PathBuf,
        bin: PathBuf,
        inside: PathBuf,
        outside: PathBuf,
    }

    fn env(tag: &str) -> Env {
        let dir = std::env::temp_dir().join(format!("pm-api-auth-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        let bin = dir.join("bin");
        std::fs::create_dir_all(&bin).unwrap();
        let inside = bin.join("ui");
        let outside = dir.join("other");
        std::fs::write(&inside, b"x").unwrap();
        std::fs::write(&outside, b"x").unwrap();
        Env {
            dir,
            bin,
            inside,
            outside,
        }
    }

    fn authenticator(
        lookup: FakeLookup,
        env: &Env,
        allowed: Vec<PathBuf>,
    ) -> (ApiAuthenticator, Arc<FakeLookup>) {
        let lookup = Arc::new(lookup);
        let auth = ApiAuthenticator::new(
            lookup.clone(),
            env.bin.clone(),
            allowed,
            "127.0.0.1:817".parse().unwrap(),
        )
        .with_retry_delay(Duration::ZERO);
        (auth, lookup)
    }

    fn request() -> Request {
        let mut request = Request::new("GET", "/api/v1/ping");
        request.remote_addr = "127.0.0.1:40000".to_string();
        request
    }

    fn denied_message(result: Result<Option<AuthToken>, AuthError>) -> String {
        match result {
            Err(AuthError::Denied(message)) => message,
            other => panic!("expected denial, got {other:?}"),
        }
    }

    #[test]
    fn allowed_by_bin_dir() {
        let e = env("bindir");
        let lookup = FakeLookup::new(100, vec![proc(100, 1, &e.inside)]);
        let (auth, _) = authenticator(lookup, &e, vec![]);
        assert_eq!(
            auth.authenticate(&request()).unwrap(),
            Some(
                AuthToken::new(PERMIT_SELF, PERMIT_SELF).with_process(AuthProcess {
                    pid: 100,
                    path: e.inside.to_string_lossy().into_owned(),
                }),
            )
        );
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn bin_dir_prefix_is_confined_to_directory() {
        let e = env("confine");
        let sibling = e.dir.join("bin-evil");
        std::fs::write(&sibling, b"x").unwrap();
        let lookup = FakeLookup::new(100, vec![proc(100, 100, &sibling)]);
        let (auth, _) = authenticator(lookup, &e, vec![]);
        denied_message(auth.authenticate(&request()));
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn allowed_by_parent_levels() {
        let e = env("parent");
        let lookup = FakeLookup::new(
            100,
            vec![
                proc(100, 90, &e.outside),
                proc(90, 80, &e.outside),
                proc(80, 70, &e.inside),
            ],
        );
        let (auth, _) = authenticator(lookup, &e, vec![]);
        assert!(auth.authenticate(&request()).unwrap().is_some());

        // Three levels up is too far.
        let lookup = FakeLookup::new(
            100,
            vec![
                proc(100, 90, &e.outside),
                proc(90, 80, &e.outside),
                proc(80, 70, &e.outside),
                proc(70, 1, &e.inside),
            ],
        );
        let (auth, _) = authenticator(lookup, &e, vec![]);
        denied_message(auth.authenticate(&request()));
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn allowed_by_allowed_clients() {
        let e = env("clients");
        let lookup = FakeLookup::new(100, vec![proc(100, 1, &e.outside)]);
        let (auth, _) = authenticator(lookup, &e, vec![std::fs::canonicalize(&e.outside).unwrap()]);
        assert!(auth.authenticate(&request()).unwrap().is_some());
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn unidentified_when_lookup_fails_without_retry() {
        let e = env("unident");
        let lookup = FakeLookup::new(UNIDENTIFIED_PROCESS_ID, vec![]);
        let (auth, lookup) = authenticator(lookup, &e, vec![]);
        let message = denied_message(auth.authenticate(&request()));
        assert_eq!(message, DENIED_MSG_UNIDENTIFIED);
        assert_eq!(*lookup.pid_calls.lock().unwrap(), 1);
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn system_process_is_denied() {
        let e = env("system");
        let lookup = FakeLookup::new(0, vec![proc(0, 0, Path::new(""))]);
        let (auth, lookup) = authenticator(lookup, &e, vec![]);
        let message = denied_message(auth.authenticate(&request()));
        assert_eq!(message, DENIED_MSG_SYSTEM);
        assert_eq!(*lookup.pid_calls.lock().unwrap(), 1);
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn unauthorized_message_lists_checked_paths() {
        let e = env("unauth");
        let lookup = FakeLookup::new(
            100,
            vec![
                proc(100, 90, &e.outside),
                proc(90, 90, Path::new("/bin/parent")),
            ],
        );
        let (auth, _) = authenticator(lookup, &e, vec![]);
        let message = denied_message(auth.authenticate(&request()));
        let root = format!("{}/", std::fs::canonicalize(&e.bin).unwrap().display());
        assert_eq!(
            message,
            format!(
                "The requesting process is not authorized to access the Portmaster API.\n\
Checked process paths:\n{}\n/bin/parent\n\n\
The authorized root path is {}.\n\
You can enable the Development Mode to disable API authentication for development purposes.\n\
For production use please create an API key in the settings.",
                e.outside.display(),
                root
            )
        );
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn misconfigured_and_missing_bin_dir() {
        let e = env("misconf");
        let lookup = Arc::new(FakeLookup::new(100, vec![proc(100, 1, &e.inside)]));
        let auth = ApiAuthenticator::new(
            lookup.clone(),
            PathBuf::new(),
            vec![],
            "127.0.0.1:817".parse().unwrap(),
        );
        assert_eq!(
            denied_message(auth.authenticate(&request())),
            DENIED_MSG_MISCONFIGURED
        );

        let auth = ApiAuthenticator::new(
            lookup,
            e.dir.join("missing"),
            vec![],
            "127.0.0.1:817".parse().unwrap(),
        );
        assert_eq!(
            denied_message(auth.authenticate(&request())),
            DENIED_MSG_UNIDENTIFIED
        );
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn not_my_ip_returns_no_token() {
        let e = env("notmine");
        let mut lookup = FakeLookup::new(100, vec![proc(100, 1, &e.inside)]);
        lookup.my_ip = Some(false);
        let (auth, lookup) = authenticator(lookup, &e, vec![]);
        assert_eq!(auth.authenticate(&request()).unwrap(), None);
        assert_eq!(*lookup.pid_calls.lock().unwrap(), 0);
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn unreadable_process_is_retried() {
        let e = env("retry");
        // PID 100 cannot be loaded at first; a later attempt finds PID 200.
        let lookup = FakeLookup::new(100, vec![proc(200, 1, &e.inside)]);
        *lookup.pids.lock().unwrap() = vec![100, 100, 200];
        let (auth, lookup) = authenticator(lookup, &e, vec![]);
        assert!(auth.authenticate(&request()).unwrap().is_some());
        assert_eq!(*lookup.pid_calls.lock().unwrap(), 3);

        // A process that never loads is denied after five attempts.
        let lookup = FakeLookup::new(100, vec![]);
        let (auth, lookup) = authenticator(lookup, &e, vec![]);
        let message = denied_message(auth.authenticate(&request()));
        assert_eq!(message, DENIED_MSG_UNIDENTIFIED);
        assert_eq!(*lookup.pid_calls.lock().unwrap(), AUTH_TRIES);
        let _ = std::fs::remove_dir_all(&e.dir);
    }

    #[test]
    fn clean_path_matches_go() {
        assert_eq!(clean_path("/a//b/./c/../d"), PathBuf::from("/a/b/d"));
        assert_eq!(clean_path("/.."), PathBuf::from("/"));
        assert_eq!(clean_path(""), PathBuf::from("."));
        assert_eq!(clean_path("../x"), PathBuf::from("../x"));
    }

    /// Needs a readable `/proc`: a connection of this process must be found.
    #[test]
    #[ignore = "needs /proc and a local TCP connection"]
    fn live_lookup_finds_current_process() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let server = listener.local_addr().unwrap();
        let stream = std::net::TcpStream::connect(server).unwrap();
        let client = stream.local_addr().unwrap();
        let _accepted = listener.accept().unwrap();

        let lookup = SystemLookup::new();
        assert_eq!(lookup.is_my_ip(client.ip()), Some(true));
        let pid = lookup.pid_of_connection(client, server);
        assert_eq!(pid, std::process::id() as i32);
        let process = lookup.process(pid).unwrap();
        assert_eq!(
            std::fs::canonicalize(&process.path).unwrap(),
            std::fs::canonicalize(std::env::current_exe().unwrap()).unwrap()
        );
    }
}
