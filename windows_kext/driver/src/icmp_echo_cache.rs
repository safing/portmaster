//! Matches an inbound ICMP echo reply to the process that sent the request.
//!
//! An outbound echo request is indicated in the context of the sending thread, so
//! the packet layer can read the originating process directly. An inbound reply
//! cannot: receive processing runs in an arbitrary (DPC) context, and measurement
//! confirmed that no WFP layer supplies a usable process either - the transport
//! layer is not even reached for an echo reply, because no socket is associated
//! with it. See the note in `packet_callouts::ip_packet_layer`.
//!
//! So the association has to be carried across by the driver: remember the
//! outbound request, then look it up when the reply comes back.
//!
//! Keyed on (remote address, echo identifier). The identifier is chosen by the
//! sender and echoed back unchanged, which is exactly what makes it usable here -
//! it is the only field that ties a reply to one specific sender when several
//! processes ping the same host. The sequence number is deliberately NOT part of
//! the key: it increments per request, so keying on it would need one entry per
//! packet in flight rather than one per session.
//!
//! Entries expire. A request that is never answered - unreachable host, dropped
//! reply - would otherwise occupy its slot forever, and an identifier reused later
//! by another process would then be attributed to the wrong one.

use alloc::collections::BTreeMap;
use smoltcp::wire::IpAddress;
use wdk::rw_spin_lock::RwSpinLock;

/// What a reply is matched against.
///
/// The remote address is the constant of the exchange: the request goes to it and
/// the reply comes from it. The local address is not part of the key - it can
/// differ between request and reply on a multi-homed host.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct EchoKey {
    remote_address: IpAddress,
    identifier: u16,
}

#[derive(Clone, Copy)]
struct EchoEntry {
    process_id: u64,
    /// Milliseconds since boot, from `get_system_timestamp_ms`.
    inserted_at_ms: u64,
}

/// How long an unanswered request is remembered.
///
/// Long enough for a reply from a slow or distant host, short enough that a
/// recycled identifier is not matched against a stale request. `ping` waits 4
/// seconds per echo by default, so 10 covers a full timeout plus slack.
const ENTRY_TTL_MS: u64 = 10_000;

/// Upper bound on tracked requests.
///
/// Reached only if many processes have unanswered requests at once; normal traffic
/// keeps this near zero because a matched reply removes its entry immediately. The
/// bound exists so that a host which never replies cannot grow the map without
/// limit.
///
/// The data is small: a key is 20 bytes (a smoltcp `IpAddress` is 17 - a
/// discriminant plus room for a v6 address - and the identifier pads it out) and a
/// value is 16, so 512 entries carry about 18 KB. Actual pool use is higher, and
/// not by a constant factor: entries sit in `BTreeMap` nodes that hold a fixed
/// number of slots, stay only part full after a split, and are each a separate
/// non-paged allocation with its own header. Budget around 40 KB, not 20.
const MAX_ENTRIES: usize = 512;

pub struct IcmpEchoCache {
    /// A map rather than a flat array: unlike the endpoint PID table, the key space
    /// here includes a full address, so it cannot be indexed directly. The map
    /// stays small because entries are removed as soon as they are used.
    entries: BTreeMap<EchoKey, EchoEntry>,
    lock: RwSpinLock,
}

impl IcmpEchoCache {
    pub fn new() -> Self {
        Self {
            entries: BTreeMap::new(),
            lock: RwSpinLock::default(),
        }
    }

    /// Records an outbound echo request.
    ///
    /// A repeated request with the same identifier to the same host overwrites the
    /// previous entry, which also refreshes its timestamp - that is correct for
    /// `ping`, where every echo in a run shares one identifier.
    pub fn insert_request(&mut self, remote_address: IpAddress, identifier: u16, process_id: u64) {
        if process_id == 0 {
            // Nothing worth remembering: a zero PID would later be reported as
            // "unknown" anyway, and storing it would only occupy a slot.
            return;
        }

        let now = wdk::utils::get_system_timestamp_ms();
        let key = EchoKey {
            remote_address,
            identifier,
        };
        let entry = EchoEntry {
            process_id,
            inserted_at_ms: now,
        };

        let _guard = self.lock.write_lock();

        if self.entries.len() >= MAX_ENTRIES && !self.entries.contains_key(&key) {
            // At capacity. Drop everything already expired; that alone usually
            // frees room, because the cap is only approached when requests are
            // going unanswered.
            self.entries
                .retain(|_, e| now.saturating_sub(e.inserted_at_ms) <= ENTRY_TTL_MS);

            if self.entries.len() >= MAX_ENTRIES {
                // Still full: every entry is live. Give up on this request rather
                // than evicting someone else's - a missing entry costs one reply
                // reported as PID 0, while evicting a live entry would do the same
                // to a different process and lose the older information too.
                return;
            }
        }

        self.entries.insert(key, entry);
    }

    /// Returns the process that sent the matching request, removing the entry.
    ///
    /// Removal is deliberate. Each reply consumes its request, so a duplicated or
    /// spoofed reply arriving afterwards is not attributed to the process. For
    /// `ping`, the next echo re-inserts the entry before its own reply arrives.
    pub fn take_request_pid(
        &mut self,
        remote_address: IpAddress,
        identifier: u16,
    ) -> Option<u64> {
        let key = EchoKey {
            remote_address,
            identifier,
        };

        let _guard = self.lock.write_lock();
        let entry = self.entries.remove(&key)?;

        // Expiry is checked on read as well as on insert: an entry can sit here
        // long after its TTL if no insert forced a cleanup in between.
        let now = wdk::utils::get_system_timestamp_ms();
        if now.saturating_sub(entry.inserted_at_ms) > ENTRY_TTL_MS {
            return None;
        }

        Some(entry.process_id)
    }

    /// Drops every entry past its TTL.
    ///
    /// Called from the periodic `CleanEndedConnections` command, which runs at
    /// PASSIVE_LEVEL. That is the point of doing it here: otherwise the only
    /// sweep is the one in `insert_request`, which walks the whole map from
    /// inside a packet callout at DISPATCH_LEVEL, and only when the map is
    /// already at capacity. Requests that go unanswered while the map stays
    /// below `MAX_ENTRIES` are never revisited and would hold their slots
    /// indefinitely - nothing reads them again, so nothing removes them.
    ///
    /// Only expired entries go. A full clear would discard requests still in
    /// flight, and their replies would then be reported as PID 0.
    pub fn clean_expired_entries(&mut self) {
        let now = wdk::utils::get_system_timestamp_ms();

        let _guard = self.lock.write_lock();
        self.entries
            .retain(|_, e| now.saturating_sub(e.inserted_at_ms) <= ENTRY_TTL_MS);
    }
}
