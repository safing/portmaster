//! Reading process details from the proc file system.

use std::collections::BTreeMap;
use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;

use pm_netstate::ProcFs;

use crate::UNDEFINED_PROCESS_ID;

/// Clock ticks per second used by the kernel; matches Go `gopsutil.ClockTicks`.
const CLOCK_TICKS: i64 = 100;

/// Errors of the proc reader.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("process {0} does not exist")]
    NoSuchProcess(i32),
    #[error("could not parse {0}")]
    Parse(String),
}

/// Result type of the proc reader.
pub type Result<T> = std::result::Result<T, Error>;

/// Details of a process, as read from the proc file system.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ProcInfo {
    pub pid: i32,
    pub ppid: i32,
    /// Process group of the process.
    pub pgrp: i32,
    /// Creation time in milliseconds since the Unix epoch, like Go's
    /// `gopsutil.CreateTime`.
    pub created_at: i64,
    pub uid: i32,
    pub username: String,
    /// Name from `/proc/<pid>/stat` (the comm field).
    pub name: String,
    pub exe: String,
    pub cwd: String,
    pub cmdline: String,
    pub environ: BTreeMap<String, String>,
}

/// Reads process details from a proc root.
pub struct ProcReader {
    procfs: ProcFs,
    passwd: PathBuf,
    boot_time: Mutex<Option<i64>>,
}

impl ProcReader {
    /// Creates a reader that resolves usernames through `/etc/passwd`.
    pub fn new(procfs: ProcFs) -> ProcReader {
        ProcReader::with_passwd(procfs, "/etc/passwd")
    }

    /// Creates a reader with a custom passwd file, for tests.
    pub fn with_passwd(procfs: ProcFs, passwd: impl Into<PathBuf>) -> ProcReader {
        ProcReader {
            procfs,
            passwd: passwd.into(),
            boot_time: Mutex::new(None),
        }
    }

    pub fn procfs(&self) -> &ProcFs {
        &self.procfs
    }

    /// Reads the details of a process.
    pub fn read(&self, pid: i32) -> Result<ProcInfo> {
        let dir = self.procfs.root().join(pid.to_string());

        let stat = fs::read_to_string(dir.join("stat")).map_err(|error| {
            if error.kind() == std::io::ErrorKind::NotFound {
                Error::NoSuchProcess(pid)
            } else {
                Error::Io(error)
            }
        })?;
        let stat = parse_stat(&stat)?;

        let status = fs::read_to_string(dir.join("status")).unwrap_or_default();
        let uid = parse_uid(&status).unwrap_or(UNDEFINED_PROCESS_ID);

        let exe = fs::read_link(dir.join("exe"))
            .map_err(Error::Io)?
            .to_string_lossy()
            .to_string();
        let exe = exe.trim_end_matches(" (deleted)").to_string();

        let cwd = fs::read_link(dir.join("cwd"))
            .map(|path| path.to_string_lossy().to_string())
            .unwrap_or_default();

        let cmdline = read_list(&dir.join("cmdline"));
        let environ = read_environ(&dir.join("environ"));

        Ok(ProcInfo {
            pid,
            ppid: stat.ppid,
            pgrp: stat.pgrp,
            created_at: (stat.start_ticks / CLOCK_TICKS + self.boot_time()) * 1000,
            uid,
            username: self.username(uid),
            name: stat.name,
            exe,
            cwd,
            cmdline,
            environ,
        })
    }

    /// Returns the boot time of the system in seconds.
    fn boot_time(&self) -> i64 {
        let mut cached = self.boot_time.lock().expect("lock poisoned");
        if let Some(boot_time) = *cached {
            return boot_time;
        }
        let boot_time = fs::read_to_string(self.procfs.root().join("stat"))
            .ok()
            .and_then(|stat| {
                stat.lines()
                    .find_map(|line| line.strip_prefix("btime "))
                    .and_then(|value| value.trim().parse::<i64>().ok())
            })
            .unwrap_or(0);
        *cached = Some(boot_time);
        boot_time
    }

    /// Resolves a UID to a username, like Go `user.LookupId`.
    fn username(&self, uid: i32) -> String {
        let Ok(passwd) = fs::read_to_string(&self.passwd) else {
            return String::new();
        };
        for line in passwd.lines() {
            let fields: Vec<&str> = line.split(':').collect();
            if fields.len() >= 3 && fields[2].parse::<i32>().ok() == Some(uid) {
                return fields[0].to_string();
            }
        }
        String::new()
    }
}

struct Stat {
    name: String,
    ppid: i32,
    pgrp: i32,
    start_ticks: i64,
}

/// Parses `/proc/<pid>/stat`, handling names with spaces and parentheses.
fn parse_stat(stat: &str) -> Result<Stat> {
    let Some(name_start) = stat.find('(') else {
        return Err(Error::Parse("stat: missing process name".to_string()));
    };
    let Some(name_end) = stat.rfind(')') else {
        return Err(Error::Parse("stat: missing process name".to_string()));
    };
    if name_end <= name_start {
        return Err(Error::Parse("stat: invalid process name".to_string()));
    }
    let name = stat[name_start + 1..name_end].to_string();

    // Fields after the name start with the state (overall field 3).
    let rest: Vec<&str> = stat[name_end + 1..].split_whitespace().collect();
    let parse = |index: usize, field: &str| -> Result<i64> {
        rest.get(index)
            .ok_or_else(|| Error::Parse(format!("stat: missing field {field}")))?
            .parse::<i64>()
            .map_err(|_| Error::Parse(format!("stat: invalid field {field}")))
    };

    Ok(Stat {
        name,
        ppid: parse(1, "ppid")? as i32,
        pgrp: parse(2, "pgrp")? as i32,
        start_ticks: parse(19, "starttime")?,
    })
}

/// Parses the first value of the `Uid:` line of `/proc/<pid>/status`.
fn parse_uid(status: &str) -> Option<i32> {
    status
        .lines()
        .find_map(|line| line.strip_prefix("Uid:"))
        .and_then(|values| values.split_whitespace().next())
        .and_then(|uid| uid.parse::<i32>().ok())
}

/// Reads a NUL-separated list and joins it with spaces like gopsutil.
fn read_list(path: &std::path::Path) -> String {
    let Ok(data) = fs::read(path) else {
        return String::new();
    };
    data.split(|&byte| byte == 0)
        .filter(|part| !part.is_empty())
        .map(|part| String::from_utf8_lossy(part).into_owned())
        .collect::<Vec<String>>()
        .join(" ")
}

/// Reads a NUL-separated environment into a map.
fn read_environ(path: &std::path::Path) -> BTreeMap<String, String> {
    let Ok(data) = fs::read(path) else {
        return BTreeMap::new();
    };
    let mut env = BTreeMap::new();
    for entry in data.split(|&byte| byte == 0) {
        if entry.is_empty() {
            continue;
        }
        let entry = String::from_utf8_lossy(entry);
        if let Some((key, value)) = entry.split_once('=') {
            env.insert(
                key.trim_matches(|c| c == '"' || c == '\'').to_string(),
                value.trim_matches(|c| c == '"' || c == '\'').to_string(),
            );
        }
    }
    env
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_stat_with_odd_names() {
        let line =
            "123 (my (weird) name) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 999 23 24 25";
        let stat = parse_stat(line).unwrap();
        assert_eq!(stat.name, "my (weird) name");
        assert_eq!(stat.ppid, 1);
        assert_eq!(stat.pgrp, 2);
        assert_eq!(stat.start_ticks, 999);
    }

    #[test]
    fn parses_uid() {
        let status = "Name:\ttest\nUid:\t1000\t1001\t1002\t1003\n";
        assert_eq!(parse_uid(status), Some(1000));
        assert_eq!(parse_uid("Name:\ttest\n"), None);
    }

    #[test]
    fn malformed_stat_is_rejected() {
        assert!(parse_stat("").is_err());
        assert!(parse_stat("123 (no end").is_err());
        assert!(parse_stat("123 (short) S 1").is_err());
    }
}
