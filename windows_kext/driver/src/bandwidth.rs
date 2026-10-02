use alloc::collections::BTreeMap;
use protocol::info::{BandwidthValueV4, BandwidthValueV6, Info};
use smoltcp::wire::{IpProtocol, Ipv4Address, Ipv6Address};
use wdk::rw_spin_lock::RwSpinLock;

use crate::connection::Direction;

#[derive(Debug, PartialEq, Eq, PartialOrd, Ord, Clone, Copy, Default)]
pub struct Key<Address: Ord> {
    pub local_ip: Address,
    pub local_port: u16,
    pub remote_ip: Address,
    pub remote_port: u16,
}

#[derive(Default)]
struct Value {
    received_bytes: usize,
    transmitted_bytes: usize,
}

pub struct Bandwidth {
    stats_tcp_v4: RwSpinLock<BTreeMap<Key<Ipv4Address>, Value>>,
    stats_tcp_v6: RwSpinLock<BTreeMap<Key<Ipv6Address>, Value>>,
    stats_udp_v4: RwSpinLock<BTreeMap<Key<Ipv4Address>, Value>>,
    stats_udp_v6: RwSpinLock<BTreeMap<Key<Ipv6Address>, Value>>,
}

impl Bandwidth {
    pub fn new() -> Self {
        Self {
            stats_tcp_v4: RwSpinLock::new(BTreeMap::new()),
            stats_tcp_v6: RwSpinLock::new(BTreeMap::new()),
            stats_udp_v4: RwSpinLock::new(BTreeMap::new()),
            stats_udp_v6: RwSpinLock::new(BTreeMap::new()),
        }
    }

    pub fn get_all_updates_tcp_v4(&self) -> Option<Info> {
        let stats_map = {
            let mut stats_map = self.stats_tcp_v4.write_lock();
            if stats_map.is_empty() {
                return None;
            }
            core::mem::take(&mut *stats_map)
        };

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map {
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

    pub fn get_all_updates_tcp_v6(&self) -> Option<Info> {
        let stats_map = {
            let mut stats_map = self.stats_tcp_v6.write_lock();
            if stats_map.is_empty() {
                return None;
            }
            core::mem::take(&mut *stats_map)
        };

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map {
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

    pub fn get_all_updates_udp_v4(&self) -> Option<Info> {
        let stats_map = {
            let mut stats_map = self.stats_udp_v4.write_lock();
            if stats_map.is_empty() {
                return None;
            }
            core::mem::take(&mut *stats_map)
        };

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map {
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

    pub fn get_all_updates_udp_v6(&self) -> Option<Info> {
        let stats_map = {
            let mut stats_map = self.stats_udp_v6.write_lock();
            if stats_map.is_empty() {
                return None;
            }
            core::mem::take(&mut *stats_map)
        };

        let mut values = alloc::vec::Vec::with_capacity(stats_map.len());
        for (key, value) in stats_map {
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

    #[inline]
    pub fn update_tcp_v4(&self, key: Key<Ipv4Address>, direction: Direction, bytes: usize) {
        Self::update(&self.stats_tcp_v4, key, direction, bytes);
    }

    #[inline]
    pub fn update_tcp_v6(&self, key: Key<Ipv6Address>, direction: Direction, bytes: usize) {
        Self::update(&self.stats_tcp_v6, key, direction, bytes);
    }

    #[inline]
    pub fn update_udp_v4(&self, key: Key<Ipv4Address>, direction: Direction, bytes: usize) {
        Self::update(&self.stats_udp_v4, key, direction, bytes);
    }

    #[inline]
    pub fn update_udp_v6(&self, key: Key<Ipv6Address>, direction: Direction, bytes: usize) {
        Self::update(&self.stats_udp_v6, key, direction, bytes);
    }

    #[inline]
    fn update<Address: Ord>(
        stats: &RwSpinLock<BTreeMap<Key<Address>, Value>>,
        key: Key<Address>,
        direction: Direction,
        bytes: usize,
    ) {
        let mut stats = stats.write_lock();
        let value = stats.entry(key).or_default();
        match direction {
            Direction::Outbound => value.transmitted_bytes += bytes,
            Direction::Inbound => value.received_bytes += bytes,
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_counts(&self) -> (usize, usize, usize, usize) {
        // Keep all four guards until the lengths have been read so this diagnostic
        // remains one coherent snapshot while packet callbacks update the maps.
        let tcp_v4 = self.stats_tcp_v4.read_lock();
        let tcp_v6 = self.stats_tcp_v6.read_lock();
        let udp_v4 = self.stats_udp_v4.read_lock();
        let udp_v6 = self.stats_udp_v6.read_lock();

        (tcp_v4.len(), tcp_v6.len(), udp_v4.len(), udp_v6.len())
    }
}
