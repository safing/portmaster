use alloc::vec::Vec;

#[repr(u8)]
#[derive(Clone, Copy)]
enum InfoType {
    LogLine = 0,
    ConnectionIpv4 = 1,
    ConnectionIpv6 = 2,
    ConnectionEndEventV4 = 3,
    ConnectionEndEventV6 = 4,
    BandwidthStatsV4 = 5,
    BandwidthStatsV6 = 6,
}

// Fallow this pattern when adding new packets: [InfoType: u8, data_size_in_bytes: u32, data: ...]

trait PushBytes {
    fn push(self, vec: &mut Vec<u8>);
}

impl PushBytes for u8 {
    fn push(self, vec: &mut Vec<u8>) {
        vec.push(self);
    }
}

impl PushBytes for InfoType {
    fn push(self, vec: &mut Vec<u8>) {
        vec.push(self as u8);
    }
}

impl PushBytes for u16 {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&u16::to_le_bytes(self));
    }
}

impl PushBytes for u32 {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&u32::to_le_bytes(self));
    }
}

impl PushBytes for u64 {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&u64::to_le_bytes(self));
    }
}

impl PushBytes for usize {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&usize::to_le_bytes(self));
    }
}

impl PushBytes for [u8; 4] {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&self);
    }
}

impl PushBytes for [u8; 16] {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(&self);
    }
}

impl PushBytes for &[u8] {
    fn push(self, vec: &mut Vec<u8>) {
        vec.extend_from_slice(self);
    }
}

macro_rules! push_bytes {
    ($vec:expr,$value:expr) => {
        PushBytes::push($value, $vec);
    };
}

macro_rules! get_combined_size{
    ($($a:expr),*)=>{{0 $(+core::mem::size_of_val(&$a))*}}
}

pub struct Info(Vec<u8>);

impl Info {
    fn new(info_type: InfoType, size: usize) -> Self {
        let mut vec = Vec::with_capacity(size + 5); // +1 for the info type +4 for the size.
        push_bytes!(&mut vec, info_type);
        push_bytes!(&mut vec, size as u32);
        Self(vec)
    }

    fn with_capacity(info_type: InfoType, capacity: usize) -> Self {
        let mut vec = Vec::with_capacity(capacity + 5); // +1 for the info type + 4 for the size.
        push_bytes!(&mut vec, info_type);
        push_bytes!(&mut vec, 0 as u32);
        Self(vec)
    }

    #[cfg(test)]
    fn assert_size(&self) {
        let size = u32::from_le_bytes([self.0[1], self.0[2], self.0[3], self.0[4]]) as usize;
        assert_eq!(size, self.0.len() - 5);
    }

    fn update_size(&mut self) {
        let size = self.0.len() - 5;
        let bytes = &mut self.0;
        bytes[1] = size as u8;
        bytes[2] = (size >> 8) as u8;
        bytes[3] = (size >> 16) as u8;
        bytes[4] = (size >> 24) as u8;
    }

    pub fn as_bytes(&self) -> &[u8] {
        return self.0.as_slice();
    }
}

impl core::fmt::Write for Info {
    fn write_str(&mut self, s: &str) -> Result<(), core::fmt::Error> {
        const MAX_CAPACITY: usize = 500;

        let space_left = self.0.capacity() - self.0.len();
        if s.len() > space_left {
            if self.0.capacity() < MAX_CAPACITY {
                self.0.reserve(MAX_CAPACITY);
            } else {
                return Ok(());
            }
        }

        self.0.extend_from_slice(s.as_bytes());
        self.update_size();
        Ok(())
    }
}

pub fn connection_info_v4(
    id: u64,
    process_id: u64,
    direction: u8,
    protocol: u8,
    local_ip: [u8; 4],
    remote_ip: [u8; 4],
    local_port: u16,
    remote_port: u16,
    payload_layer: u8,
    payload: &[u8],
) -> Info {
    let mut size = get_combined_size!(
        id,
        process_id,
        direction,
        protocol,
        local_ip,
        remote_ip,
        local_port,
        remote_port,
        payload_layer,
        payload.len() as u32
    );
    size += payload.len();

    let mut info = Info::new(InfoType::ConnectionIpv4, size);
    let vec = &mut info.0;
    push_bytes!(vec, id);
    push_bytes!(vec, process_id);
    push_bytes!(vec, direction);
    push_bytes!(vec, protocol);
    push_bytes!(vec, local_ip);
    push_bytes!(vec, remote_ip);
    push_bytes!(vec, local_port);
    push_bytes!(vec, remote_port);
    push_bytes!(vec, payload_layer);
    push_bytes!(vec, payload.len() as u32);
    push_bytes!(vec, payload);
    info
}

pub fn connection_info_v6(
    id: u64,
    process_id: u64,
    direction: u8,
    protocol: u8,
    local_ip: [u8; 16],
    remote_ip: [u8; 16],
    local_port: u16,
    remote_port: u16,
    payload_layer: u8,
    payload: &[u8],
) -> Info {
    let mut size = get_combined_size!(
        id,
        process_id,
        direction,
        protocol,
        local_ip,
        remote_ip,
        local_port,
        remote_port,
        payload_layer,
        payload.len() as u32
    );
    size += payload.len();
    let mut info = Info::new(InfoType::ConnectionIpv6, size);
    let vec = &mut info.0;
    push_bytes!(vec, id);
    push_bytes!(vec, process_id);
    push_bytes!(vec, direction);
    push_bytes!(vec, protocol);
    push_bytes!(vec, local_ip);
    push_bytes!(vec, remote_ip);
    push_bytes!(vec, local_port);
    push_bytes!(vec, remote_port);
    push_bytes!(vec, payload_layer);
    push_bytes!(vec, payload.len() as u32);
    if !payload.is_empty() {
        push_bytes!(vec, payload);
    }
    info
}

pub fn connection_end_event_v4_info(
    process_id: u64,
    direction: u8,
    protocol: u8,
    local_ip: [u8; 4],
    remote_ip: [u8; 4],
    local_port: u16,
    remote_port: u16,
) -> Info {
    let size = get_combined_size!(
        process_id,
        direction,
        protocol,
        local_ip,
        remote_ip,
        local_port,
        remote_port
    );
    let mut info = Info::new(InfoType::ConnectionEndEventV4, size);
    let vec = &mut info.0;
    push_bytes!(vec, process_id);
    push_bytes!(vec, direction);
    push_bytes!(vec, protocol);
    push_bytes!(vec, local_ip);
    push_bytes!(vec, remote_ip);
    push_bytes!(vec, local_port);
    push_bytes!(vec, remote_port);
    info
}

pub fn connection_end_event_v6_info(
    process_id: u64,
    direction: u8,
    protocol: u8,
    local_ip: [u8; 16],
    remote_ip: [u8; 16],
    local_port: u16,
    remote_port: u16,
) -> Info {
    let size = get_combined_size!(
        process_id,
        direction,
        protocol,
        local_ip,
        remote_ip,
        local_port,
        remote_port
    );
    let mut info = Info::new(InfoType::ConnectionEndEventV6, size);
    let vec = &mut info.0;
    push_bytes!(vec, process_id);
    push_bytes!(vec, direction);
    push_bytes!(vec, protocol);
    push_bytes!(vec, local_ip);
    push_bytes!(vec, remote_ip);
    push_bytes!(vec, local_port);
    push_bytes!(vec, remote_port);
    info
}

#[repr(u8)]
#[derive(Clone, Copy)]
pub enum Severity {
    Trace = 1,
    Debug = 2,
    Info = 3,
    Warning = 4,
    Error = 5,
    Critical = 6,
    Disabled = 7,
}

// pub fn log_line(severity: Severity, prefix: String, line: String) -> Info {
//     let mut size = get_combined_size!(severity);
//     size += prefix.len() + line.len();

//     let mut info = Info::new(InfoType::LogLine, size);
//     let vec = &mut info.0;
//     push_bytes!(vec, severity as u8);
//     push_bytes!(vec, prefix.as_bytes());
//     push_bytes!(vec, line.as_bytes());
//     info
// }

pub fn log_line(severity: Severity, capacity: usize) -> Info {
    let mut info = Info::with_capacity(InfoType::LogLine, capacity);
    let vec = &mut info.0;
    push_bytes!(vec, severity as u8);
    info
}

// Special struct for Bandwidth stats
pub struct BandwidthValueV4 {
    pub local_ip: [u8; 4],
    pub local_port: u16,
    pub remote_ip: [u8; 4],
    pub remote_port: u16,
    pub transmitted_bytes: u64,
    pub received_bytes: u64,
}

impl BandwidthValueV4 {
    fn get_size(&self) -> usize {
        get_combined_size!(
            self.local_ip,
            self.local_port,
            self.remote_ip,
            self.remote_port,
            self.transmitted_bytes,
            self.received_bytes
        )
    }
}

impl PushBytes for BandwidthValueV4 {
    fn push(self, vec: &mut Vec<u8>) {
        push_bytes!(vec, self.local_ip);
        push_bytes!(vec, self.local_port);
        push_bytes!(vec, self.remote_ip);
        push_bytes!(vec, self.remote_port);
        push_bytes!(vec, self.transmitted_bytes);
        push_bytes!(vec, self.received_bytes);
    }
}

pub struct BandwidthValueV6 {
    pub local_ip: [u8; 16],
    pub local_port: u16,
    pub remote_ip: [u8; 16],
    pub remote_port: u16,
    pub transmitted_bytes: u64,
    pub received_bytes: u64,
}

impl BandwidthValueV6 {
    fn get_size(&self) -> usize {
        get_combined_size!(
            self.local_ip,
            self.local_port,
            self.remote_ip,
            self.remote_port,
            self.transmitted_bytes,
            self.received_bytes
        )
    }
}

impl PushBytes for BandwidthValueV6 {
    fn push(self, vec: &mut Vec<u8>) {
        push_bytes!(vec, self.local_ip);
        push_bytes!(vec, self.local_port);
        push_bytes!(vec, self.remote_ip);
        push_bytes!(vec, self.remote_port);
        push_bytes!(vec, self.transmitted_bytes);
        push_bytes!(vec, self.received_bytes);
    }
}

pub fn bandiwth_stats_array_v4(protocol: u8, values: Vec<BandwidthValueV4>) -> Info {
    let mut size = get_combined_size!(protocol, values.len() as u32);

    if !values.is_empty() {
        size += values[0].get_size() * values.len();
    }

    let mut info = Info::new(InfoType::BandwidthStatsV4, size);
    let vec = &mut info.0;
    push_bytes!(vec, protocol);
    push_bytes!(vec, values.len() as u32);
    for v in values {
        push_bytes!(vec, v);
    }
    info
}

pub fn bandiwth_stats_array_v6(protocol: u8, values: Vec<BandwidthValueV6>) -> Info {
    let mut size = get_combined_size!(protocol, values.len() as u32);

    if !values.is_empty() {
        size += values[0].get_size() * values.len();
    }

    let mut info = Info::new(InfoType::BandwidthStatsV6, size);
    let vec = &mut info.0;
    push_bytes!(vec, protocol);
    push_bytes!(vec, values.len() as u32);
    for v in values {
        push_bytes!(vec, v);
    }
    info
}

#[cfg(test)]
use std::fs::File;
#[cfg(test)]
use std::io::Write;

#[cfg(test)]
use rand::seq::SliceRandom;

#[test]
fn generate_test_info_file() -> Result<(), std::io::Error> {
    let mut file = File::create("../kextinterface/testdata/rust_info_test.bin")?;
    let enums = [
        InfoType::LogLine,
        InfoType::ConnectionIpv4,
        InfoType::ConnectionIpv6,
        InfoType::ConnectionEndEventV4,
        InfoType::ConnectionEndEventV6,
        InfoType::BandwidthStatsV4,
        InfoType::BandwidthStatsV6,
    ];

    let mut selected: Vec<InfoType> = Vec::with_capacity(1000);
    let mut rng = rand::thread_rng();
    for _ in 0..selected.capacity() {
        selected.push(enums.choose(&mut rng).unwrap().clone());
    }
    // Write wrong size data. To make sure that mismatches between kext and portmaster are handled properly.
    let mut info = connection_info_v6(
        1,
        2,
        3,
        4,
        [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
        [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],
        5,
        6,
        7,
        &[1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
    );
    info.assert_size();
    info.0[0] = InfoType::ConnectionIpv4 as u8;
    file.write_all(&info.0)?;

    for value in selected {
        file.write_all(&match value {
            InfoType::LogLine => {
                let mut info = log_line(Severity::Trace, 5);
                use std::fmt::Write;
                _ = write!(info, "prefix: test log");
                info.assert_size();
                info.0
            }
            InfoType::ConnectionIpv4 => {
                let info = connection_info_v4(
                    1,
                    2,
                    3,
                    4,
                    [1, 2, 3, 4],
                    [2, 3, 4, 5],
                    5,
                    6,
                    7,
                    &[1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
                );
                info.assert_size();
                info.0
            }

            InfoType::ConnectionIpv6 => {
                let info = connection_info_v6(
                    1,
                    2,
                    3,
                    4,
                    [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
                    [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],
                    5,
                    6,
                    7,
                    &[1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
                );
                info.assert_size();
                info.0
            }
            InfoType::ConnectionEndEventV4 => {
                let info = connection_end_event_v4_info(1, 2, 3, [1, 2, 3, 4], [2, 3, 4, 5], 4, 5);
                info.assert_size();
                info.0
            }
            InfoType::ConnectionEndEventV6 => {
                let info = connection_end_event_v6_info(
                    1,
                    2,
                    3,
                    [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
                    [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],
                    4,
                    5,
                );
                info.assert_size();
                info.0
            }
            InfoType::BandwidthStatsV4 => {
                let mut vec = Vec::new();
                vec.push(BandwidthValueV4 {
                    local_ip: [1, 2, 3, 4],
                    local_port: 1,
                    remote_ip: [2, 3, 4, 5],
                    remote_port: 2,
                    transmitted_bytes: 3,
                    received_bytes: 4,
                });
                vec.push(BandwidthValueV4 {
                    local_ip: [1, 2, 3, 4],
                    local_port: 5,
                    remote_ip: [2, 3, 4, 5],
                    remote_port: 6,
                    transmitted_bytes: 7,
                    received_bytes: 8,
                });
                let info = bandiwth_stats_array_v4(1, vec);
                info.assert_size();
                info.0
            }
            InfoType::BandwidthStatsV6 => {
                let mut vec = Vec::new();
                vec.push(BandwidthValueV6 {
                    local_ip: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
                    local_port: 1,
                    remote_ip: [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],
                    remote_port: 2,
                    transmitted_bytes: 3,
                    received_bytes: 4,
                });
                vec.push(BandwidthValueV6 {
                    local_ip: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
                    local_port: 5,
                    remote_ip: [2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],
                    remote_port: 6,
                    transmitted_bytes: 7,
                    received_bytes: 8,
                });
                let info = bandiwth_stats_array_v6(1, vec);
                info.assert_size();
                info.0
            }
        })?;
    }

    return Ok(());
}
