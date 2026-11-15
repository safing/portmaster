#![allow(dead_code)] // Not every test binary uses every helper.

//! Shared helpers: fake process resolver, entity factory, environment.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{cache_interface, Entity, FilterLists, StaticLocations};
use pm_network::{Clock, Environment, ManualClock, Network, PacketInfo, ProcessResolver};
use pm_process::Process;

pub const START: i64 = 1_700_000_000;

pub fn new_entity_factory() -> Arc<dyn Fn() -> Entity + Send + Sync> {
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected("cache", "Cache").unwrap();
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
        .unwrap();
    let lists = Arc::new(FilterLists::with_capacity(
        cache_interface(&databases),
        100,
        10,
        5,
        20,
        20,
    ));
    let locations = Arc::new(StaticLocations::new());
    Arc::new(move || Entity::new(Arc::clone(&lists), locations.clone()))
}

pub fn process(pid: i32, name: &str) -> Arc<Process> {
    let mut process = Process::empty(&format!("{pid}-1"));
    process.pid = pid;
    process.name = name.to_string();
    process.path = format!("/usr/bin/{name}");
    process.user_name = "user".to_string();
    process.created_at = 1;
    Arc::new(process)
}

/// A resolver with fixed answers.
#[derive(Default)]
pub struct FakeResolver {
    /// PID by local port.
    pub pids: Mutex<HashMap<u16, i32>>,
    pub processes: Mutex<HashMap<i32, Arc<Process>>>,
    /// Connection existence by local port; missing means gone.
    pub alive: Mutex<HashMap<u16, bool>>,
    pub controller_set: Mutex<bool>,
}

impl FakeResolver {
    pub fn add_process(&self, pid: i32, name: &str) {
        self.processes
            .lock()
            .unwrap()
            .insert(pid, process(pid, name));
    }
}

impl ProcessResolver for FakeResolver {
    fn pid_of_connection(&self, info: &mut PacketInfo) -> (i32, bool) {
        let pid = self
            .pids
            .lock()
            .unwrap()
            .get(&info.local_port())
            .copied()
            .unwrap_or(-2);
        (pid, info.inbound)
    }

    fn process(&self, pid: i32) -> Arc<Process> {
        self.processes
            .lock()
            .unwrap()
            .get(&pid)
            .cloned()
            .unwrap_or_else(|| process(-2, "Other Connections"))
    }

    fn network_host(&self, ip: IpAddr) -> Arc<Process> {
        process(-255, &format!("host-{ip}"))
    }

    fn exists(&self, info: &PacketInfo, _now: SystemTime) -> bool {
        self.alive
            .lock()
            .unwrap()
            .get(&info.src_port)
            .copied()
            .unwrap_or(false)
    }

    fn process_from_storage(&self, key: &str) -> Option<Arc<Process>> {
        self.processes
            .lock()
            .unwrap()
            .values()
            .find(|process| process.key() == key)
            .cloned()
    }

    fn all_processes(&self) -> Vec<Arc<Process>> {
        self.processes.lock().unwrap().values().cloned().collect()
    }

    fn set_db_controller(&self, _controller: Arc<pm_database::Controller>) {
        *self.controller_set.lock().unwrap() = true;
    }
}

pub struct Setup {
    pub network: Arc<Network>,
    pub clock: Arc<ManualClock>,
    pub resolver: Arc<FakeResolver>,
}

pub fn setup() -> Setup {
    let clock = Arc::new(ManualClock::new(START));
    let resolver = Arc::new(FakeResolver::default());
    let env = Environment::new(resolver.clone(), new_entity_factory())
        .with_clock(clock.clone() as Arc<dyn Clock>);
    Setup {
        network: Network::new(env),
        clock,
        resolver,
    }
}

pub fn ip(s: &str) -> IpAddr {
    s.parse().unwrap()
}
