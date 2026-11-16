//! Inert nfqueue fallback for non-Linux platforms.
//!
//! Packet interception uses Linux `NFQUEUE` netlink sockets (Go
//! `interception/nfq`). The Windows kernel-extension backend from Go is not
//! ported. This module keeps the packet-consumer API available so that the
//! firewall compiles: [`channel`] and [`VerdictHandle::detached`] work for
//! in-process tests, but [`start`] always returns an error and no packets are
//! ever intercepted.

use std::io;
use std::time::Duration;

use tokio::sync::{mpsc, oneshot};

use crate::marks::Mark;
use crate::packet::Parsed;

/// A packet without verdict is dropped after this time.
pub const VERDICT_TIMEOUT: Duration = Duration::from_secs(20);
const HANDOFF_CAPACITY: usize = 1000;

/// An intercepted packet waiting for a verdict.
#[derive(Debug)]
pub struct QueuedPacket {
    pub queue: u16,
    pub id: u32,
    pub inbound: bool,
    pub info: Parsed,
    pub payload: Vec<u8>,
    pub verdict: VerdictHandle,
}

/// Sends the verdict for one packet. Dropping it without a verdict drops the packet.
#[derive(Debug)]
pub struct VerdictHandle {
    id: u32,
    tx: mpsc::UnboundedSender<(u32, Mark)>,
    sent: bool,
}

impl VerdictHandle {
    /// Creates a handle that is not connected to a kernel queue; the marks
    /// arrive on the returned receiver. For tests of packet consumers.
    pub fn detached(id: u32) -> (VerdictHandle, mpsc::UnboundedReceiver<(u32, Mark)>) {
        let (tx, rx) = mpsc::unbounded_channel();
        (
            VerdictHandle {
                id,
                tx,
                sent: false,
            },
            rx,
        )
    }

    /// Sets the mark (there is no kernel queue off Linux).
    pub fn set(mut self, mark: Mark) {
        self.sent = true;
        let _ = self.tx.send((self.id, mark));
    }
}

impl Drop for VerdictHandle {
    fn drop(&mut self) {
        if !self.sent {
            let _ = self.tx.send((self.id, Mark::Drop));
        }
    }
}

/// Stand-in for a running queue; no kernel socket is ever opened.
#[derive(Debug)]
pub struct QueueHandle {
    stop: Option<oneshot::Sender<()>>,
}

impl QueueHandle {
    pub async fn stop(mut self) -> io::Result<()> {
        if let Some(stop) = self.stop.take() {
            let _ = stop.send(());
        }
        Ok(())
    }
}

impl Drop for QueueHandle {
    fn drop(&mut self) {
        if let Some(stop) = self.stop.take() {
            let _ = stop.send(());
        }
    }
}

/// Never succeeds off Linux: there is no nfqueue socket to bind.
pub fn start(
    _num: u16,
    _inbound: bool,
    _out: mpsc::Sender<QueuedPacket>,
) -> io::Result<QueueHandle> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "packet interception is only available on Linux",
    ))
}

/// Channel capacity for handing packets to the firewall.
pub fn channel() -> (mpsc::Sender<QueuedPacket>, mpsc::Receiver<QueuedPacket>) {
    mpsc::channel(HANDOFF_CAPACITY)
}
