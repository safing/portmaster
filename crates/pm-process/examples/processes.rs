//! Prints details of PID 1 and of the current process, read from `/proc`.
//!
//! Run with `cargo run -p pm-process --example processes`.

use pm_netstate::ProcFs;
use pm_process::{ProcReader, ProcessCache};

fn main() {
    let reader = ProcReader::new(ProcFs::default());
    for pid in [1, std::process::id() as i32] {
        match reader.read(pid) {
            Ok(info) => println!(
                "pid {}: name={:?} user={:?} exe={:?} ppid={} pgrp={} created_at={}ms cmdline={:?}",
                info.pid,
                info.name,
                info.username,
                info.exe,
                info.ppid,
                info.pgrp,
                info.created_at,
                info.cmdline
            ),
            Err(error) => println!("pid {pid}: {error}"),
        }
    }

    let cache = ProcessCache::new(ProcReader::new(ProcFs::default()));
    let self_pid = std::process::id() as i32;
    match cache.get_with_leader(self_pid) {
        Ok((process, leader)) => println!(
            "cache: {} key={} user={} leader={} exe={:?} tags={}",
            process.name,
            process.key(),
            process.user_name,
            leader
                .as_ref()
                .map(|leader| leader.pid.to_string())
                .unwrap_or_else(|| "-".to_string()),
            process.exec_name,
            process.tags.len()
        ),
        Err(error) => println!("cache: {error}"),
    }

    println!("known processes: {}", cache.all().len());
}
