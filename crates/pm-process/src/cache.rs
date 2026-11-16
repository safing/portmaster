//! The process cache, special processes, tags and the process tree database.

use std::collections::{HashMap, HashSet};
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pm_database::{Controller, Key, Meta, Record};

use crate::info::{ProcInfo, ProcReader};
use crate::{
    process_key, Process, SYSTEM_PROCESS_ID, SYSTEM_PROFILE_NAME, UNDEFINED_PROCESS_ID,
    UNIDENTIFIED_PROCESS_ID, UNIDENTIFIED_PROFILE_NAME, UNSOLICITED_PROCESS_ID,
    UNSOLICITED_PROFILE_NAME,
};

/// How long processes are kept after they were last seen.
const DELETE_PROCESSES_THRESHOLD: Duration = Duration::from_secs(7 * 60);

/// Database namespace of the process tree.
pub const PROCESS_DATABASE_NAMESPACE: &str = "tree";

/// Errors of the process cache.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("process: {0}")]
    Proc(#[from] crate::info::Error),
    #[error("tag handler \"{0}\" is already registered")]
    TagHandler(String),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
}

/// Result type of the process cache.
pub type Result<T> = std::result::Result<T, Error>;

/// Describes a process tag.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagDescription {
    pub id: String,
    pub name: String,
    pub description: String,
}

/// A collection of process tag related callbacks.
pub trait TagHandler: Send + Sync {
    /// Returns the name of the tag handler.
    fn name(&self) -> &str;

    /// Returns all possible tags of this handler.
    fn tag_descriptions(&self) -> Vec<TagDescription> {
        Vec::new()
    }

    /// Adds tags to the given process.
    fn add_tags(&self, process: &mut Process);
}

/// The kinds of special processes.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SpecialProcess {
    Unidentified,
    Unsolicited,
    System,
}

/// The cache of all known processes.
pub struct ProcessCache {
    reader: ProcReader,
    processes: RwLock<HashMap<String, Arc<Process>>>,
    load_lock: Mutex<()>,
    tags: RwLock<Vec<Arc<dyn TagHandler>>>,
    db: RwLock<Option<Arc<Controller>>>,
    enabled: AtomicBool,
}

impl ProcessCache {
    /// Creates a cache with the given proc reader.
    pub fn new(reader: ProcReader) -> Arc<ProcessCache> {
        Arc::new(ProcessCache {
            reader,
            processes: RwLock::new(HashMap::new()),
            load_lock: Mutex::new(()),
            tags: RwLock::new(Vec::new()),
            db: RwLock::new(None),
            enabled: AtomicBool::new(true),
        })
    }

    /// Creates a cache with all built-in tag handlers registered.
    ///
    /// Use this in production so that process attribution and profile
    /// matching see the tags produced by the Linux handlers.
    pub fn with_default_tag_handlers(reader: ProcReader) -> Arc<ProcessCache> {
        let cache = ProcessCache::new(reader);
        cache
            .register_default_tag_handlers()
            .expect("registering distinct default tag handlers cannot fail");
        cache
    }

    /// Registers every built-in tag handler, like Go's tag package `init()`s.
    pub fn register_default_tag_handlers(&self) -> Result<()> {
        crate::tags::register_default_tag_handlers(self)
    }

    pub fn reader(&self) -> &ProcReader {
        &self.reader
    }

    /// Enables or disables process detection.
    pub fn set_enabled(&self, enabled: bool) {
        self.enabled.store(enabled, Ordering::SeqCst);
    }

    pub fn enabled(&self) -> bool {
        self.enabled.load(Ordering::SeqCst)
    }

    /// Registers a tag handler.
    pub fn register_tag_handler(&self, handler: Arc<dyn TagHandler>) -> Result<()> {
        let mut tags = self.tags.write().expect("lock poisoned");
        if tags
            .iter()
            .any(|existing| existing.name() == handler.name())
        {
            return Err(Error::TagHandler(handler.name().to_string()));
        }
        tags.push(handler);
        Ok(())
    }

    /// Returns all registered tag descriptions.
    pub fn tag_descriptions(&self) -> Vec<TagDescription> {
        self.tags
            .read()
            .expect("lock poisoned")
            .iter()
            .flat_map(|handler| handler.tag_descriptions())
            .collect()
    }

    /// Sets the database controller that receives process updates.
    pub fn set_db_controller(&self, controller: Arc<Controller>) {
        *self.db.write().expect("lock poisoned") = Some(controller);
    }

    /// Returns the process from the internal storage.
    pub fn get_from_storage(&self, key: &str) -> Option<Arc<Process>> {
        self.processes
            .read()
            .expect("lock poisoned")
            .get(key)
            .cloned()
    }

    /// Returns all processes.
    pub fn all(&self) -> HashMap<i32, Arc<Process>> {
        self.processes
            .read()
            .expect("lock poisoned")
            .values()
            .map(|process| (process.pid, Arc::clone(process)))
            .collect()
    }

    /// Returns the process for the given PID, loading it if necessary.
    pub fn get_or_find(self: &Arc<Self>, pid: i32) -> Result<Arc<Process>> {
        match pid {
            UNIDENTIFIED_PROCESS_ID => return Ok(self.special(SpecialProcess::Unidentified)),
            UNSOLICITED_PROCESS_ID => return Ok(self.special(SpecialProcess::Unsolicited)),
            SYSTEM_PROCESS_ID => return Ok(self.special(SpecialProcess::System)),
            _ => {}
        }

        let info = self.reader.read(pid)?;
        let key = process_key(pid, info.created_at);
        if let Some(process) = self.get_from_storage(&key) {
            return Ok(process);
        }

        // Loads are serialized, like Go's single-flight group per process key.
        let _guard = self.load_lock.lock().expect("lock poisoned");
        if let Some(process) = self.get_from_storage(&key) {
            return Ok(process);
        }
        self.load(info)
    }

    /// Returns the process with the given PID and its process group leader.
    pub fn get_with_leader(
        self: &Arc<Self>,
        pid: i32,
    ) -> Result<(Arc<Process>, Option<Arc<Process>>)> {
        let process = self.get_or_find(pid)?;
        let leader = self.leader(&process);
        Ok((process, leader))
    }

    /// Returns the special process of the given kind.
    pub fn special(self: &Arc<Self>, kind: SpecialProcess) -> Arc<Process> {
        let (pid, name, user_name) = match kind {
            SpecialProcess::Unidentified => (
                UNIDENTIFIED_PROCESS_ID,
                UNIDENTIFIED_PROFILE_NAME,
                "Unknown",
            ),
            SpecialProcess::Unsolicited => {
                (UNSOLICITED_PROCESS_ID, UNSOLICITED_PROFILE_NAME, "Unknown")
            }
            SpecialProcess::System => (SYSTEM_PROCESS_ID, SYSTEM_PROFILE_NAME, "Kernel"),
        };

        let key = process_key(pid, 0);
        if let Some(process) = self.get_from_storage(&key) {
            return process;
        }

        let _guard = self.load_lock.lock().expect("lock poisoned");
        if let Some(process) = self.get_from_storage(&key) {
            return process;
        }

        let mut process = Process::empty(&key);
        process.pid = pid;
        process.user_id = pid;
        process.user_name = user_name.to_string();
        process.parent_pid = pid;
        process.name = name.to_string();
        process.first_seen = now();
        let process = Arc::new(process);
        self.save(&process);
        process
    }

    /// Loads and stores a process from its proc details.
    fn load(self: &Arc<Self>, info: ProcInfo) -> Result<Arc<Process>> {
        let mut process = Process::empty(&process_key(info.pid, info.created_at));
        process.pid = info.pid;
        process.created_at = info.created_at;
        process.first_seen = now();
        process.user_id = info.uid;
        process.user_name = info.username;
        process.parent_pid = info.ppid;
        process.parent_created_at = self
            .reader
            .read(info.ppid)
            .map(|parent| parent.created_at)
            .unwrap_or(0);
        process.leader_pid = info.pgrp;
        process.path = info.exe;
        process.exec_name = Path::new(&process.path)
            .file_name()
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_default();
        process.cwd = info.cwd;
        process.cmd_line = info.cmdline;
        process.env = info.environ;
        process.name = if info.name.is_empty() {
            process.exec_name.clone()
        } else {
            info.name
        };

        self.add_tags(&mut process);

        let process = Arc::new(process);
        self.save(&process);
        Ok(process)
    }

    fn add_tags(&self, process: &mut Process) {
        for handler in self.tags.read().expect("lock poisoned").iter() {
            handler.add_tags(process);
        }
    }

    /// Saves the process to the internal storage and pushes an update.
    pub fn save(&self, process: &Arc<Process>) {
        self.processes
            .write()
            .expect("lock poisoned")
            .insert(process.key().to_string(), Arc::clone(process));
        self.push_update(process, false);
    }

    /// Deletes the process from the internal storage and propagates the change.
    pub fn delete(&self, process: &Arc<Process>) {
        self.processes
            .write()
            .expect("lock poisoned")
            .remove(process.key());
        self.push_update(process, true);
    }

    /// Updates the last seen timestamp of a process.
    pub fn set_last_seen(&self, key: &str, last_seen: i64) -> Option<Arc<Process>> {
        let mut processes = self.processes.write().expect("lock poisoned");
        let process = processes.get(key)?;
        let mut updated = (**process).clone();
        updated.last_seen = last_seen;
        let updated = Arc::new(updated);
        processes.insert(key.to_string(), Arc::clone(&updated));
        Some(updated)
    }

    /// Returns the process group leader of the process, like Go
    /// `FindProcessGroupLeader`.
    pub fn leader(self: &Arc<Self>, process: &Process) -> Option<Arc<Process>> {
        if process.leader_pid == UNDEFINED_PROCESS_ID || process.leader_pid == process.pid {
            return None;
        }
        if let Ok(leader) = self.get_or_find(process.leader_pid) {
            return Some(leader);
        }

        // The leader exited; use the highest existing parent in the group.
        let mut next_parent_pid = process.parent_pid;
        let mut last_parent: Option<Arc<Process>> = None;
        loop {
            let parent = match self.get_or_find(next_parent_pid) {
                Ok(parent) => parent,
                Err(_) => return last_parent,
            };
            if parent.pid == process.leader_pid {
                return Some(parent);
            }
            if parent.leader_pid != process.leader_pid {
                return last_parent;
            }
            if parent.parent_pid == SYSTEM_PROCESS_ID || parent.parent_pid == crate::SYSTEM_INIT_ID
            {
                return Some(parent);
            }
            last_parent = Some(Arc::clone(&parent));
            next_parent_pid = parent.parent_pid;
        }
    }

    /// Cleans the storage from processes that are no longer active.
    pub fn clean_storage(&self, active_pids: &mut HashSet<i32>) {
        if let Ok(entries) = std::fs::read_dir(self.reader.procfs().root()) {
            for entry in entries.flatten() {
                if let Ok(pid) = entry.file_name().to_string_lossy().parse::<i32>() {
                    active_pids.insert(pid);
                }
            }
        }

        let threshold = now() - DELETE_PROCESSES_THRESHOLD.as_secs() as i64;
        for process in self.all().into_values() {
            match process.pid {
                UNIDENTIFIED_PROCESS_ID | UNSOLICITED_PROCESS_ID | SYSTEM_PROCESS_ID => continue,
                _ => {}
            }
            if active_pids.contains(&process.pid) {
                continue;
            }
            match process.last_seen {
                0 => {
                    self.set_last_seen(process.key(), now());
                }
                last_seen if last_seen > threshold => {}
                _ => self.delete(&process),
            }
        }
    }

    fn push_update(&self, process: &Arc<Process>, deleted: bool) {
        let controller = self.db.read().expect("lock poisoned").clone();
        let Some(controller) = controller else {
            return;
        };

        let mut meta = Meta {
            created: process.first_seen,
            modified: now(),
            ..Meta::default()
        };
        let data = if deleted {
            meta.delete(now());
            Vec::new()
        } else {
            match pm_dsd::gojson::to_vec(process.as_ref()) {
                Ok(data) => data,
                Err(error) => {
                    tracing::error!(%error, process = %process.key(), "process: failed to serialize");
                    return;
                }
            }
        };

        controller.push_update(&Record {
            key: Key::new(
                "network",
                &format!("{PROCESS_DATABASE_NAMESPACE}/{}", process.key()),
            ),
            meta,
            format: pm_dsd::format::JSON,
            data,
        });
    }
}

/// Returns the current time in Unix seconds.
pub fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}
