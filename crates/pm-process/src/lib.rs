//! Port of the Go `service/process` package.
//!
//! The crate keeps a cache of processes, reads their details from the proc file
//! system and attributes network connections to processes.
//!
//! # Compatibility notes
//!
//! * Process details are read from `/proc` directly instead of `gopsutil`.
//! * Profiles and their matching are not ported yet; `PrimaryProfileID`, tags
//!   and `MatchingPath` are carried as data, and the process group leader is
//!   resolved through the cache instead of a link on the process object.
//! * Process loads are serialized with one mutex instead of Go's single-flight
//!   groups; the resulting cache is the same.
//! * Executable hashing (`ExecHashes`) is not implemented yet.
//! * The process tree database is populated through an injected
//!   [`pm_database::Controller`], like Go's `SetDBController`.

mod cache;
mod config;
mod find;
mod info;

use serde::{Deserialize, Serialize};

pub use cache::{now, ProcessCache, SpecialProcess, TagDescription, TagHandler};
pub use config::{enabled_getter, register_options, CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY};
pub use find::{get_pid_of_connection, get_process_of_connection};
pub use info::{Error as ProcError, ProcInfo, ProcReader};

/// PID that is not used by any process.
pub const UNDEFINED_PROCESS_ID: i32 = -1;
/// PID for outgoing connections without an attributable process.
pub const UNIDENTIFIED_PROCESS_ID: i32 = -2;
/// PID for incoming connections without an attributable process.
pub const UNSOLICITED_PROCESS_ID: i32 = -3;
/// PID used for hosts on the network.
pub const NETWORK_HOST_PROCESS_ID: i32 = -255;
/// PID of the system/kernel itself.
pub const SYSTEM_PROCESS_ID: i32 = 0;
/// PID of the system init process.
pub const SYSTEM_INIT_ID: i32 = 1;

/// Name of the unidentified process profile.
pub const UNIDENTIFIED_PROFILE_NAME: &str = "Other Connections";
/// Name of the unsolicited process profile.
pub const UNSOLICITED_PROFILE_NAME: &str = "Network Noise";
/// Name of the system profile.
pub const SYSTEM_PROFILE_NAME: &str = "Operating System";

/// A simple key/value tag used in process metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Tag {
    #[serde(rename = "Key")]
    pub key: String,
    #[serde(rename = "Value")]
    pub value: String,
}

/// A process running on the operating system.
///
/// Field names and order match the Go struct, so records serialized by this
/// crate are compatible with the Go core.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Process {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "UserID")]
    pub user_id: i32,
    #[serde(rename = "UserName")]
    pub user_name: String,
    #[serde(rename = "UserHome")]
    pub user_home: String,
    #[serde(rename = "Pid")]
    pub pid: i32,
    #[serde(rename = "CreatedAt")]
    pub created_at: i64,
    #[serde(rename = "ParentPid")]
    pub parent_pid: i32,
    #[serde(rename = "ParentCreatedAt")]
    pub parent_created_at: i64,
    #[serde(rename = "LeaderPid")]
    pub leader_pid: i32,
    #[serde(rename = "Path")]
    pub path: String,
    #[serde(rename = "ExecName")]
    pub exec_name: String,
    #[serde(rename = "Cwd")]
    pub cwd: String,
    #[serde(rename = "CmdLine")]
    pub cmd_line: String,
    #[serde(rename = "FirstArg")]
    pub first_arg: String,
    #[serde(rename = "Env")]
    pub env: std::collections::BTreeMap<String, String>,
    #[serde(rename = "Tags")]
    pub tags: Vec<Tag>,
    #[serde(rename = "MatchingPath")]
    pub matching_path: String,
    #[serde(rename = "PrimaryProfileID")]
    pub primary_profile_id: String,
    #[serde(rename = "FirstSeen")]
    pub first_seen: i64,
    #[serde(rename = "LastSeen")]
    pub last_seen: i64,
    #[serde(rename = "Error")]
    pub error: String,
    #[serde(rename = "ExecHashes")]
    pub exec_hashes: std::collections::BTreeMap<String, String>,
    /// Unique identifier `"pid-createdAt"`; not serialized.
    #[serde(skip)]
    process_key: String,
}

impl Process {
    /// Returns the unique process key (`pid-createdAt`).
    pub fn key(&self) -> &str {
        &self.process_key
    }

    /// Returns whether the process was identified as a real process.
    pub fn is_identified(&self) -> bool {
        !matches!(
            self.pid,
            UNDEFINED_PROCESS_ID | UNIDENTIFIED_PROCESS_ID | UNSOLICITED_PROCESS_ID
        )
    }

    /// Returns whether the process has a valid PID of an actual process.
    pub fn has_valid_pid(&self) -> bool {
        self.pid >= 0
    }

    /// Returns whether both processes are identified and have the same PID.
    pub fn equal(&self, other: &Process) -> bool {
        self.is_identified() && other.is_identified() && self.pid == other.pid
    }

    /// Returns the process tag with the given key.
    pub fn tag(&self, key: &str) -> Option<&Tag> {
        self.tags.iter().find(|tag| tag.key == key)
    }

    /// Returns the matching data for profile matching.
    pub fn matching_data(&self) -> MatchingData<'_> {
        MatchingData { process: self }
    }

    /// Returns the database key of the process record.
    pub fn scoped_key(&self) -> String {
        format!("tree/{}", self.process_key)
    }

    /// Creates a process with empty fields and the given key.
    pub fn empty(process_key: &str) -> Process {
        Process {
            name: String::new(),
            user_id: UNDEFINED_PROCESS_ID,
            user_name: String::new(),
            user_home: String::new(),
            pid: UNDEFINED_PROCESS_ID,
            created_at: 0,
            parent_pid: UNDEFINED_PROCESS_ID,
            parent_created_at: 0,
            leader_pid: UNDEFINED_PROCESS_ID,
            path: String::new(),
            exec_name: String::new(),
            cwd: String::new(),
            cmd_line: String::new(),
            first_arg: String::new(),
            env: std::collections::BTreeMap::new(),
            tags: Vec::new(),
            matching_path: String::new(),
            primary_profile_id: String::new(),
            first_seen: 0,
            last_seen: 0,
            error: String::new(),
            exec_hashes: std::collections::BTreeMap::new(),
            process_key: process_key.to_string(),
        }
    }
}

impl std::fmt::Display for Process {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}:{}:{}", self.user_name, self.path, self.pid)
    }
}

/// An interface-compatible view on the process for profile matching.
pub struct MatchingData<'a> {
    process: &'a Process,
}

impl MatchingData<'_> {
    pub fn tags(&self) -> &[Tag] {
        &self.process.tags
    }

    pub fn env(&self) -> &std::collections::BTreeMap<String, String> {
        &self.process.env
    }

    pub fn path(&self) -> &str {
        &self.process.path
    }

    pub fn matching_path(&self) -> &str {
        &self.process.matching_path
    }

    pub fn cmdline(&self) -> &str {
        &self.process.cmd_line
    }
}

/// Builds the unique process key from PID and creation time.
pub fn process_key(pid: i32, created_at: i64) -> String {
    format!("{pid}-{created_at}")
}
