#![allow(dead_code)] // Not every test binary uses every helper.

//! Helpers for building a fake proc tree.

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use pm_netstate::ProcFs;
use pm_process::{ProcReader, ProcessCache};

pub fn temp_dir(tag: &str) -> PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-process-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let _ = fs::remove_dir_all(&path);
    fs::create_dir_all(&path).unwrap();
    path
}

pub fn current_uid() -> u32 {
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        std::fs::metadata(".").map(|m| m.uid()).unwrap_or(1000)
    }
    #[cfg(not(unix))]
    {
        1000
    }
}

pub fn write_file(path: &Path, content: impl AsRef<[u8]>) {
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    fs::write(path, content).unwrap();
}

/// Writes `/proc/stat` with the given boot time.
pub fn set_boot_time(root: &Path, boot_time: i64) {
    write_file(
        &root.join("stat"),
        format!("cpu  1 2 3 4\nbtime {boot_time}\nprocesses 1234\n"),
    );
}

/// Builds a `/proc/<pid>/stat` line with the given process group and start time.
pub fn stat_line(pid: i32, name: &str, ppid: i32, pgrp: i32, start_ticks: i64) -> String {
    // Field 3 is the state, 4 the ppid, 5 the pgrp and 22 the start time.
    let mut values: Vec<String> = (1..=18).map(|value| value.to_string()).collect();
    values[0] = ppid.to_string();
    values[1] = pgrp.to_string();

    let mut line = format!("{pid} ({name}) S {}", values.join(" "));
    line.push(' ');
    line.push_str(&start_ticks.to_string());
    line.push_str(" 100 0 0 10 0");
    line
}

/// Adds a process with stat, status, cmdline, environ, exe and cwd.
pub fn add_process(
    root: &Path,
    pid: i32,
    ppid: i32,
    pgrp: i32,
    start_ticks: i64,
    name: &str,
    exe: &str,
) {
    let dir = root.join(pid.to_string());
    write_file(
        &dir.join("stat"),
        stat_line(pid, name, ppid, pgrp, start_ticks),
    );
    write_file(
        &dir.join("status"),
        format!("Name:\t{name}\nUid:\t1000\t1000\t1000\t1000\n"),
    );
    write_file(&dir.join("cmdline"), b"myproc\0--flag\0");
    write_file(
        &dir.join("environ"),
        b"HOME=/home/test\0PATH=/usr/bin\0QUOTED=\"value\"\0",
    );
    let _ = std::os::unix::fs::symlink(exe, dir.join("exe"));
    let _ = std::os::unix::fs::symlink("/tmp", dir.join("cwd"));
}

/// Writes a passwd file with a single test user.
pub fn write_passwd(root: &Path) -> PathBuf {
    let path = root.join("etc/passwd");
    write_file(
        &path,
        "root:x:0:0:root:/root:/bin/sh\ntestuser:x:1000:1000:Test:/home/test:/bin/sh\n",
    );
    path
}

/// Makes the process the owner of the given socket inode.
pub fn add_socket_owner(root: &Path, pid: i32, inode: i64) {
    let fd_dir = root.join(pid.to_string()).join("fd");
    fs::create_dir_all(&fd_dir).unwrap();
    let _ = std::os::unix::fs::symlink(format!("socket:[{inode}]"), fd_dir.join("7"));
}

/// Creates a process cache for the fake proc tree.
pub fn cache(root: &Path, passwd: &Path) -> Arc<ProcessCache> {
    ProcessCache::new(ProcReader::with_passwd(ProcFs::new(root), passwd))
}

pub const TABLE_HEADER: &str = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode";

/// Writes a `/proc/net/tcp` table with the given lines.
pub fn write_tcp_table(root: &Path, lines: &[String]) {
    let mut data = String::from(TABLE_HEADER);
    data.push('\n');
    for line in lines {
        data.push_str(line);
        data.push('\n');
    }
    write_file(&root.join("net/tcp"), data);
}
