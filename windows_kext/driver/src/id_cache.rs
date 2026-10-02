use core::mem;

use alloc::{
    collections::{btree_map::Entry as MapEntry, BTreeMap, VecDeque},
    vec::Vec,
};
#[cfg(not(test))]
use protocol::info::Info;
#[cfg(not(test))]
use smoltcp::wire::{IpAddress, IpProtocol};
#[cfg(not(test))]
use wdk::rw_spin_lock::RwSpinLock;

#[cfg(not(test))]
use crate::{connection::Direction, device::Packet};
use crate::{connection_map::Key, tcp_closure_cache::request_matches_tcp_endpoint};

#[cfg(test)]
pub(crate) struct Packet(bool);

#[cfg(test)]
impl Packet {
    fn survives_connection_end(&self, _key: &Key) -> bool {
        self.0
    }
}

#[cfg(test)]
struct RwSpinLock;

#[cfg(test)]
impl RwSpinLock {
    const fn default() -> Self {
        Self
    }

    fn read_lock(&self) {}
    fn write_lock(&self) {}
}

pub struct Entry<T> {
    pub value: T,
    id: u64,
}

impl<T> Entry<T> {
    pub fn id(&self) -> u64 {
        self.id
    }
}

#[derive(Clone, Copy)]
struct PendingIdentity {
    key: Key,
    connection_instance_id: Option<u64>,
}

#[cfg(not(test))]
#[derive(Clone, Copy)]
struct PendingContext {
    key: Key,
    connection_instance_id: Option<u64>,
    process_id: u64,
    direction: Direction,
    ale_layer: bool,
}

/// Request IDs still queued for one live connection generation. Most
/// generations have only one undecided packet, so keep that ID inline and
/// allocate a vector only while several requests overlap.
enum PendingIds {
    One(u64),
    Multiple(Vec<u64>),
}

impl PendingIds {
    fn len(&self) -> usize {
        match self {
            Self::One(_) => 1,
            Self::Multiple(ids) => ids.len(),
        }
    }

    fn extend(&self, target: &mut Vec<u64>) {
        match self {
            Self::One(id) => target.push(*id),
            Self::Multiple(ids) => target.extend_from_slice(ids),
        }
    }

    fn push(&mut self, id: u64) {
        match self {
            Self::One(previous) => {
                let previous = *previous;
                *self = Self::Multiple(alloc::vec![previous, id]);
            }
            Self::Multiple(ids) => ids.push(id),
        }
    }

    /// Removes one claimed request and returns whether the bucket is empty.
    fn remove(&mut self, id: u64) -> Option<bool> {
        match self {
            Self::One(existing) => (*existing == id).then_some(true),
            Self::Multiple(ids) => {
                let index = ids.binary_search(&id).ok()?;
                ids.remove(index);
                Some(ids.is_empty())
            }
        }
    }

    fn append_with_instance(self, instance_id: u64, target: &mut Vec<(u64, u64)>) {
        match self {
            Self::One(id) => target.push((id, instance_id)),
            Self::Multiple(ids) => {
                target.extend(ids.into_iter().map(|id| (id, instance_id)));
            }
        }
    }

    fn for_each(self, mut apply: impl FnMut(u64)) {
        match self {
            Self::One(id) => apply(id),
            Self::Multiple(ids) => ids.into_iter().for_each(apply),
        }
    }
}

pub struct PendingPacket {
    pub key: Key,
    pub packet: Packet,
    /// Exact live connection that queued this packet. Protocols without
    /// connection state (for example ICMP) deliberately leave this unset.
    pub connection_instance_id: Option<u64>,
}

pub struct IdCache {
    values: VecDeque<Entry<PendingPacket>>,
    /// Reverse index for requests that have not yet been claimed by a verdict.
    /// Active requests are tracked separately in `active` and cannot be retired
    /// from this queue.
    pending_by_instance: BTreeMap<u64, PendingIds>,
    active: BTreeMap<u64, PendingIdentity>,
    lock: RwSpinLock,
    next_id: u64,
}

impl IdCache {
    pub fn new() -> Self {
        Self {
            values: VecDeque::with_capacity(1000),
            pending_by_instance: BTreeMap::new(),
            active: BTreeMap::new(),
            lock: RwSpinLock::default(),
            next_id: 1, // 0 is invalid id
        }
    }

    #[cfg(not(test))]
    pub fn push(
        &mut self,
        value: (Key, Packet),
        connection_instance_id: Option<u64>,
        process_id: u64,
        direction: Direction,
        ale_layer: bool,
    ) -> Vec<(u64, Info)> {
        let _guard = self.lock.write_lock();
        let (key, packet) = value;
        let context = PendingContext {
            key,
            connection_instance_id,
            process_id,
            direction,
            ale_layer,
        };

        // One outgoing WFP indication can contain several NET_BUFFER packets.
        // Userspace decides packets rather than indications, so give every clone
        // its own cache entry, event and verdict ID. Keep this expansion under the
        // same lock (and the caller's connection-liveness guard) so endpoint closure
        // cannot observe only part of the original batch.
        match packet {
            Packet::NetworkBatch(nbls, inject_info) => {
                let mut queued = Vec::with_capacity(nbls.len());
                for nbl in nbls {
                    let packet = Packet::Network(nbl, inject_info);
                    if let Some(entry) =
                        push_packet(&mut self.values, &mut self.next_id, packet, context)
                    {
                        add_pending_request(
                            &mut self.pending_by_instance,
                            connection_instance_id,
                            entry.0,
                        );
                        queued.push(entry);
                    }
                }
                queued
            }
            packet => {
                let queued = push_packet(&mut self.values, &mut self.next_id, packet, context);
                if let Some((id, _)) = &queued {
                    add_pending_request(&mut self.pending_by_instance, connection_instance_id, *id);
                }
                queued.into_iter().collect()
            }
        }
    }

    pub fn pop_id(&mut self, id: u64) -> Option<PendingPacket> {
        let _guard = self.lock.write_lock();
        if let Ok(index) = self.values.binary_search_by_key(&id, |val| val.id) {
            let entry = self.values.remove(index)?;
            remove_pending_request(
                &mut self.pending_by_instance,
                entry.value.connection_instance_id,
                id,
            );
            self.active.insert(
                id,
                PendingIdentity {
                    key: entry.value.key,
                    connection_instance_id: entry.value.connection_instance_id,
                },
            );
            return Some(entry.value);
        }
        None
    }

    /// Removes the in-progress marker created when a verdict claimed this ID.
    pub fn finish_id(&mut self, id: u64) {
        let _guard = self.lock.write_lock();
        self.active.remove(&id);
    }

    /// Snapshots packet decisions already queued or being applied for one closing
    /// TCP endpoint. A loopback-like peer's packet-layer tuple is reversed, so
    /// include that side as well as requests bound directly to this connection
    /// generation.
    pub fn tcp_endpoint_request_ids(&self, key: &Key, instance_id: u64) -> Vec<u64> {
        let _guard = self.lock.read_lock();
        let matches = |identity: PendingIdentity| {
            request_matches_tcp_endpoint(
                key,
                instance_id,
                &identity.key,
                identity.connection_instance_id,
            )
        };

        let mut ids = Vec::new();
        if key.is_loopback_like() {
            // The server-side loopback-like request has a reversed tuple and a
            // distinct connection generation, so it still needs the complete
            // tuple scan.
            ids.extend(self.values.iter().filter_map(|entry| {
                matches(PendingIdentity {
                    key: entry.value.key,
                    connection_instance_id: entry.value.connection_instance_id,
                })
                .then_some(entry.id)
            }));
        } else if let Some(pending) = self.pending_by_instance.get(&instance_id) {
            pending.extend(&mut ids);
        }
        ids.extend(
            self.active
                .iter()
                .filter_map(|(id, identity)| matches(*identity).then_some(*id)),
        );
        ids.sort_unstable();
        ids.dedup();
        ids
    }

    /// Retires pending packets owned by one of the supplied connection instances
    /// while preserving the ID order of unrelated requests.
    ///
    /// Outbound UDP packet-layer clones are retained but detached from the ended
    /// instance. The application's send completed before the packet was absorbed,
    /// and endpoint closure must not revoke that already accepted datagram. A later
    /// verdict therefore remains valid for the clone, but cannot update a reused
    /// connection tuple. Every NET_BUFFER packet retains its own request ID; entries
    /// are never merged. Other packets are removed and returned for fail-closed
    /// completion after both the cache lock and its outer Device lock are released.
    ///
    /// The reverse index makes the ordinary one-endpoint path proportional to the
    /// number of that endpoint's pending requests rather than the size of the global
    /// queue. No replacement queue is allocated: surviving UDP entries are changed
    /// in place and other entries are removed directly from the existing deque.
    pub fn retire_connection_instances(
        &mut self,
        sorted_instance_ids: &[u64],
    ) -> VecDeque<Entry<PendingPacket>> {
        if sorted_instance_ids.is_empty() {
            return VecDeque::new();
        }

        let _guard = self.lock.write_lock();
        let mut removed = VecDeque::new();

        if sorted_instance_ids.len() == 1 {
            let instance_id = sorted_instance_ids[0];
            if let Some(ids) = self.pending_by_instance.remove(&instance_id) {
                retire_pending_ids(&mut self.values, instance_id, ids, &mut removed);
            }
            return removed;
        }

        let mut previous_instance_id = None;
        let pending_count = sorted_instance_ids
            .iter()
            .copied()
            .filter(|instance_id| {
                let unique = previous_instance_id != Some(*instance_id);
                previous_instance_id = Some(*instance_id);
                unique
            })
            .filter_map(|instance_id| self.pending_by_instance.get(&instance_id))
            .map(PendingIds::len)
            .sum();
        if pending_count == 0 {
            return removed;
        }

        // Avoid a temporary allocation when a batched lifecycle notification has
        // only one queued request among all of its connection generations.
        if pending_count == 1 {
            if let Some(instance_id) = sorted_instance_ids
                .iter()
                .copied()
                .find(|instance_id| self.pending_by_instance.contains_key(instance_id))
            {
                if let Some(ids) = self.pending_by_instance.remove(&instance_id) {
                    retire_pending_ids(&mut self.values, instance_id, ids, &mut removed);
                }
            }
            return removed;
        }

        // Requests from several instances can interleave in the global queue. Sort
        // only the affected IDs so fail-closed completions retain their prior order.
        let mut pending_ids = Vec::with_capacity(pending_count);
        previous_instance_id = None;
        for instance_id in sorted_instance_ids.iter().copied() {
            if previous_instance_id == Some(instance_id) {
                continue;
            }
            previous_instance_id = Some(instance_id);
            if let Some(ids) = self.pending_by_instance.remove(&instance_id) {
                ids.append_with_instance(instance_id, &mut pending_ids);
            }
        }
        pending_ids.sort_unstable_by_key(|(id, _)| *id);
        for (id, instance_id) in pending_ids {
            retire_pending_id(&mut self.values, id, instance_id, &mut removed);
        }
        removed
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        let _guard = self.lock.read_lock();
        return self.values.len() + self.active.len();
    }

    pub fn pop_all(&mut self) -> VecDeque<Entry<PendingPacket>> {
        let mut values = VecDeque::with_capacity(1);
        let _guard = self.lock.write_lock();
        mem::swap(&mut self.values, &mut values);
        self.pending_by_instance.clear();

        return values;
    }
}

fn add_pending_request(
    pending_by_instance: &mut BTreeMap<u64, PendingIds>,
    connection_instance_id: Option<u64>,
    request_id: u64,
) {
    let Some(instance_id) = connection_instance_id.filter(|instance_id| *instance_id != 0) else {
        return;
    };

    match pending_by_instance.entry(instance_id) {
        MapEntry::Occupied(entry) => entry.into_mut().push(request_id),
        MapEntry::Vacant(entry) => {
            entry.insert(PendingIds::One(request_id));
        }
    }
}

fn remove_pending_request(
    pending_by_instance: &mut BTreeMap<u64, PendingIds>,
    connection_instance_id: Option<u64>,
    request_id: u64,
) {
    let Some(instance_id) = connection_instance_id.filter(|instance_id| *instance_id != 0) else {
        return;
    };
    let Some(ids) = pending_by_instance.get_mut(&instance_id) else {
        return;
    };
    let remove_instance = ids.remove(request_id).unwrap_or(false);
    if remove_instance {
        pending_by_instance.remove(&instance_id);
    }
}

fn retire_pending_ids(
    values: &mut VecDeque<Entry<PendingPacket>>,
    instance_id: u64,
    ids: PendingIds,
    removed: &mut VecDeque<Entry<PendingPacket>>,
) {
    ids.for_each(|id| retire_pending_id(values, id, instance_id, removed));
}

fn retire_pending_id(
    values: &mut VecDeque<Entry<PendingPacket>>,
    id: u64,
    instance_id: u64,
    removed: &mut VecDeque<Entry<PendingPacket>>,
) {
    let Ok(index) = values.binary_search_by_key(&id, |entry| entry.id) else {
        return;
    };
    if values[index].value.connection_instance_id != Some(instance_id) {
        return;
    }

    if values[index]
        .value
        .packet
        .survives_connection_end(&values[index].value.key)
    {
        values[index].value.connection_instance_id = None;
    } else if let Some(entry) = values.remove(index) {
        removed.push_back(entry);
    }
}

#[cfg(not(test))]
fn push_packet(
    values: &mut VecDeque<Entry<PendingPacket>>,
    next_id: &mut u64,
    packet: Packet,
    context: PendingContext,
) -> Option<(u64, Info)> {
    let id = *next_id;
    let info = build_info(
        &context.key,
        id,
        context.process_id,
        context.direction,
        &packet,
        context.ale_layer,
    )?;
    values.push_back(Entry {
        value: PendingPacket {
            key: context.key,
            packet,
            connection_instance_id: context.connection_instance_id,
        },
        id,
    });
    *next_id = next_id.wrapping_add(1); // Assuming this will not overflow.
    Some((id, info))
}

#[cfg(not(test))]
fn get_payload(packet: &Packet) -> Option<&[u8]> {
    match packet {
        Packet::Network(nbl, _) => nbl.get_data(),
        Packet::NetworkBatch(nbls, _) => nbls.first().and_then(|nbl| nbl.get_data()),
        Packet::AleLayer(defer) => defer
            .packet_list()
            .and_then(|packet_list| packet_list.get_event_data()),
    }
}

#[cfg(not(test))]
fn build_info(
    key: &Key,
    packet_id: u64,
    process_id: u64,
    direction: Direction,
    packet: &Packet,
    ale_layer: bool,
) -> Option<Info> {
    let (local_port, remote_port) = match key.protocol {
        IpProtocol::Tcp | IpProtocol::Udp => (key.local_port, key.remote_port),
        _ => (0, 0),
    };

    let payload_layer = if ale_layer {
        4 // Transport layer
    } else {
        3 // Network layer
    };

    let mut payload = &[][..];
    if let Some(p) = get_payload(packet) {
        payload = p;
    }

    match (key.local_address, key.remote_address) {
        (IpAddress::Ipv6(local_ip), IpAddress::Ipv6(remote_ip)) if key.is_ipv6() => {
            Some(protocol::info::connection_info_v6(
                packet_id,
                process_id,
                direction as u8,
                u8::from(key.protocol),
                local_ip.0,
                remote_ip.0,
                local_port,
                remote_port,
                payload_layer,
                payload,
            ))
        }
        (IpAddress::Ipv4(local_ip), IpAddress::Ipv4(remote_ip)) => {
            Some(protocol::info::connection_info_v4(
                packet_id,
                process_id,
                direction as u8,
                u8::from(key.protocol),
                local_ip.0,
                remote_ip.0,
                local_port,
                remote_port,
                payload_layer,
                payload,
            ))
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::{add_pending_request, Entry, IdCache, Packet, PendingPacket};
    use crate::connection_map::Key;
    use alloc::vec;
    use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address};

    fn key(remote_port: u16) -> Key {
        Key {
            protocol: IpProtocol::Udp,
            local_address: IpAddress::Ipv4(Ipv4Address::new(10, 0, 0, 1)),
            local_port: 40_000,
            remote_address: IpAddress::Ipv4(Ipv4Address::new(192, 0, 2, 1)),
            remote_port,
        }
    }

    fn tcp_key() -> Key {
        let mut key = key(443);
        key.protocol = IpProtocol::Tcp;
        key
    }

    fn queue(
        cache: &mut IdCache,
        id: u64,
        connection_instance_id: Option<u64>,
        survives_connection_end: bool,
    ) {
        cache.values.push_back(Entry {
            value: PendingPacket {
                key: key(id as u16),
                packet: Packet(survives_connection_end),
                connection_instance_id,
            },
            id,
        });
        add_pending_request(&mut cache.pending_by_instance, connection_instance_id, id);
    }

    fn queued_ids(cache: &IdCache) -> alloc::vec::Vec<u64> {
        cache.values.iter().map(Entry::id).collect()
    }

    #[test]
    fn surviving_request_is_detached_in_place() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, Some(20), true);
        queue(&mut cache, 3, Some(30), false);
        let capacity = cache.values.capacity();

        let removed = cache.retire_connection_instances(&[20]);

        assert!(removed.is_empty());
        assert_eq!(queued_ids(&cache), vec![1, 2, 3]);
        assert_eq!(cache.values.capacity(), capacity);
        assert_eq!(cache.values[1].value.connection_instance_id, None);
        assert!(!cache.pending_by_instance.contains_key(&20));
        assert!(cache.pending_by_instance.contains_key(&10));
        assert!(cache.pending_by_instance.contains_key(&30));

        let detached = cache.pop_id(2).expect("detached request remains claimable");
        assert_eq!(detached.connection_instance_id, None);
        assert_eq!(queued_ids(&cache), vec![1, 3]);
    }

    #[test]
    fn batched_retirement_preserves_global_request_order() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, Some(20), false);
        queue(&mut cache, 3, Some(30), true);
        queue(&mut cache, 4, Some(20), false);
        queue(&mut cache, 5, Some(40), false);

        let removed = cache.retire_connection_instances(&[20, 30]);

        assert_eq!(
            removed
                .iter()
                .map(Entry::id)
                .collect::<alloc::vec::Vec<_>>(),
            vec![2, 4]
        );
        assert_eq!(queued_ids(&cache), vec![1, 3, 5]);
        assert_eq!(cache.values[1].value.connection_instance_id, None);
        assert!(!cache.pending_by_instance.contains_key(&20));
        assert!(!cache.pending_by_instance.contains_key(&30));
        assert!(cache.pending_by_instance.contains_key(&10));
        assert!(cache.pending_by_instance.contains_key(&40));
    }

    #[test]
    fn claiming_requests_keeps_reverse_index_exact() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, Some(10), false);
        queue(&mut cache, 3, Some(20), false);

        let claimed = cache.pop_id(1).expect("first request");
        assert_eq!(claimed.connection_instance_id, Some(10));
        assert_eq!(
            cache.pending_by_instance.get(&10).map(|ids| ids.len()),
            Some(1)
        );
        assert_eq!(cache.tcp_endpoint_request_ids(&tcp_key(), 10), vec![1, 2]);

        let removed = cache.retire_connection_instances(&[10]);
        assert_eq!(
            removed
                .iter()
                .map(Entry::id)
                .collect::<alloc::vec::Vec<_>>(),
            vec![2]
        );
        assert_eq!(queued_ids(&cache), vec![3]);
        assert!(cache.pending_by_instance.get(&10).is_none());
        assert!(cache.active.contains_key(&1));
    }

    #[test]
    fn unknown_instances_do_not_rotate_or_reallocate_queue() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, None, false);
        queue(&mut cache, 3, Some(30), false);
        let first = cache.values.front().map(|entry| entry as *const _);
        let capacity = cache.values.capacity();

        let removed = cache.retire_connection_instances(&[20]);

        assert!(removed.is_empty());
        assert_eq!(queued_ids(&cache), vec![1, 2, 3]);
        assert_eq!(cache.values.front().map(|entry| entry as *const _), first);
        assert_eq!(cache.values.capacity(), capacity);
    }

    #[test]
    fn duplicate_instance_ids_are_retired_once() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, Some(20), false);

        let removed = cache.retire_connection_instances(&[10, 10]);

        assert_eq!(
            removed
                .iter()
                .map(Entry::id)
                .collect::<alloc::vec::Vec<_>>(),
            vec![1]
        );
        assert_eq!(queued_ids(&cache), vec![2]);
    }

    #[test]
    fn draining_pending_requests_clears_reverse_index() {
        let mut cache = IdCache::new();
        queue(&mut cache, 1, Some(10), false);
        queue(&mut cache, 2, Some(20), false);

        let drained = cache.pop_all();

        assert_eq!(
            drained
                .iter()
                .map(Entry::id)
                .collect::<alloc::vec::Vec<_>>(),
            vec![1, 2]
        );
        assert!(cache.values.is_empty());
        assert!(cache.pending_by_instance.is_empty());
    }
}
