use smoltcp::wire::{Ipv4Address, Ipv6Address};
use wdk::filter_engine::{callout_data::CalloutData, layer, net_buffer::NetBufferListIter};

use crate::{bandwidth, connection::Direction};

pub fn stream_layer_tcp_v4(data: CalloutData) {
    type Fields = layer::FieldsStreamV4;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let mut direction = Direction::Outbound;
    let data_length = if let Some(packet) = data.get_stream_callout_packet() {
        if packet.is_receive() {
            direction = Direction::Inbound;
        }
        packet.get_data_len()
    } else {
        return;
    };
    let local_ip = Ipv4Address::from_bytes(
        &data
            .get_value_u32(Fields::IpLocalAddress as usize)
            .to_be_bytes(),
    );
    let local_port = data.get_value_u16(Fields::IpLocalPort as usize);
    let remote_ip = Ipv4Address::from_bytes(
        &data
            .get_value_u32(Fields::IpRemoteAddress as usize)
            .to_be_bytes(),
    );
    let remote_port = data.get_value_u16(Fields::IpRemotePort as usize);
    match direction {
        Direction::Outbound => {
            device.bandwidth_stats.update_tcp_v4_tx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
        Direction::Inbound => {
            device.bandwidth_stats.update_tcp_v4_rx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
    }
}

pub fn stream_layer_tcp_v6(data: CalloutData) {
    type Fields = layer::FieldsStreamV6;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let mut direction = Direction::Outbound;
    let data_length = if let Some(packet) = data.get_stream_callout_packet() {
        if packet.is_receive() {
            direction = Direction::Inbound;
        }
        packet.get_data_len()
    } else {
        return;
    };

    if data_length == 0 {
        return;
    }
    let local_ip =
        Ipv6Address::from_bytes(data.get_value_byte_array16(Fields::IpLocalAddress as usize));
    let local_port = data.get_value_u16(Fields::IpLocalPort as usize);

    let remote_ip =
        Ipv6Address::from_bytes(data.get_value_byte_array16(Fields::IpRemoteAddress as usize));
    let remote_port = data.get_value_u16(Fields::IpRemotePort as usize);

    match direction {
        Direction::Outbound => {
            device.bandwidth_stats.update_tcp_v6_tx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
        Direction::Inbound => {
            device.bandwidth_stats.update_tcp_v6_rx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
    }
}

pub fn stream_layer_udp_v4(data: CalloutData) {
    type Fields = layer::FieldsDatagramDataV4;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let mut data_length: usize = 0;
    for nbl in NetBufferListIter::new(data.get_layer_data() as _) {
        data_length += nbl.get_data_length() as usize;
    }
    let mut direction = Direction::Inbound;
    if data.get_value_u8(Fields::Direction as usize) == 0 {
        direction = Direction::Outbound;
    }

    let local_ip = Ipv4Address::from_bytes(
        &data
            .get_value_u32(Fields::IpLocalAddress as usize)
            .to_be_bytes(),
    );
    let local_port = data.get_value_u16(Fields::IpLocalPort as usize);
    let remote_ip = Ipv4Address::from_bytes(
        &data
            .get_value_u32(Fields::IpRemoteAddress as usize)
            .to_be_bytes(),
    );
    let remote_port = data.get_value_u16(Fields::IpRemotePort as usize);
    match direction {
        Direction::Outbound => {
            device.bandwidth_stats.update_udp_v4_tx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
        Direction::Inbound => {
            device.bandwidth_stats.update_udp_v4_rx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
    }
}

pub fn stream_layer_udp_v6(data: CalloutData) {
    type Fields = layer::FieldsDatagramDataV6;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let mut data_length: usize = 0;
    for nbl in NetBufferListIter::new(data.get_layer_data() as _) {
        data_length += nbl.get_data_length() as usize;
    }
    let mut direction = Direction::Inbound;
    if data.get_value_u8(Fields::Direction as usize) == 0 {
        direction = Direction::Outbound;
    }

    let local_ip =
        Ipv6Address::from_bytes(data.get_value_byte_array16(Fields::IpLocalAddress as usize));
    let local_port = data.get_value_u16(Fields::IpLocalPort as usize);
    let remote_ip =
        Ipv6Address::from_bytes(data.get_value_byte_array16(Fields::IpRemoteAddress as usize));
    let remote_port = data.get_value_u16(Fields::IpRemotePort as usize);
    match direction {
        Direction::Outbound => {
            device.bandwidth_stats.update_udp_v6_tx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
        Direction::Inbound => {
            device.bandwidth_stats.update_udp_v6_rx(
                bandwidth::Key {
                    local_ip,
                    local_port,
                    remote_ip,
                    remote_port,
                },
                data_length,
            );
        }
    }
}
