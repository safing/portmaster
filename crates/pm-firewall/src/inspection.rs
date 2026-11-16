//! Port of Go `service/firewall/inspection/inspection.go`.
//!
//! A small registry of traffic inspectors that runs on a packet before the
//! final verdict is issued. The Go original keeps the registry in package
//! globals; here the state lives in an [`InspectorRegistry`] that can be
//! injected into the packet environment. [`global_registry`] mirrors the Go
//! globals for callers that do not want to own an instance.
//!
//! Action values and verdict handling follow Go exactly, including the
//! per-connection `activeInspectors` flags (once an inspector has finished a
//! connection it is skipped on later packets).

use std::sync::{Arc, Mutex, OnceLock};

use pm_network::{Connection, Verdict};

use crate::packet::QueuedPacket;

/// An action returned by an inspector. The discriminants are the values of
/// Go's `uint8` iota constants and must not change.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum InspectorAction {
    DoNothing = 0,
    BlockPacket = 1,
    DropPacket = 2,
    BlockConn = 3,
    DropConn = 4,
    StopInspecting = 5,
}

impl InspectorAction {
    /// Go `DO_NOTHING`.
    pub const DO_NOTHING: Self = Self::DoNothing;
    /// Go `BLOCK_PACKET`.
    pub const BLOCK_PACKET: Self = Self::BlockPacket;
    /// Go `DROP_PACKET`.
    pub const DROP_PACKET: Self = Self::DropPacket;
    /// Go `BLOCK_CONN`.
    pub const BLOCK_CONN: Self = Self::BlockConn;
    /// Go `DROP_CONN`.
    pub const DROP_CONN: Self = Self::DropConn;
    /// Go `STOP_INSPECTING`.
    pub const STOP_INSPECTING: Self = Self::StopInspecting;

    /// Converts a numeric action, like a Go `uint8` return value.
    pub fn from_u8(value: u8) -> Option<Self> {
        Some(match value {
            0 => Self::DoNothing,
            1 => Self::BlockPacket,
            2 => Self::DropPacket,
            3 => Self::BlockConn,
            4 => Self::DropConn,
            5 => Self::StopInspecting,
            _ => return None,
        })
    }

    /// Returns the action as its Go numeric value.
    pub fn as_u8(self) -> u8 {
        self as u8
    }
}

/// Go `type inspectorFn func(*network.Connection, packet.Packet) uint8`.
pub type InspectorFn = dyn Fn(&mut Connection, &dyn QueuedPacket) -> InspectorAction + Send + Sync;

struct RegisteredInspector {
    #[allow(dead_code)]
    name: String,
    inspect_verdict: Verdict,
    function: Box<InspectorFn>,
}

/// Registry of traffic inspectors, the injectable counterpart of the Go
/// package globals `inspectors`, `inspectorNames` and `inspectVerdicts`.
///
/// Cloning the `Arc`s before calling inspectors means an inspector may
/// register another one without deadlocking. Go left the registration mutex
/// commented out in `RunInspectors`, which has the same effect.
#[derive(Default)]
pub struct InspectorRegistry {
    inspectors: Mutex<Vec<Arc<RegisteredInspector>>>,
}

impl InspectorRegistry {
    /// Creates an empty registry.
    pub fn new() -> InspectorRegistry {
        InspectorRegistry::default()
    }

    /// Registers an inspector and returns its index, like Go
    /// `RegisterInspector`.
    pub fn register_inspector<F>(
        &self,
        name: impl Into<String>,
        inspector: F,
        inspect_verdict: Verdict,
    ) -> usize
    where
        F: Fn(&mut Connection, &dyn QueuedPacket) -> InspectorAction + Send + Sync + 'static,
    {
        let index = {
            let mut inspectors = self.inspectors.lock().unwrap_or_else(|e| e.into_inner());
            let index = inspectors.len();
            inspectors.push(Arc::new(RegisteredInspector {
                name: name.into(),
                inspect_verdict,
                function: Box::new(inspector),
            }));
            index
        };
        index
    }

    /// Returns the number of registered inspectors.
    pub fn len(&self) -> usize {
        self.inspectors
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .len()
    }

    /// Returns whether the registry is empty.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns the names in registration order (Go `inspectorNames`).
    pub fn names(&self) -> Vec<String> {
        self.inspectors
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .iter()
            .map(|inspector| inspector.name.clone())
            .collect()
    }

    /// Runs all applicable inspectors on the given packet, like Go
    /// `RunInspectors`. Returns the resulting verdict and whether inspection
    /// should continue on the next packet of the connection.
    pub fn run_inspectors(
        &self,
        conn: &mut Connection,
        pkt: &dyn QueuedPacket,
    ) -> (Verdict, bool) {
        let inspectors = {
            let guard = self.inspectors.lock().unwrap_or_else(|e| e.into_inner());
            guard.clone()
        };

        if conn.active_inspectors.is_empty() {
            conn.active_inspectors = vec![false; inspectors.len()];
        }
        // Go also initializes `inspectorData` to an empty map here. A Rust
        // `BTreeMap` needs no explicit initialization; inspectors may use the
        // field directly.

        let mut continue_inspection = false;
        let mut verdict = Verdict::Undecided;

        for (key, inspector) in inspectors.iter().enumerate() {
            if key >= conn.active_inspectors.len() {
                break;
            }
            if conn.active_inspectors[key] {
                continue;
            }

            // Stop inspecting a connection once its verdict is past the
            // criteria of this inspector.
            if conn.verdict > inspector.inspect_verdict {
                conn.active_inspectors[key] = true;
                continue;
            }

            let action = (inspector.function)(conn, pkt);
            match action {
                InspectorAction::DoNothing => {
                    if verdict < Verdict::Accept {
                        verdict = Verdict::Accept;
                    }
                    continue_inspection = true;
                }
                InspectorAction::BlockPacket => {
                    if verdict < Verdict::Block {
                        verdict = Verdict::Block;
                    }
                    continue_inspection = true;
                }
                InspectorAction::DropPacket => {
                    verdict = Verdict::Drop;
                    continue_inspection = true;
                }
                InspectorAction::BlockConn => {
                    conn.set_verdict(Verdict::Block, "", "", None);
                    verdict = conn.verdict;
                    conn.active_inspectors[key] = true;
                }
                InspectorAction::DropConn => {
                    conn.set_verdict(Verdict::Drop, "", "", None);
                    verdict = conn.verdict;
                    conn.active_inspectors[key] = true;
                }
                InspectorAction::StopInspecting => {
                    conn.active_inspectors[key] = true;
                }
            }
        }

        (verdict, continue_inspection)
    }
}

static GLOBAL: OnceLock<InspectorRegistry> = OnceLock::new();

/// The process-wide registry, counterpart of the Go package globals.
pub fn global_registry() -> &'static InspectorRegistry {
    GLOBAL.get_or_init(InspectorRegistry::new)
}

/// Registers an inspector in the process-wide registry.
pub fn register_inspector<F>(
    name: impl Into<String>,
    inspector: F,
    inspect_verdict: Verdict,
) -> usize
where
    F: Fn(&mut Connection, &dyn QueuedPacket) -> InspectorAction + Send + Sync + 'static,
{
    global_registry().register_inspector(name, inspector, inspect_verdict)
}

/// Runs the inspectors of the process-wide registry.
pub fn run_inspectors(conn: &mut Connection, pkt: &dyn QueuedPacket) -> (Verdict, bool) {
    global_registry().run_inspectors(conn, pkt)
}
