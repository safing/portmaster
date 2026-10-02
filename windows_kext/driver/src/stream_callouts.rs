use smoltcp::wire::{IpProtocol, Ipv4Address, Ipv6Address};
use wdk::filter_engine::{callout_data::CalloutData, layer, net_buffer::NetBufferListIter};

use crate::{bandwidth, connection::Direction, device::Device};

#[inline]
fn get_stream_direction_and_length(data: &mut CalloutData) -> Option<(Direction, usize)> {
    let packet = data.get_stream_callout_packet()?;
    let direction = if packet.is_receive() {
        Direction::Inbound
    } else {
        Direction::Outbound
    };
    // WFP also reports stream lifecycle indications that carry no payload.
    // They must not create zero-valued bandwidth entries.
    let data_length = packet.get_data_len();
    if data_length == 0 {
        return None;
    }
    Some((direction, data_length))
}

pub fn stream_layer_tcp_v4(mut data: CalloutData) {
    type Fields = layer::FieldsStreamV4;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let Some((direction, data_length)) = get_stream_direction_and_length(&mut data) else {
        return;
    };

    let key = bandwidth::Key {
        local_ip: Ipv4Address::from_bytes(
            &data
                .get_value_u32(Fields::IpLocalAddress as usize)
                .to_be_bytes(),
        ),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: Ipv4Address::from_bytes(
            &data
                .get_value_u32(Fields::IpRemoteAddress as usize)
                .to_be_bytes(),
        ),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };
    device
        .bandwidth_stats
        .update_tcp_v4(key, direction, data_length);
}

pub fn stream_layer_tcp_v6(mut data: CalloutData) {
    type Fields = layer::FieldsStreamV6;

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let Some((direction, data_length)) = get_stream_direction_and_length(&mut data) else {
        return;
    };

    let key = bandwidth::Key {
        local_ip: Ipv6Address::from_bytes(
            data.get_value_byte_array16(Fields::IpLocalAddress as usize),
        ),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: Ipv6Address::from_bytes(
            data.get_value_byte_array16(Fields::IpRemoteAddress as usize),
        ),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };
    device
        .bandwidth_stats
        .update_tcp_v6(key, direction, data_length);
}

/// Returns the number of transport payload bytes described by the layer data.
///
/// The datagram data layer positions the net buffer differently per direction:
/// for outbound packets the data starts at the transport header, for inbound
/// packets the transport header has already been consumed and the data starts
/// at the payload. The UDP header is therefore subtracted for outbound traffic
/// only, and per net buffer, since a single net buffer list may carry several
/// datagrams.
#[inline]
fn get_datagram_payload_length(data: &CalloutData, direction: Direction) -> usize {
    // SAFETY: This helper is called only from datagram-data classify handlers.
    // WFP owns the layer-data NBL chain and keeps it stable for the callback;
    // every yielded wrapper is consumed by the selected iterator.
    let nbls = unsafe { NetBufferListIter::new(data.get_layer_data() as _) };
    match direction {
        Direction::Outbound => nbls
            .map(|nbl| nbl.get_data_length_excluding_header(8))
            .sum(),
        Direction::Inbound => nbls.map(|nbl| nbl.get_data_length() as usize).sum(),
    }
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
#[inline]
fn is_self_injected(device: &Device, data: &CalloutData, ipv6: bool) -> bool {
    // SAFETY: Both callers are datagram-data classify handlers. Their layer data
    // is a WFP-owned NBL that remains live through this synchronous query.
    unsafe {
        device
            .injector
            .was_network_packet_injected_by_self(data.get_layer_data() as _, ipv6)
    }
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
    let protocol = IpProtocol::from(data.get_value_u8(Fields::IpProtocol as usize));
    if protocol != IpProtocol::Udp {
        return;
    }

    // FWP_DIRECTION is declared as FWP_UINT32 at the datagram data layers, not
    // FWP_UINT8. Reading uint8 from the union happens to work on little-endian
    // for the values 0 and 1 because they fit in the low byte, but it is reading
    // the wrong union member and would break on any wider value.
    let direction = if data.get_value_u32(Fields::Direction as usize) == 0 {
        Direction::Outbound
    } else {
        Direction::Inbound
    };

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

    let key = bandwidth::Key {
        local_ip: Ipv4Address::from_bytes(
            &data
                .get_value_u32(Fields::IpLocalAddress as usize)
                .to_be_bytes(),
        ),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: Ipv4Address::from_bytes(
            &data
                .get_value_u32(Fields::IpRemoteAddress as usize)
                .to_be_bytes(),
        ),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };
    device
        .bandwidth_stats
        .update_udp_v4(key, direction, data_length);
}

pub fn stream_layer_udp_v6(data: CalloutData) {
    type Fields = layer::FieldsDatagramDataV6;

    let Some(device) = crate::entry::get_device() else {
        return;
    };

    let protocol = IpProtocol::from(data.get_value_u8(Fields::IpProtocol as usize));
    if protocol != IpProtocol::Udp {
        return;
    }

    // FWP_DIRECTION is declared as FWP_UINT32 at the datagram data layers, not
    // FWP_UINT8. Reading uint8 from the union happens to work on little-endian
    // for the values 0 and 1 because they fit in the low byte, but it is reading
    // the wrong union member and would break on any wider value.
    let direction = if data.get_value_u32(Fields::Direction as usize) == 0 {
        Direction::Outbound
    } else {
        Direction::Inbound
    };

    // See stream_layer_udp_v4.
    if matches!(direction, Direction::Outbound) && is_self_injected(device, &data, true) {
        return;
    }

    let data_length = get_datagram_payload_length(&data, direction);
    if data_length == 0 {
        return;
    }

    let key = bandwidth::Key {
        local_ip: Ipv6Address::from_bytes(
            data.get_value_byte_array16(Fields::IpLocalAddress as usize),
        ),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: Ipv6Address::from_bytes(
            data.get_value_byte_array16(Fields::IpRemoteAddress as usize),
        ),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };
    device
        .bandwidth_stats
        .update_udp_v6(key, direction, data_length);
}
