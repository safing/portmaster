use alloc::collections::VecDeque;
use protocol::info::Info;
use smoltcp::wire::{IpAddress, IpProtocol};
use wdk::rw_spin_lock::RwSpinLock;

use crate::{connection::Direction, connection_map::Key, device::Packet};

struct Entry<T> {
    value: T,
    id: u64,
}

pub struct IdCache {
    values: VecDeque<Entry<(Key, Packet)>>,
    lock: RwSpinLock,
    next_id: u64,
}

impl IdCache {
    pub fn new() -> Self {
        Self {
            values: VecDeque::with_capacity(1000),
            lock: RwSpinLock::default(),
            next_id: 1, // 0 is invalid id
        }
    }

    pub fn push(
        &mut self,
        value: (Key, Packet),
        process_id: u64,
        direction: Direction,
        ale_layer: bool,
    ) -> Option<Info> {
        let _guard = self.lock.write_lock();
        let id = self.next_id;
        let info = build_info(&value.0, id, process_id, direction, &value.1, ale_layer);
        self.values.push_back(Entry { value, id });
        self.next_id = self.next_id.wrapping_add(1); // Assuming this will not overflow.

        return info;
    }

    pub fn pop_id(&mut self, id: u64) -> Option<(Key, Packet)> {
        let _guard = self.lock.write_lock();
        if let Ok(index) = self.values.binary_search_by_key(&id, |val| val.id) {
            return Some(self.values.remove(index).unwrap().value);
        }
        None
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        let _guard = self.lock.read_lock();
        return self.values.len();
    }
}

fn get_payload(packet: &Packet) -> Option<&[u8]> {
    match packet {
        Packet::PacketLayer(nbl, _) => nbl.get_data(),
        Packet::AleLayer(defer) => {
            let p = match defer {
                wdk::filter_engine::callout_data::ClassifyDefer::Initial(_, p) => p,
                wdk::filter_engine::callout_data::ClassifyDefer::Reauthorization(_, p) => p,
            };
            if let Some(tpl) = p {
                tpl.net_buffer_list.get_data()
            } else {
                None
            }
        }
    }
}

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
