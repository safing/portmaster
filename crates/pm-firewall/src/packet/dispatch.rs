//! The dispatcher and the per-connection packet workers, a port of Go
//! `handlePacket` (`packet_handler.go`) and `HandlePacket` /
//! `packetHandlerWorker` (`network/connection_handler.go`).
//!
//! Go has one goroutine that receives all packets (`packetHandler`, started in
//! `module.go`). It looks up or creates the connection and puts the packet into
//! the connection's queue (100 packets, a full queue drops the packet). A
//! worker per connection takes the packets in order and runs the firewall
//! handler under the connection lock. Once the handler chain is finished
//! (`StopFirewallHandler`) the dispatcher applies the default handler to
//! further packets itself.
//!
//! The port keeps this model. Instead of one goroutine per connection, a
//! connection that has queued packets gets one drain job on an executor; the
//! job ends when the queue is empty, so idle connections cost nothing and
//! packets of one connection keep their order while connections run in
//! parallel.

use std::collections::{HashMap, VecDeque};
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use pm_interception::Verdict as PktVerdict;
use pm_netstate::UNDEFINED_PROCESS_ID;
use pm_network::{ProfileLookup, SharedConnection};

use super::env::{BandwidthSink, ConntrackControl, LayeredProfileLookup, PacketEnv};
use super::handler::{locked, Handler, HandlerCore};
use super::queued::QueuedPacket;
use crate::preauth::PreAuthPorts;

/// Size of the packet queue of a connection (Go `make(chan packet.Packet, 100)`).
const QUEUE_SIZE: usize = 100;
/// Stale handler states are looked for every this many new connections.
const PRUNE_EVERY: u64 = 1024;

/// Runs a job on a thread that may block. Use
/// `|job| { tokio::task::spawn_blocking(job); }` inside a tokio runtime.
pub type Executor = Arc<dyn Fn(Box<dyn FnOnce() + Send>) + Send + Sync>;

fn thread_executor() -> Executor {
    Arc::new(|job| {
        std::thread::Builder::new()
            .name("packet worker".to_string())
            .spawn(job)
            .expect("failed to spawn packet worker");
    })
}

struct Queue {
    /// The firewall handler; `None` after `StopFirewallHandler`.
    handler: Option<Handler>,
    packets: VecDeque<Box<dyn QueuedPacket>>,
    /// Go `pktQueueActive`: packets are queued instead of handled inline.
    active: bool,
    /// A drain job is scheduled or running.
    running: bool,
}

/// The firewall handler state and packet queue of one connection (the Go
/// fields `firewallHandler`, `pktQueue` and `pktQueueActive`). It is created
/// before the connection is handed to the worker, and removed when the handler
/// chain finishes.
struct ConnState {
    id: String,
    queue: Mutex<Queue>,
}

struct Shared {
    core: HandlerCore,
    states: Mutex<HashMap<String, Arc<ConnState>>>,
    /// Packets that are queued or being handled.
    inflight: AtomicUsize,
    shutdown: AtomicBool,
    created: AtomicU64,
}

/// Dispatches packets to the connection handlers.
///
/// # Calling convention
///
/// [`PacketHandler::dispatch`] must be called by **one task only** (Go: the
/// single `packetHandler` worker): it looks up or creates connections and
/// installs their handler state, which is only safe without concurrent
/// dispatchers. It never blocks on connection work: the handler chain
/// (socket lookups, prompts) runs on the executor. Typical use in the core:
///
/// ```ignore
/// while let Some(raw) = receiver.recv().await {
///     handler.dispatch(Box::new(NfqPacket::new(raw)));
/// }
/// ```
///
/// The core's other tasks (resets, bandwidth updates, cleaner) may use the
/// handler concurrently through `&PacketHandler`; call
/// [`PacketHandler::prune_handlers`] on the cleaner tick.
pub struct PacketHandler {
    shared: Arc<Shared>,
    executor: Executor,
}

impl std::ops::Deref for PacketHandler {
    type Target = HandlerCore;

    fn deref(&self) -> &HandlerCore {
        &self.shared.core
    }
}

impl PacketHandler {
    pub fn new(
        network: Arc<pm_network::Network>,
        env: Arc<dyn PacketEnv>,
        profiles: Arc<dyn LayeredProfileLookup>,
        preauth: Arc<PreAuthPorts>,
        conntrack: Arc<dyn ConntrackControl>,
    ) -> PacketHandler {
        PacketHandler {
            shared: Arc::new(Shared {
                core: HandlerCore::new(network, env, profiles, preauth, conntrack, None),
                states: Mutex::new(HashMap::new()),
                inflight: AtomicUsize::new(0),
                shutdown: AtomicBool::new(false),
                created: AtomicU64::new(0),
            }),
            executor: thread_executor(),
        }
    }

    #[must_use]
    pub fn with_bandwidth_sink(mut self, sink: Arc<dyn BandwidthSink>) -> PacketHandler {
        if let Some(shared) = Arc::get_mut(&mut self.shared) {
            shared.core.bandwidth = Some(sink);
        }
        self
    }

    /// Sets the executor of the connection workers. The default spawns a
    /// thread per drain job.
    #[must_use]
    pub fn with_executor(mut self, executor: Executor) -> PacketHandler {
        self.executor = executor;
        self
    }

    /// Go `handlePacket`: finds or creates the connection of the packet and
    /// hands the packet to it. Single caller only, see the type docs.
    pub fn dispatch(&self, pkt: Box<dyn QueuedPacket>) {
        let shared = &self.shared;
        let core = &shared.core;
        if shared.shutdown.load(Ordering::SeqCst) {
            return;
        }

        // A packet that already has a verdict (e.g. the default drop after the
        // timeout) cannot be influenced anymore.
        if pkt.verdict_set() {
            return;
        }
        core.pid_hints.rotate_if_due(Instant::now());

        let id = pkt.connection_id();
        let conn = match core.network.get_connection(&id) {
            Some(conn) => conn,
            None => {
                // Info-only packets can never complete a connection. Only keep
                // the process attribution for the first real packet.
                if pkt.info_only() {
                    if pkt.info().pid != UNDEFINED_PROCESS_ID {
                        core.pid_hints.save(&id, pkt.info().pid);
                    }
                    return;
                }
                self.create_connection(&id, pkt.as_ref())
            }
        };

        let mut state = locked(&shared.states).get(&id).cloned();
        if state.is_none() && !connection_is_decided(&conn) {
            // A connection that is still gathering data must never reach the
            // default handler, which would issue a verdict without a decision.
            state = Some(self.install_state(&id));
        }

        let Some(state) = state else {
            self.handle_inline(&conn, pkt);
            return;
        };

        let mut queue = locked(&state.queue);
        if !queue.active {
            drop(queue);
            self.handle_inline(&conn, pkt);
            return;
        }
        if queue.packets.len() >= QUEUE_SIZE {
            drop(queue);
            tracing::debug!(
                "filter: dropping packet of {id}, as there is no space in the connection's handling queue"
            );
            let _ = pkt.set_verdict(PktVerdict::Drop);
            return;
        }
        shared.inflight.fetch_add(1, Ordering::SeqCst);
        queue.packets.push_back(pkt);
        let start = !queue.running;
        queue.running = true;
        drop(queue);
        if start {
            let shared = Arc::clone(&self.shared);
            (self.executor)(Box::new(move || shared.drain(&conn, &state)));
        }
    }

    fn create_connection(&self, id: &str, pkt: &dyn QueuedPacket) -> SharedConnection {
        let core = &self.shared.core;
        let mut info = pkt.info().clone();
        if info.pid == UNDEFINED_PROCESS_ID {
            if let Some(pid) = core.pid_hints.take(id) {
                info.pid = pid;
            }
        }
        let conn = core.network.new_incomplete_connection(&info);
        locked(&conn).profiles = Arc::clone(&core.profiles) as Arc<dyn ProfileLookup>;
        self.install_state(id);
        if self.shared.created.fetch_add(1, Ordering::Relaxed) % PRUNE_EVERY == PRUNE_EVERY - 1 {
            self.prune_handlers();
        }
        conn
    }

    /// Go `SetFirewallHandler(fastTrackHandler)`.
    fn install_state(&self, id: &str) -> Arc<ConnState> {
        let state = Arc::new(ConnState {
            id: id.to_string(),
            queue: Mutex::new(Queue {
                handler: Some(Handler::FastTrack),
                packets: VecDeque::new(),
                active: true,
                running: false,
            }),
        });
        locked(&self.shared.states).insert(id.to_string(), Arc::clone(&state));
        state
    }

    /// Runs the default handler for a connection without active handler.
    /// Go does this on the dispatcher without taking the connection lock; the
    /// port takes it, and moves to the executor if it is busy.
    fn handle_inline(&self, conn: &SharedConnection, pkt: Box<dyn QueuedPacket>) {
        let shared = &self.shared;
        match conn.try_lock() {
            Ok(mut guard) => shared
                .core
                .handle_without_handler(conn, &mut guard, pkt.as_ref()),
            Err(std::sync::TryLockError::Poisoned(poisoned)) => {
                let mut guard = poisoned.into_inner();
                shared
                    .core
                    .handle_without_handler(conn, &mut guard, pkt.as_ref());
            }
            Err(std::sync::TryLockError::WouldBlock) => {
                shared.inflight.fetch_add(1, Ordering::SeqCst);
                let shared = Arc::clone(&self.shared);
                let conn = Arc::clone(conn);
                (self.executor)(Box::new(move || {
                    let mut guard = locked(&conn);
                    shared
                        .core
                        .handle_without_handler(&conn, &mut guard, pkt.as_ref());
                    drop(guard);
                    shared.inflight.fetch_sub(1, Ordering::SeqCst);
                }));
            }
        }
    }

    /// Number of connections with an active firewall handler.
    pub fn tracked_handlers(&self) -> usize {
        locked(&self.shared.states).len()
    }

    /// Drops the handler state and queued packets of connections that are not
    /// in the store anymore (e.g. removed by the cleaner while still
    /// gathering data). Called automatically for every 1024th new connection;
    /// the core should also call it on the network cleaner tick.
    pub fn prune_handlers(&self) {
        let stale: Vec<Arc<ConnState>> = {
            let states = locked(&self.shared.states);
            states
                .values()
                .filter(|state| self.shared.core.network.get_connection(&state.id).is_none())
                .cloned()
                .collect()
        };
        for state in stale {
            self.shared.close(&state);
        }
    }

    /// Stops the dispatcher and discards all queued packets (they get no
    /// verdict from us and are dropped by the queue). Go ends the workers with
    /// their context in the same way.
    pub fn shutdown(&self) {
        self.shared.shutdown.store(true, Ordering::SeqCst);
        let states: Vec<Arc<ConnState>> = locked(&self.shared.states).values().cloned().collect();
        for state in states {
            self.shared.close(&state);
        }
    }

    /// Waits until no packet is queued or being handled. Returns false on
    /// timeout. For tests and orderly shutdown.
    pub fn wait_idle(&self, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        while self.shared.inflight.load(Ordering::SeqCst) > 0 {
            if Instant::now() >= deadline {
                return false;
            }
            std::thread::sleep(Duration::from_millis(1));
        }
        true
    }
}

fn connection_is_decided(conn: &SharedConnection) -> bool {
    match conn.try_lock() {
        Ok(conn) => conn.data_complete,
        Err(std::sync::TryLockError::Poisoned(poisoned)) => poisoned.into_inner().data_complete,
        // Someone else holds it (reset, bandwidth); a decided connection.
        Err(std::sync::TryLockError::WouldBlock) => true,
    }
}

impl Shared {
    /// The worker: handles the queued packets of a connection in order.
    fn drain(&self, conn: &SharedConnection, state: &Arc<ConnState>) {
        loop {
            let pkt = {
                let mut queue = locked(&state.queue);
                match queue.packets.pop_front() {
                    Some(pkt) => pkt,
                    None => {
                        queue.running = false;
                        return;
                    }
                }
            };
            if !self.shutdown.load(Ordering::SeqCst) {
                self.handle_queued(conn, state, pkt.as_ref());
            }
            self.inflight.fetch_sub(1, Ordering::SeqCst);
        }
    }

    /// Go `packetHandlerHandleConn`.
    fn handle_queued(
        &self,
        conn: &SharedConnection,
        state: &Arc<ConnState>,
        pkt: &dyn QueuedPacket,
    ) {
        // Skip packets that received a verdict while waiting in the queue.
        if pkt.verdict_set() {
            return;
        }

        let mut guard = locked(conn);
        guard.last_seen = self.core.network.env().clock.now();

        let current = locked(&state.queue).handler;
        let mut handler = current;
        self.core.run_handler(conn, &mut guard, pkt, &mut handler);

        if handler != current {
            let mut queue = locked(&state.queue);
            queue.handler = handler;
            if handler.is_none() {
                // Go `StopFirewallHandler`: queued packets are still handled by
                // the worker, new ones are handled by the dispatcher.
                queue.active = false;
                drop(queue);
                self.forget(state);
            }
        }

        // Go only pushes changes for connections that have a handler.
        if current.is_some() && guard.save_when_finished {
            guard.save_when_finished = false;
            self.core.network.save_locked(conn, &mut guard);
        }
    }

    fn forget(&self, state: &Arc<ConnState>) {
        let mut states = locked(&self.states);
        if states.get(&state.id).is_some_and(|s| Arc::ptr_eq(s, state)) {
            states.remove(&state.id);
        }
    }

    /// Deactivates the queue of a connection and drops its waiting packets.
    fn close(&self, state: &Arc<ConnState>) {
        let dropped = {
            let mut queue = locked(&state.queue);
            queue.active = false;
            queue.handler = None;
            let dropped = queue.packets.len();
            queue.packets.clear();
            dropped
        };
        self.inflight.fetch_sub(dropped, Ordering::SeqCst);
        self.forget(state);
    }
}

impl HandlerCore {
    /// The default handler for a connection without firewall handler; no
    /// saving, like Go.
    fn handle_without_handler(
        &self,
        conn: &SharedConnection,
        guard: &mut pm_network::Connection,
        pkt: &dyn QueuedPacket,
    ) {
        guard.last_seen = self.network.env().clock.now();
        if pkt.verdict_set() {
            return;
        }
        let mut handler = None;
        self.run_handler(conn, guard, pkt, &mut handler);
    }
}
