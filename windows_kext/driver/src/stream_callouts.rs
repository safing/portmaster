use smoltcp::wire::{Ipv4Address, Ipv6Address};
use wdk::filter_engine::{callout_data::CalloutData, layer, net_buffer::NetBufferListIter};

use crate::{bandwidth, connection::Direction, device::Device};

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

/// Returns the number of transport payload bytes described by the layer data.
///
/// The datagram data layer positions the net buffer differently per direction:
/// for outbound packets the data starts at the transport header, for inbound
/// packets the transport header has already been consumed and the data starts
/// at the payload. The UDP header is therefore subtracted for outbound traffic
/// only, and per net buffer, since a single net buffer list may carry several
/// datagrams.
fn get_datagram_payload_length(data: &CalloutData, direction: Direction) -> usize {
    let mut length: usize = 0;
    for nbl in NetBufferListIter::new(data.get_layer_data() as _) {
        length += match direction {
            Direction::Outbound => nbl.get_data_length_excluding_header(8),
            Direction::Inbound => nbl.get_data_length() as usize,
        };
    }
    length
}

/// Returns true if the net buffer list of this indication is a copy that the
/// driver re-injected itself.
///
/// Only the *first* packet of a connection is absorbed and re-injected: it is
/// pended while Portmaster decides a verdict. Once the verdict is cached, later
/// packets are permitted in place and never re-injected.
///
/// The caller must combine this with the direction, because the meaning of a
/// self-injected copy differs per direction:
///
/// - Outbound: the original datagram is indicated *before* it is absorbed, and
///   the re-injected copy is indicated again. Two indications, one datagram, so
///   the copy is a duplicate and must be skipped.
/// - Inbound: the original is absorbed at the inbound packet layer *below* this
///   layer, so it never reaches here. The re-injected copy is the only
///   indication that exists, and skipping it loses the bytes entirely.
///
/// Skipping inbound copies is what broke accounting for a datagram received from
/// a remote host: `rx` dropped to 0 because that packet opened the connection and
/// was therefore pended and re-injected.
fn is_self_injected(device: &Device, data: &CalloutData, ipv6: bool) -> bool {
    device
        .injector
        .was_network_packet_injected_by_self(data.get_layer_data() as _, ipv6)
}

pub fn stream_layer_udp_v4(data: CalloutData) {
    type Fields = layer::FieldsDatagramDataV4;

    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // The datagram data layer is not UDP only: raw sockets and ICMP are
    // indicated here as well. Their bytes must not be attributed to UDP,
    // especially since ICMP reports type/code in the port fields, which would
    // corrupt the counters of an unrelated UDP connection.
	let protocol = smoltcp::wire::IpProtocol::from(data.get_value_u8(Fields::IpProtocol as usize));
    if protocol != smoltcp::wire::IpProtocol::Udp {
        return;
    }

    // FWP_DIRECTION is declared as FWP_UINT32 at the datagram data layers, not
    // FWP_UINT8. Reading uint8 from the union happens to work on little-endian
    // for the values 0 and 1 because they fit in the low byte, but it is reading
    // the wrong union member and would break on any wider value.
    let mut direction = Direction::Inbound;
    if data.get_value_u32(Fields::Direction as usize) == 0 {
        direction = Direction::Outbound;
    }

    // Skip only the outbound re-injected copy: it is the second indication of a
    // datagram already counted when the original was indicated. Inbound copies are
    // the only indication their datagram gets and must be counted.
    if matches!(direction, Direction::Outbound) && is_self_injected(device, &data, false) {
        return;
    }

    let data_length = get_datagram_payload_length(&data, direction);
    if data_length == 0 {
        return;
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

	let protocol = smoltcp::wire::IpProtocol::from(data.get_value_u8(Fields::IpProtocol as usize));
    if protocol != smoltcp::wire::IpProtocol::Udp {
        return;
    }

    // FWP_DIRECTION is declared as FWP_UINT32 at the datagram data layers, not
    // FWP_UINT8. Reading uint8 from the union happens to work on little-endian
    // for the values 0 and 1 because they fit in the low byte, but it is reading
    // the wrong union member and would break on any wider value.
    let mut direction = Direction::Inbound;
    if data.get_value_u32(Fields::Direction as usize) == 0 {
        direction = Direction::Outbound;
    }

    // See stream_layer_udp_v4.
    if matches!(direction, Direction::Outbound) && is_self_injected(device, &data, true) {
        return;
    }

    let data_length = get_datagram_payload_length(&data, direction);
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
