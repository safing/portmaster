//! nfqueue reader, port of Go `interception/nfq/nfq.go`.
//!
//! Each queue runs in one tokio task that owns the netlink socket. Packets
//! are handed out with a [`VerdictHandle`]; verdicts travel back to the task,
//! which keeps the kernel messages. Unanswered packets are dropped after
//! [`VERDICT_TIMEOUT`], like Go.

use std::collections::HashMap;
use std::io::ErrorKind;
use std::time::{Duration, Instant};

use tokio::io::unix::AsyncFd;
use tokio::sync::{mpsc, oneshot};

use crate::marks::Mark;
use crate::packet::{parse_layer3, Parsed};

/// A packet without verdict is dropped after this time.
pub const VERDICT_TIMEOUT: Duration = Duration::from_secs(20);
const MAX_PACKET_LEN: u16 = 1600;
const MAX_QUEUE_LEN: u32 = 0xffff;
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
    /// Sets the mark (the kernel verdict is always accept).
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

/// A running queue; stops when dropped or on [`QueueHandle::stop`].
pub struct QueueHandle {
    stop: Option<oneshot::Sender<()>>,
    task: tokio::task::JoinHandle<std::io::Result<()>>,
}

impl QueueHandle {
    pub async fn stop(mut self) -> std::io::Result<()> {
        if let Some(stop) = self.stop.take() {
            let _ = stop.send(());
        }
        match (&mut self.task).await {
            Ok(result) => result,
            Err(e) => Err(std::io::Error::other(e)),
        }
    }
}

impl Drop for QueueHandle {
    fn drop(&mut self) {
        if let Some(stop) = self.stop.take() {
            let _ = stop.send(());
        }
    }
}

/// Opens and binds the queue; packets go to `out`.
pub fn start(
    num: u16,
    inbound: bool,
    out: mpsc::Sender<QueuedPacket>,
) -> std::io::Result<QueueHandle> {
    let mut queue = nfq::Queue::open()?;
    queue.bind(num)?;
    queue.set_copy_range(num, MAX_PACKET_LEN)?;
    queue.set_queue_max_len(num, MAX_QUEUE_LEN)?;
    queue.set_nonblocking(true);
    let fd = register(queue)?;
    let (stop_tx, stop_rx) = oneshot::channel();
    let task = tokio::spawn(run(fd, num, inbound, out, stop_rx));
    Ok(QueueHandle {
        stop: Some(stop_tx),
        task,
    })
}

/// The only `unsafe` in the crate (allowed by the user for this spot).
#[allow(unsafe_code)]
fn register(queue: nfq::Queue) -> std::io::Result<AsyncFd<nfq::Queue>> {
    // SAFETY: `nfq::Queue` owns its netlink socket as an `OwnedFd` field and
    // `as_raw_fd` always returns that fd. The queue is moved into the AsyncFd,
    // so the fd stays open and unchanged for the AsyncFd's whole lifetime.
    unsafe { AsyncFd::register(queue) }.map_err(|e| e.into_parts().1)
}

/// Channel capacity for handing packets to the firewall.
pub fn channel() -> (mpsc::Sender<QueuedPacket>, mpsc::Receiver<QueuedPacket>) {
    mpsc::channel(HANDOFF_CAPACITY)
}

async fn run(
    mut fd: AsyncFd<nfq::Queue>,
    num: u16,
    inbound: bool,
    out: mpsc::Sender<QueuedPacket>,
    mut stop: oneshot::Receiver<()>,
) -> std::io::Result<()> {
    let (verdict_tx, mut verdict_rx) = mpsc::unbounded_channel::<(u32, Mark)>();
    let mut pending: HashMap<u32, (nfq::Message, Instant)> = HashMap::new();
    let mut sweep = tokio::time::interval(Duration::from_secs(1));

    loop {
        tokio::select! {
            _ = &mut stop => break,
            Some((id, mark)) = verdict_rx.recv() => {
                if let Some((msg, _)) = pending.remove(&id) {
                    send_verdict(fd.get_mut(), msg, mark, num)?;
                }
            }
            _ = sweep.tick() => {
                let now = Instant::now();
                let expired: Vec<u32> = pending
                    .iter()
                    .filter(|(_, (_, seen))| now.duration_since(*seen) > VERDICT_TIMEOUT)
                    .map(|(id, _)| *id)
                    .collect();
                for id in expired {
                    if let Some((msg, _)) = pending.remove(&id) {
                        tracing::warn!(queue = num, id, "nfqueue: no verdict set after 20s, dropping");
                        send_verdict(fd.get_mut(), msg, Mark::Drop, num)?;
                    }
                }
            }
            guard = fd.readable_mut() => {
                let mut guard = guard?;
                loop {
                    let msg = match guard.get_inner_mut().recv() {
                        Ok(msg) => msg,
                        Err(e) if e.kind() == ErrorKind::WouldBlock => {
                            guard.clear_ready();
                            break;
                        }
                        // The kernel drops packets when our socket buffer is full; keep going.
                        Err(e) if e.raw_os_error() == Some(105) => {
                            tracing::warn!(queue = num, "nfqueue: receive buffer overrun");
                            continue;
                        }
                        Err(e) => return Err(e),
                    };
                    let id = msg.get_packet_id();
                    let payload = msg.get_payload().to_vec();
                    let info = match parse_layer3(&payload) {
                        Ok(info) => info,
                        Err(e) => {
                            tracing::warn!(queue = num, id, error = %e, "nfqueue: failed to parse payload");
                            send_verdict(guard.get_inner_mut(), msg, Mark::Drop, num)?;
                            continue;
                        }
                    };
                    let packet = QueuedPacket {
                        queue: num,
                        id,
                        inbound,
                        info,
                        payload,
                        verdict: VerdictHandle { id, tx: verdict_tx.clone(), sent: false },
                    };
                    match out.try_send(packet) {
                        Ok(()) => {
                            pending.insert(id, (msg, Instant::now()));
                        }
                        Err(_) => {
                            tracing::warn!(queue = num, id, "nfqueue: failed to queue packet, dropping");
                            send_verdict(guard.get_inner_mut(), msg, Mark::Drop, num)?;
                        }
                    }
                }
            }
        }
    }

    // Unanswered packets must not stay in the kernel queue.
    for (_, (msg, _)) in pending.drain() {
        send_verdict(fd.get_mut(), msg, Mark::Drop, num)?;
    }
    let _ = fd.get_mut().unbind(num);
    Ok(())
}

fn send_verdict(
    queue: &mut nfq::Queue,
    mut msg: nfq::Message,
    mark: Mark,
    num: u16,
) -> std::io::Result<()> {
    msg.set_nfmark(mark as u32);
    msg.set_verdict(nfq::Verdict::Accept);
    if let Err(e) = queue.verdict(msg) {
        tracing::error!(queue = num, mark = mark.name(), error = %e, "nfqueue: failed to set verdict");
        return Err(e);
    }
    Ok(())
}
