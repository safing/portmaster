//! Maps a bound local endpoint to the process that owns it.
//!
//! The inbound IP packet layer (FWPM_LAYER_INBOUND_IPPACKET_V4/V6) sits just
//! after the IP header has been parsed and before any network layer processing.
//! No socket is associated with the packet at that point, so WFP does not supply
//! FWPS_METADATA_FIELD_PROCESS_ID and the packet layer had no choice but to
//! record process ID 0 for every inbound connection it created.
//!
//! The owning PID *is* available earlier, at the resource assignment (bind)
//! layer. This cache is filled there and read back at the packet layer.
//!
//! Verified on Windows 11 with a listener on 0.0.0.0:1234: the bind was
//! indicated with the correct PID 4.4 seconds before the first datagram reached
//! the packet layer, so the entry is always in place by the time it is needed.
//!
//! Keyed on (address family, protocol, local port).
//!
//! The address itself cannot be part of the key: a bind to a wildcard address
//! leaves the local address field as FWP_EMPTY, so it is simply not available.
//!
//! The address *family* must be, though. IPv4 and IPv6 port spaces are
//! independent, and two unrelated processes can hold the same port number at the
//! same time - one on 0.0.0.0, one on [::]. Measured with two listeners on port
//! 1234 (PID 6552 on v4, PID 5824 on v6): keying on the port alone made the
//! second bind overwrite the first, and the v4 connection was then reported with
//! the v6 listener's PID.
//!
//! Scope: only endpoints whose port the application named itself, i.e. listening
//! services. Stack-assigned ephemeral ports are deliberately not tracked - the
//! outbound connections that use them are classified at the ALE connect layer,
//! which already has the process ID.
//!
//! Storage is a flat array with one slot per possible endpoint, allocated once.
//! The key space is small and fully bounded - 2 families x 2 protocols x 65536
//! ports, at 4 bytes per slot, so 1 MB - which removes the need for any entry
//! limit. An earlier version used a map capped at 8192 entries, and that cap was
//! reachable by legitimate software: RTP media servers and TURN relays routinely
//! bind tens of thousands of named UDP ports. Past the cap new binds were
//! silently dropped, so whichever ports happened to be bound first kept the
//! table and every later service reported PID 0.
//!
//! The flat array also removes the per-insert allocation, which matters because
//! inserts happen at DISPATCH_LEVEL inside a spin lock.
//!
//! Known gap: sockets bound *before* the driver loaded were never indicated at
//! the bind layer and are therefore absent. Those connections still report 0.

use smoltcp::wire::IpProtocol;
use wdk::rw_spin_lock::RwSpinLock;

/// Number of tracked slots: one per (family, protocol, port) combination.
///
/// The key space is fully bounded - 2 families x 2 protocols x 65536 ports - so
/// the table is allocated once at its maximum size instead of growing. See the
/// module comment for why no entry limit is needed or wanted.
const SLOT_COUNT: usize = 2 * 2 * 65536;

/// Owning process of a bound local endpoint, or 0 for "not known".
///
/// A `u32` is used rather than the `u64` WFP reports: Windows process IDs are
/// 32-bit (a kernel HANDLE value that fits in a DWORD), and halving the entry
/// makes the whole table fit in 1 MB.
type SlotPid = u32;

pub struct EndpointPidCache {
    /// Bound local endpoint -> owning process ID, indexed by `slot_index`.
    ///
    /// Heap allocated because this is far too large for the kernel stack; the
    /// allocation happens once, when the device is created.
    ///
    /// Empty when the allocation failed. The driver still works in that state -
    /// every lookup reports "unknown" and connections fall back to PID 0 - which
    /// is much better than the alternative: this driver's panic handler is
    /// `loop {}`, so an allocation failure that panicked would hang the machine
    /// instead of producing a crash dump.
    slots: alloc::vec::Vec<SlotPid>,
    lock: RwSpinLock,
}

/// Computes the table index for an endpoint.
///
/// Every input is bounded, so this always yields a valid index and the lookup
/// needs no bounds check at the call site: `port` covers the full `u16` range,
/// and only two protocols and two families are ever passed in.
fn slot_index(ipv6: bool, protocol: IpProtocol, port: u16) -> usize {
    // Only TCP and UDP are valid here. This assertion catches a caller that did
    // not filter the protocol first - it would silently fold another protocol
    // (ICMP, raw sockets) into the TCP plane, reading or writing an unrelated
    // entry. Debug-only because release builds should not panic in the kernel,
    // but the assertion documents the contract and catches mistakes in testing.
    debug_assert!(
        matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp),
        "endpoint_pid_cache: unsupported protocol {:?}",
        protocol
    );

    let family = usize::from(ipv6);
    // Only TCP and UDP reach this table; anything else is rejected before the
    // call. Mapping them to 0/1 keeps the table at two protocol planes rather
    // than 256.
    let proto = match protocol {
        IpProtocol::Udp => 0,
        _ => 1,
    };
    (family * 2 + proto) * 65536 + port as usize
}

impl EndpointPidCache {
    pub fn new() -> Self {
        // try_reserve_exact rather than vec![0; N]: an infallible allocation ends
        // in handle_alloc_error, which panics, and the panic handler is `loop {}`.
        // A 1 MB non-paged allocation is not guaranteed to succeed, so failure has
        // to be a value rather than a trap.
        //
        // This only works because WindowsAllocator::alloc returns null on failure
        // as GlobalAlloc requires. It used to call handle_alloc_error itself, which
        // made the Err arm below unreachable - the machine hung instead of taking
        // it.
        let mut slots = alloc::vec::Vec::new();
        match slots.try_reserve_exact(SLOT_COUNT) {
            Ok(()) => slots.resize(SLOT_COUNT, 0),
            Err(_) => {
                crate::err!(
                    "failed to allocate endpoint PID table ({} bytes); inbound \
                     connections will report PID 0",
                    SLOT_COUNT * core::mem::size_of::<SlotPid>()
                );
            }
        }

        Self {
            slots,
            lock: RwSpinLock::default(),
        }
    }

    /// Records the process that bound `port` for `protocol` and `ipv6`.
    ///
    /// A later bind on the same endpoint replaces the earlier entry. Ports are
    /// reused after release, and with SO_REUSEADDR two processes can hold the
    /// same port at once; in both cases the most recent binder is the better
    /// guess, and keeping the stale one would attribute traffic to a process
    /// that may no longer exist.
    pub fn insert(&mut self, ipv6: bool, protocol: IpProtocol, port: u16, process_id: u64) {
        // Truncation is not possible in practice - Windows PIDs are 32-bit - but
        // a value that does not fit would alias an unrelated process, so it is
        // dropped rather than truncated.
        let Ok(pid) = SlotPid::try_from(process_id) else {
            return;
        };

        let index = slot_index(ipv6, protocol, port);
        let _guard = self.lock.write_lock();
        // get_mut rather than indexing: the table is empty if its allocation
        // failed, and a panic here would hang the machine.
        if let Some(slot) = self.slots.get_mut(index) {
            *slot = pid;
        }
    }

    /// Returns the process that owns the endpoint, or `None` if it is not known.
    ///
    /// A zero slot means "never recorded" and is reported as `None`, so callers
    /// cannot mistake it for a resolved process.
    ///
    /// Only TCP and UDP endpoints are tracked. Other protocols (ICMP, raw sockets)
    /// return `None` rather than reading an unrelated slot.
    pub fn get(&self, ipv6: bool, protocol: IpProtocol, port: u16) -> Option<u64> {
        // Only TCP and UDP endpoints are tracked. Rejecting other protocols here
        // prevents a caller passing ICMP or a raw socket protocol from reading an
        // unrelated TCP or UDP entry by accident - defense in depth, since the
        // actual callers already filter, but that guard is not encoded in the type
        // system and could be removed in a future change.
        if !matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp) {
            return None;
        }

        let index = slot_index(ipv6, protocol, port);
        let _guard = self.lock.read_lock();
        match self.slots.get(index).copied() {
            None | Some(0) => None,
            Some(pid) => Some(u64::from(pid)),
        }
    }

    /// Drops the entry for a released port, but only if `process_id` is the
    /// process currently recorded for it.
    ///
    /// The ownership test is what makes a shared endpoint safe. One slot holds one
    /// PID and there is no reference count, so an unconditional remove would let
    /// the first release clear the entry of an owner that is still bound. Measured
    /// with two processes holding UDP port 1245 via SO_REUSEADDR: the release from
    /// the process being torn down arrived while the other was still receiving,
    /// and cleared its entry - the next datagram, delivered to the survivor, was
    /// reported with PID 0.
    ///
    /// WFP supplies the owning PID on the release indication, so the check is
    /// simply "is this release for the owner I know about". A release for the
    /// other owner of a shared endpoint leaves the slot alone; the surviving
    /// owner's own release still matches and clears it.
    ///
    /// The compare and the clear happen under one write lock, so a concurrent
    /// bind on the same endpoint cannot slip in between them.
    pub fn remove(&mut self, ipv6: bool, protocol: IpProtocol, port: u16, process_id: u64) {
        let Ok(pid) = SlotPid::try_from(process_id) else {
            return;
        };
        if pid == 0 {
            // Nothing useful to compare against: a release with no PID cannot be
            // attributed to an owner, so the entry is left for the next bind on
            // this endpoint to overwrite.
            return;
        }

        let index = slot_index(ipv6, protocol, port);
        let _guard = self.lock.write_lock();
        if let Some(slot) = self.slots.get_mut(index) {
            if *slot == pid {
                *slot = 0;
            }
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        let _guard = self.lock.read_lock();
        self.slots.iter().filter(|pid| **pid != 0).count()
    }

    /// Drops every entry.
    ///
    /// Deliberately not called from the ClearCache command: that command resets
    /// decided verdicts, while this table holds observed OS state that a verdict
    /// reset does not invalidate. Since entries are only created by a bind
    /// indication, and an already-bound socket is never re-indicated, clearing
    /// would make every existing listener report PID 0 permanently. See the
    /// ClearCache handler in device.rs.
    #[allow(dead_code)]
    pub fn clear(&mut self) {
        let _guard = self.lock.write_lock();
        self.slots.fill(0);
    }
}
