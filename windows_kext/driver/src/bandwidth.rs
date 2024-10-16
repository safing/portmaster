use alloc::collections::BTreeMap;
use protocol::info::{BandwidthValueV4, BandwidthValueV6, Info};
use smoltcp::wire::{IpProtocol, Ipv4Address, Ipv6Address};
use wdk::rw_spin_lock::RwSpinLock;

#[derive(Debug, PartialEq, Eq, PartialOrd, Ord, Clone, Copy, Default)]
pub struct Key<Address: Ord> {
    pub local_ip: Address,
    pub local_port: u16,
    pub remote_ip: Address,
    pub remote_port: u16,
}

struct Value {
    received_bytes: usize,
    transmitted_bytes: usize,
}

enum Direction {
    Tx(usize),
    Rx(usize),
}
pub struct Bandwidth {
    stats_tcp_v4: BTreeMap<Key<Ipv4Address>, Value>,
    stats_tcp_v4_lock: RwSpinLock,

    stats_tcp_v6: BTreeMap<Key<Ipv6Address>, Value>,
    stats_tcp_v6_lock: RwSpinLock,

    stats_udp_v4: BTreeMap<Key<Ipv4Address>, Value>,
    stats_udp_v4_lock: RwSpinLock,

    stats_udp_v6: BTreeMap<Key<Ipv6Address>, Value>,
    stats_udp_v6_lock: RwSpinLock,
}

impl Bandwidth {
    pub fn new() -> Self {
        Self {
            stats_tcp_v4: BTreeMap::new(),
            stats_tcp_v4_lock: RwSpinLock::default(),

            stats_tcp_v6: BTreeMap::new(),
            stats_tcp_v6_lock: RwSpinLock::default(),

            stats_udp_v4: BTreeMap::new(),
            stats_udp_v4_lock: RwSpinLock::default(),

            stats_udp_v6: BTreeMap::new(),
            stats_udp_v6_lock: RwSpinLock::default(),
        }
    }

    pub fn get_all_updates_tcp_v4(&mut self) -> Option<Info> {
        let stats_map;
        {
            let _guard = self.stats_tcp_v4_lock.write_lock();
            if self.stats_tcp_v4.is_empty() {
                return None;
            }
            stats_map = core::mem::replace(&mut self.stats_tcp_v4, BTreeMap::new());
        }

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map.iter() {
            values.push(BandwidthValueV4 {
                local_ip: key.local_ip.0,
                local_port: key.local_port,
                remote_ip: key.remote_ip.0,
                remote_port: key.remote_port,
                transmitted_bytes: value.transmitted_bytes as u64,
                received_bytes: value.received_bytes as u64,
            });
        }
        Some(protocol::info::bandiwth_stats_array_v4(
            u8::from(IpProtocol::Tcp),
            values,
        ))
    }

    pub fn get_all_updates_tcp_v6(&mut self) -> Option<Info> {
        let stats_map;
        {
            let _guard = self.stats_tcp_v6_lock.write_lock();
            if self.stats_tcp_v6.is_empty() {
                return None;
            }
            stats_map = core::mem::replace(&mut self.stats_tcp_v6, BTreeMap::new());
        }

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map.iter() {
            values.push(BandwidthValueV6 {
                local_ip: key.local_ip.0,
                local_port: key.local_port,
                remote_ip: key.remote_ip.0,
                remote_port: key.remote_port,
                transmitted_bytes: value.transmitted_bytes as u64,
                received_bytes: value.received_bytes as u64,
            });
        }
        Some(protocol::info::bandiwth_stats_array_v6(
            u8::from(IpProtocol::Tcp),
            values,
        ))
    }

    pub fn get_all_updates_udp_v4(&mut self) -> Option<Info> {
        let stats_map;
        {
            let _guard = self.stats_udp_v4_lock.write_lock();
            if self.stats_udp_v4.is_empty() {
                return None;
            }
            stats_map = core::mem::replace(&mut self.stats_udp_v4, BTreeMap::new());
        }

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map.iter() {
            values.push(BandwidthValueV4 {
                local_ip: key.local_ip.0,
                local_port: key.local_port,
                remote_ip: key.remote_ip.0,
                remote_port: key.remote_port,
                transmitted_bytes: value.transmitted_bytes as u64,
                received_bytes: value.received_bytes as u64,
            });
        }
        Some(protocol::info::bandiwth_stats_array_v4(
            u8::from(IpProtocol::Udp),
            values,
        ))
    }

    pub fn get_all_updates_udp_v6(&mut self) -> Option<Info> {
        let stats_map;
        {
            let _guard = self.stats_udp_v6_lock.write_lock();
            if self.stats_udp_v6.is_empty() {
                return None;
            }
            stats_map = core::mem::replace(&mut self.stats_udp_v6, BTreeMap::new());
        }

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map.iter() {
            values.push(BandwidthValueV6 {
                local_ip: key.local_ip.0,
                local_port: key.local_port,
                remote_ip: key.remote_ip.0,
                remote_port: key.remote_port,
                transmitted_bytes: value.transmitted_bytes as u64,
                received_bytes: value.received_bytes as u64,
            });
        }
        Some(protocol::info::bandiwth_stats_array_v6(
            u8::from(IpProtocol::Udp),
            values,
        ))
    }

    pub fn update_tcp_v4_tx(&mut self, key: Key<Ipv4Address>, tx_bytes: usize) {
        Self::update(
            &mut self.stats_tcp_v4,
            &mut self.stats_tcp_v4_lock,
            key,
            Direction::Tx(tx_bytes),
        );
    }

    pub fn update_tcp_v4_rx(&mut self, key: Key<Ipv4Address>, rx_bytes: usize) {
        Self::update(
            &mut self.stats_tcp_v4,
            &mut self.stats_tcp_v4_lock,
            key,
            Direction::Rx(rx_bytes),
        );
    }

    pub fn update_tcp_v6_tx(&mut self, key: Key<Ipv6Address>, tx_bytes: usize) {
        Self::update(
            &mut self.stats_tcp_v6,
            &mut self.stats_tcp_v6_lock,
            key,
            Direction::Tx(tx_bytes),
        );
    }

    pub fn update_tcp_v6_rx(&mut self, key: Key<Ipv6Address>, rx_bytes: usize) {
        Self::update(
            &mut self.stats_tcp_v6,
            &mut self.stats_tcp_v6_lock,
            key,
            Direction::Rx(rx_bytes),
        );
    }

    pub fn update_udp_v4_tx(&mut self, key: Key<Ipv4Address>, tx_bytes: usize) {
        Self::update(
            &mut self.stats_udp_v4,
            &mut self.stats_udp_v4_lock,
            key,
            Direction::Tx(tx_bytes),
        );
    }

    pub fn update_udp_v4_rx(&mut self, key: Key<Ipv4Address>, rx_bytes: usize) {
        Self::update(
            &mut self.stats_udp_v4,
            &mut self.stats_udp_v4_lock,
            key,
            Direction::Rx(rx_bytes),
        );
    }

    pub fn update_udp_v6_tx(&mut self, key: Key<Ipv6Address>, tx_bytes: usize) {
        Self::update(
            &mut self.stats_udp_v6,
            &mut self.stats_udp_v6_lock,
            key,
            Direction::Tx(tx_bytes),
        );
    }

    pub fn update_udp_v6_rx(&mut self, key: Key<Ipv6Address>, rx_bytes: usize) {
        Self::update(
            &mut self.stats_udp_v6,
            &mut self.stats_udp_v6_lock,
            key,
            Direction::Rx(rx_bytes),
        );
    }

    fn update<Address: Ord>(
        map: &mut BTreeMap<Key<Address>, Value>,
        lock: &mut RwSpinLock,
        key: Key<Address>,
        bytes: Direction,
    ) {
        let _guard = lock.write_lock();
        if let Some(value) = map.get_mut(&key) {
            match bytes {
                Direction::Tx(bytes_count) => value.transmitted_bytes += bytes_count,
                Direction::Rx(bytes_count) => value.received_bytes += bytes_count,
            }
        } else {
            let mut received_bytes = 0;
            let mut transmitted_bytes = 0;
            match bytes {
                Direction::Tx(bytes_count) => transmitted_bytes += bytes_count,
                Direction::Rx(bytes_count) => received_bytes += bytes_count,
            }
            map.insert(
                key,
                Value {
                    received_bytes,
                    transmitted_bytes,
                },
            );
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_count(&self) -> usize {
        let mut size = 0;
        {
            let values = &self.stats_tcp_v4.values();
            let _guard = self.stats_tcp_v4_lock.read_lock();
            size += values.len();
        }
        {
            let values = &self.stats_tcp_v6.values();
            let _guard = self.stats_tcp_v6_lock.read_lock();
            size += values.len();
        }
        {
            let values = &self.stats_udp_v4.values();
            let _guard = self.stats_udp_v4_lock.read_lock();
            size += values.len();
        }
        {
            let values = &self.stats_udp_v6.values();
            let _guard = self.stats_udp_v6_lock.read_lock();
            size += values.len();
        }

        return size;
    }
}
