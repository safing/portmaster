use crate::connection::{Connection, ConnectionV4, ConnectionV6, Direction, Verdict};
use crate::connection_map::Key;
use crate::device::{Device, Packet};

use crate::info;
use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv4Address, Ipv6Address, IPV4_HEADER_LEN, IPV6_HEADER_LEN,
};
use wdk::filter_engine::callout_data::CalloutData;
use wdk::filter_engine::layer::{self, FieldsAleAuthConnectV4, FieldsAleAuthConnectV6, ValueType};
use wdk::filter_engine::net_buffer::NetBufferList;
use wdk::filter_engine::packet::{Injector, TransportPacketList};

// ALE Layers

#[derive(Debug)]
#[allow(dead_code)]
struct AleLayerData {
    is_ipv6: bool,
    reauthorize: bool,
    process_id: u64,
    protocol: IpProtocol,
    direction: Direction,
    local_ip: IpAddress,
    local_port: u16,
    remote_ip: IpAddress,
    remote_port: u16,
    interface_index: u32,
    sub_interface_index: u32,
}

impl AleLayerData {
    fn as_key(&self) -> Key {
        let mut local_port = 0;
        let mut remote_port = 0;
        match self.protocol {
            IpProtocol::Tcp | IpProtocol::Udp => {
                local_port = self.local_port;
                remote_port = self.remote_port;
            }
            _ => {}
        }

        Key {
            protocol: self.protocol,
            local_address: self.local_ip,
            local_port,
            remote_address: self.remote_ip,
            remote_port,
        }
    }
}

fn get_protocol(data: &CalloutData, index: usize) -> IpProtocol {
    IpProtocol::from(data.get_value_u8(index))
}

/// Reads a `FWP_UINT32` field, returning 0 when the field is not populated.
///
/// Several fields are only filled in at some layers. Reading the union member
/// regardless yields an unrelated value rather than an error, so the type is
/// checked first.
fn get_u32_or_zero(data: &CalloutData, index: usize) -> u32 {
    match data.get_value_type(index) {
        ValueType::FwpUint32 => data.get_value_u32(index),
        _ => 0,
    }
}

fn get_ipv4_address(data: &CalloutData, index: usize) -> IpAddress {
    IpAddress::Ipv4(Ipv4Address::from_bytes(
        &data.get_value_u32(index).to_be_bytes(),
    ))
}

fn get_ipv4_address_if_present(data: &CalloutData, index: usize) -> Option<IpAddress> {
    matches!(data.get_value_type(index), ValueType::FwpUint32)
        .then(|| get_ipv4_address(data, index))
}

fn get_ipv6_address(data: &CalloutData, index: usize) -> IpAddress {
    IpAddress::Ipv6(Ipv6Address::from_bytes(data.get_value_byte_array16(index)))
}

fn get_ipv6_address_if_present(data: &CalloutData, index: usize) -> Option<IpAddress> {
    matches!(data.get_value_type(index), ValueType::FwpByteArray16Type)
        .then(|| get_ipv6_address(data, index))
}

pub fn ale_layer_connect_v4(data: CalloutData) {
    type Fields = FieldsAleAuthConnectV4;
    let ale_data = AleLayerData {
        is_ipv6: false,
        reauthorize: data.is_reauthorize(Fields::Flags as usize),
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        direction: Direction::Outbound,
        local_ip: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        interface_index: get_u32_or_zero(&data, Fields::InterfaceIndex as usize),
        sub_interface_index: get_u32_or_zero(&data, Fields::SubInterfaceIndex as usize),
    };

    ale_layer_auth(data, ale_data);
}

pub fn ale_layer_connect_v6(data: CalloutData) {
    type Fields = FieldsAleAuthConnectV6;

    let ale_data = AleLayerData {
        is_ipv6: true,
        reauthorize: data.is_reauthorize(Fields::Flags as usize),
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        direction: Direction::Outbound,
        local_ip: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        // Read only when actually populated. WFP reports SubInterfaceIndex as
        // FWP_EMPTY at the connect authorization layer - there is no interface
        // binding yet at that point - so reading it unconditionally returned
        // whatever the union held, and that value went on to be used as an
        // injection parameter. The v4 path already passes zeros here.
        interface_index: get_u32_or_zero(&data, Fields::InterfaceIndex as usize),
        sub_interface_index: get_u32_or_zero(&data, Fields::SubInterfaceIndex as usize),
    };

    ale_layer_auth(data, ale_data);
}

fn ale_layer_auth(mut data: CalloutData, ale_data: AleLayerData) {
    // Make the default path as drop.
    data.block_and_absorb();

    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // Check if packet was previously injected from the packet layer.
    if device
        .injector
        .was_network_packet_injected_by_self(data.get_layer_data() as _, ale_data.is_ipv6)
    {
        data.action_permit();
        return;
    }

    match ale_data.protocol {
        IpProtocol::Tcp | IpProtocol::Udp => {
            // Only TCP and UDP make sense to be supported in the ALE layer.
            // Everything else is not associated with a connection and will be handled in the packet layer.
        }
        _ => {
            // Outbound: Will be handled by packet layer next.
            // Inbound: Was already handled by the packet layer.
            data.action_permit();
            return;
        }
    }

    let key = ale_data.as_key();

    // Outbound UDP is decided at the IP packet layer, not here.
    //
    // Holding a datagram at this layer corrupts the send status seen by the
    // application. Both ways of holding it are broken for Registered I/O:
    // pend_operation freezes the endpoint, so datagrams submitted concurrently
    // are completed with WSAEINVAL and never reach the stack at all; absorbing
    // instead keeps every datagram (they are re-injected once a verdict arrives)
    // but still completes them with WSAEINVAL, so the application is told a
    // delivered datagram failed. A caller that retries on error duplicates it.
    //
    // The IP packet layer sits below the socket, so by the time a datagram is
    // absorbed there its send has already completed successfully. Verified with
    // RIOSendEx: six concurrent datagrams all report success, all six are
    // delivered, and the packet layer still gets to decide each one.
    //
    // Register the connection here so the process ID is recorded while it is
    // available - the packet layer has no access to it and would store 0 - then
    // permit and let the packet layer classify. Inbound is unaffected: it is
    // already decided at the packet layer before reaching this one.
    if matches!(ale_data.protocol, IpProtocol::Udp)
        && matches!(ale_data.direction, Direction::Outbound)
    {
        // Only register once. A cached connection may already carry a verdict,
        // and overwriting it with Undecided would send it for a decision again.
        let known = if ale_data.is_ipv6 {
            device
                .connection_cache
                .read_connection_v6(&key, |_| -> Option<()> { Some(()) })
        } else {
            device
                .connection_cache
                .read_connection_v4(&key, |_| -> Option<()> { Some(()) })
        };

        if known.is_none() {
            crate::dbg!(
                "ale layer registering udp connection for packet layer: {} PID: {}",
                key,
                ale_data.process_id
            );
            if ale_data.is_ipv6 {
                match ConnectionV6::from_key(&key, ale_data.process_id, ale_data.direction) {
                    Ok(conn) => device.connection_cache.add_connection_v6(conn),
                    Err(err) => crate::err!("failed to build connection: {}", err),
                }
            } else {
                match ConnectionV4::from_key(&key, ale_data.process_id, ale_data.direction) {
                    Ok(conn) => device.connection_cache.add_connection_v4(conn),
                    Err(err) => crate::err!("failed to build connection: {}", err),
                }
            }
        }

        data.action_permit();

        if device.is_owner_pid(ale_data.process_id as u32) {
            // Keep other firewalls from overriding the permit on Portmaster's own
            // traffic, matching the cached-verdict path below.
            data.clear_write_flag();
        }
        return;
    }

    // Check if connection is already in cache.
    let verdict = if ale_data.is_ipv6 {
        device
            .connection_cache
            .read_connection_v6(&key, |conn| -> Option<Verdict> {
                // Function is behind spin lock, just copy and return.
                Some(conn.verdict)
            })
    } else {
        device
            .connection_cache
            .read_connection_v4(&ale_data.as_key(), |conn| -> Option<Verdict> {
                // Function is behind spin lock, just copy and return.
                Some(conn.verdict)
            })
    };

    // Connection already in cache.
    if let Some(verdict) = verdict {
        crate::dbg!("processing existing connection: {} {}", key, verdict);
        match verdict {
            // No verdict yet
            Verdict::Undecided => {
                crate::dbg!("saving packet: {}", key);
                // Connection is already pended. Save packet and wait for verdict.
                match save_packet(device, &mut data, &ale_data, false) {
                    Ok(packet) => {
                        let info = device.packet_cache.push(
                            (key, packet),
                            ale_data.process_id,
                            ale_data.direction,
                            true,
                        );
                        if let Some(info) = info {
                            let _ = device.event_queue.push(info);
                        }
                    }
                    Err(err) => {
                        crate::err!("failed to pend packet: {}", err);
                    }
                };
                data.block_and_absorb();
            }
            // There is a verdict
            Verdict::PermanentAccept
            | Verdict::Accept
            | Verdict::RedirectNameServer
            | Verdict::RedirectTunnel
            | Verdict::RedirectSplitTunnel => {
                // Continue to packet layer.
                data.action_permit();

                if device.is_owner_pid(ale_data.process_id as u32) && matches!(ale_data.direction, Direction::Outbound) {
                    // If this is Portmaster's own outbound connection, clear the write flag
                    // to prevent subsequent filters in the chain from overriding the permit action.
                    // This prevents other firewall applications from blocking Portmaster's own connections.
                    data.clear_write_flag();
                }
            }
            Verdict::PermanentBlock | Verdict::Undeterminable | Verdict::Failed => {
                // Packet layer will not see this connection.
                crate::dbg!("permanent block {}", key);
                data.action_block_hard();
            }
            Verdict::PermanentDrop => {
                // Packet layer will not see this connection.
                crate::dbg!("permanent drop {}", key);
                data.block_and_absorb();
            }
            Verdict::Block => {
                if let Direction::Outbound = ale_data.direction {
                    // Handled by packet layer.
                    data.action_permit();
                } else {
                    // packet layer will still see the packets.
                    data.action_block_hard();
                }
            }
            Verdict::Drop => {
                if let Direction::Outbound = ale_data.direction {
                    // Handled by packet layer.
                    data.action_permit();
                } else {
                    // packet layer will still see the packets.
                    data.block_and_absorb();
                }
            }
        }
    } else {
        crate::dbg!("pending connection: {} {}", key, ale_data.direction);
        // Only first packet of a connection can be pended: reauthorize == false
        //
        // Outbound UDP never reaches this point - it returns above and is decided
        // at the packet layer, because holding a datagram at this layer breaks the
        // send status reported to the application. Inbound UDP still arrives here
        // and is safe to pend: there is no application send operation to freeze.
        let can_pend_connection = !ale_data.reauthorize;
        match save_packet(device, &mut data, &ale_data, can_pend_connection) {
            Ok(packet) => {
                let info = device.packet_cache.push(
                    (key, packet),
                    ale_data.process_id,
                    ale_data.direction,
                    true,
                );
                if let Some(info) = info {
                    let _ = device.event_queue.push(info);
                }
            }
            Err(err) => {
                crate::err!("failed to pend packet: {}", err);
            }
        };

        // Connection is not in cache, add it.
        crate::dbg!(
            "ale layer adding connection: {} PID: {}",
            key,
            ale_data.process_id
        );
        if ale_data.is_ipv6 {
            let conn =
                ConnectionV6::from_key(&key, ale_data.process_id, ale_data.direction).unwrap();
            device.connection_cache.add_connection_v6(conn);
        } else {
            let conn =
                ConnectionV4::from_key(&key, ale_data.process_id, ale_data.direction).unwrap();
            device.connection_cache.add_connection_v4(conn);
        }

        // Drop packet. It will be re-injected after Portmaster returns a verdict.
        data.block_and_absorb();
    }
}

fn save_packet(
    device: &Device,
    callout_data: &mut CalloutData,
    ale_data: &AleLayerData,
    pend: bool,
) -> Result<Packet, alloc::string::String> {
    let mut packet_list = None;
    let mut save_packet_list = true;
    if ale_data.protocol == IpProtocol::Tcp {
        if let Direction::Outbound = ale_data.direction {
            // Only time a packet data is missing is during connect state of outbound TCP connection.
            // Don't save packet list only if connection is outbound, reauthorize is false and the protocol is TCP.
            save_packet_list = ale_data.reauthorize;
        }
    };
    if save_packet_list {
        packet_list = create_packet_list(device, callout_data, ale_data);
    }
    if pend && matches!(ale_data.protocol, IpProtocol::Tcp | IpProtocol::Udp) {
        match callout_data.pend_operation(packet_list) {
            Ok(classify_defer) => Ok(Packet::AleLayer(classify_defer)),
            Err(err) => Err(alloc::format!("failed to defer connection: {}", err)),
        }
    } else {
        Ok(Packet::AleLayer(callout_data.pend_filter_rest(packet_list)))
    }
}

fn create_packet_list(
    device: &Device,
    callout_data: &mut CalloutData,
    ale_data: &AleLayerData,
) -> Option<TransportPacketList> {
    let mut nbl = NetBufferList::new(callout_data.get_layer_data() as _);
    let mut inbound = false;
    if let Direction::Inbound = ale_data.direction {
        // Retreat by the size WFP reports, not the fixed base header length: with
        // IPv4 options, or an IPv6 extension header chain, the header is longer and
        // a fixed retreat leaves the buffer inside it. See retreat_to_ip_header in
        // packet_callouts.rs for the full reasoning.
        let base = if ale_data.is_ipv6 {
            IPV6_HEADER_LEN
        } else {
            IPV4_HEADER_LEN
        } as u32;
        let size = match callout_data.get_ip_header_size() {
            Some(size) if size >= base && size <= 128 => size,
            _ => base,
        };
        nbl.retreat(size, true);
        inbound = true;
    }

    let address: &[u8] = match &ale_data.remote_ip {
        IpAddress::Ipv4(address) => &address.0,
        IpAddress::Ipv6(address) => &address.0,
    };
    if let Ok(clone) = nbl.clone(&device.network_allocator) {
        return Some(Injector::from_ale_callout(
            ale_data.is_ipv6,
            callout_data,
            clone,
            address,
            inbound,
            ale_data.interface_index,
            ale_data.sub_interface_index,
        ));
    }
    return None;
}

pub fn endpoint_closure_v4(data: CalloutData) {
    type Fields = layer::FieldsAleEndpointClosureV4;
    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let ip_address_type = data.get_value_type(Fields::IpLocalAddress as usize);
    // The remote endpoint is checked as well, not just the local one. WFP leaves
    // IpRemoteAddress and IpRemotePort as FWP_EMPTY for closures that have no
    // remote peer (a listening socket, for example). Reading them regardless
    // returns whatever the union happens to hold, and the resulting key either
    // matches no cached connection - so the entry is never removed and the cache
    // grows - or matches an unrelated one and ends the wrong connection.
    //
    // The v6 path already validates both addresses; this brings v4 in line.
    let remote_address_type = data.get_value_type(Fields::IpRemoteAddress as usize);
    let remote_port_type = data.get_value_type(Fields::IpRemotePort as usize);
    let remote_present = matches!(remote_address_type, ValueType::FwpUint32)
        && matches!(remote_port_type, ValueType::FwpUint16);

    if matches!(ip_address_type, ValueType::FwpUint32) && remote_present {
        let key = Key {
            protocol: get_protocol(&data, Fields::IpProtocol as usize),
            local_address: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
            local_port: data.get_value_u16(Fields::IpLocalPort as usize),
            remote_address: get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
            remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        };

        let conn = device.connection_cache.end_connection_v4(key);
        if let Some(conn) = conn {
            let info = protocol::info::connection_end_event_v4_info(
                data.get_process_id().unwrap_or(0),
                conn.get_direction() as u8,
                u8::from(get_protocol(&data, Fields::IpProtocol as usize)),
                conn.local_address.0,
                conn.remote_address.0,
                conn.local_port,
                conn.remote_port,
            );
            let _ = device.event_queue.push(info);
        }
    } else {
        // Invalid ip address type. Just ignore the error.
        // err!(
        //     device.logger,
        //     "unknown ipv4 address type: {:?}",
        //     ip_address_type
        // );
    }
}

pub fn endpoint_closure_v6(data: CalloutData) {
    type Fields = layer::FieldsAleEndpointClosureV6;
    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let local_ip_address_type = data.get_value_type(Fields::IpLocalAddress as usize);
    let remote_ip_address_type = data.get_value_type(Fields::IpRemoteAddress as usize);
    // Ports are validated too: the addresses being present does not guarantee the
    // port fields are, and an unpopulated port would silently become part of the
    // key. See endpoint_closure_v4 for the consequences.
    let ports_present = matches!(
        data.get_value_type(Fields::IpLocalPort as usize),
        ValueType::FwpUint16
    ) && matches!(
        data.get_value_type(Fields::IpRemotePort as usize),
        ValueType::FwpUint16
    );

    if let ValueType::FwpByteArray16Type = local_ip_address_type {
        if matches!(remote_ip_address_type, ValueType::FwpByteArray16Type) && ports_present {
            let key = Key {
                protocol: get_protocol(&data, Fields::IpProtocol as usize),
                local_address: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
                local_port: data.get_value_u16(Fields::IpLocalPort as usize),
                remote_address: get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
                remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
            };

            let conn = device.connection_cache.end_connection_v6(key);
            if let Some(conn) = conn {
                let info = protocol::info::connection_end_event_v6_info(
                    data.get_process_id().unwrap_or(0),
                    conn.get_direction() as u8,
                    u8::from(get_protocol(&data, Fields::IpProtocol as usize)),
                    conn.local_address.0,
                    conn.remote_address.0,
                    conn.local_port,
                    conn.remote_port,
                );
                let _ = device.event_queue.push(info);
            }
        }
    }
}

/// Records the owning process of a newly bound local endpoint.
///
/// This exists because the inbound IP packet layer cannot determine the process
/// itself. At FWPM_LAYER_INBOUND_IPPACKET_V4/V6 no socket is associated with the
/// packet yet, so WFP supplies no process ID and every inbound connection the
/// packet layer created was reported with PID 0.
///
/// The bind layer is the earliest point where the PID is known, and it runs well
/// before the traffic does. Measured on Windows 11 with a listener on
/// 0.0.0.0:1234: bind indicated the correct PID 4.4 seconds ahead of the first
/// datagram.
///
/// The address cannot be part of the key: a bind to a wildcard address leaves
/// IpLocalAddress as FWP_EMPTY. The address family can and must be, because the
/// two families have independent port spaces.
///
/// Only ports the application named itself are recorded. The layer also fires for
/// stack-assigned ephemeral ports - bind() with port 0, or an implicit bind from
/// connect()/sendto() - and those are skipped; see the comment on the
/// IS_WILDCARD_BIND check below for why they are neither needed nor harmless.
///
/// Registered as an inspection callout, so it never alters a permit/block
/// decision. A failure to record a PID degrades to the old behaviour rather than
/// affecting traffic.
pub fn ale_resource_assignment_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // The address family is taken from the layer, not from the address field:
    // a wildcard bind leaves the address empty, and the family still has to be
    // known because the two port spaces are independent.
    let (ipv6, port, protocol, flags) = match data.layer {
        layer::Layer::AleResourceAssignmentV4 => {
            type Fields = layer::FieldsAleResourceAssignmentV4;
            (
                false,
                data.get_value_u16(Fields::IpLocalPort as usize),
                data.get_value_u8(Fields::IpProtocol as usize),
                data.get_value_u32(Fields::Flags as usize),
            )
        }
        layer::Layer::AleResourceAssignmentV6 => {
            type Fields = layer::FieldsAleResourceAssignmentV6;
            (
                true,
                data.get_value_u16(Fields::IpLocalPort as usize),
                data.get_value_u8(Fields::IpProtocol as usize),
                data.get_value_u32(Fields::Flags as usize),
            )
        }
        _ => return,
    };

    // Only TCP and UDP are tracked. The connection cache is keyed by protocol
    // and port, and other protocols carry no ports - raw sockets and promiscuous
    // mode requests are indicated at this layer as well.
    let protocol = IpProtocol::from(protocol);
    if !matches!(protocol, IpProtocol::Udp | IpProtocol::Tcp) {
        return;
    }

    // Port 0 is never a usable key. When an application asks for any port -
    // bind() with port 0, or no bind() at all - WFP indicates the assignment
    // with the port the stack actually picked and sets
    // FWP_CONDITION_FLAG_IS_WILDCARD_BIND. So a zero here means the field was
    // not populated as expected rather than a real assignment.
    if port == 0 {
        return;
    }

    // Track only ports the application asked for by number.
    //
    // FWP_CONDITION_FLAG_IS_WILDCARD_BIND is set exactly when the application let
    // the stack pick the port - bind() with port 0, or no bind() at all before a
    // send or connect. Measured over the full (address x port) matrix: the flag
    // follows the port, never the address, and stays clear for a named port even
    // when the address is 0.0.0.0 or [::]. (A wildcard address shows up
    // separately, as IpLocalAddress being FWP_EMPTY.)
    //
    // Those stack-assigned ports are the ephemeral local ports of outbound
    // connections, and they do not need this table: outbound traffic is
    // classified at the ALE connect layer, which supplies the process ID
    // directly. Recording them only adds churn - a short-lived entry per
    // outbound connection - and creates the chance of a stale entry colliding
    // with a service that later binds that port number by name.
    //
    // FWP_CONDITION_FLAG_IS_IMPLICIT_BIND would be the more direct test, but it
    // is unusable: it never appeared in any measurement, including sendto and
    // connect on an unbound socket, consistent with being documented as
    // Vista/Server 2008 only.
    //
    // The one case this drops that does need the table is a server that asked for
    // any port and then listened on it: its bind is a wildcard bind, but its
    // accepted connections are inbound and are only ever seen at the packet layer.
    // Those are picked up at the listen layer instead - see `ale_listen_monitor`,
    // which fires on `listen()` and so cannot be confused with the source port of
    // an outbound connection.
    if flags & wdk::consts::FWP_CONDITION_FLAG_IS_WILDCARD_BIND != 0 {
        return;
    }

    // Only store a PID that WFP actually supplied. get_process_id returns None
    // when the metadata field is absent, and storing 0 for that case would
    // overwrite a good entry from an earlier bind on a reused port with a value
    // that means "unknown".
    let Some(process_id) = data.get_process_id() else {
        return;
    };

    device
        .endpoint_pid_cache
        .insert(ipv6, protocol, port, process_id);
}

/// Records the owning process of a socket entering the listening state.
///
/// This covers the endpoints `ale_resource_assignment_monitor` deliberately
/// skips. That handler ignores every bind carrying
/// FWP_CONDITION_FLAG_IS_WILDCARD_BIND, because such a bind is normally the
/// stack picking an ephemeral source port for an outbound connection, which the
/// connect layer already attributes. A server that calls `bind(addr, 0)` and then
/// `listen()` is indistinguishable from that at the bind layer: measured on
/// Windows 11, a Firefox IPC listener on 127.0.0.1:45565 was indicated with
/// flags=0x9 (WILDCARD_BIND | IS_LOOPBACK) and the correct PID, while the client
/// socket of the same pair carried flags=0x8 - the only difference being the
/// loopback bit, which says nothing about the role of the socket. So the flags
/// cannot separate the two cases and the listener's PID was dropped.
///
/// Its inbound half is then only ever seen at the inbound IP packet layer, which
/// has no process ID of its own, and the connection was reported with PID 0. In
/// the same capture the accepting side never appeared at the connect layer at all
/// - no indication for `l: 127.0.0.1:45565` exists - so this layer is the only
/// place where that PID is available before the traffic arrives.
///
/// `listen()` has no UDP equivalent, so this covers TCP only. A UDP server on an
/// ephemeral port is still missed; it has no listen state to observe.
///
/// The entry is removed by the existing release handler, which filters on
/// protocol and port and does not look at the wildcard flag.
///
/// Registered as an inspection callout, so it never alters a permit/block
/// decision.
pub fn ale_listen_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // The address family comes from the layer: a listener on a wildcard address
    // leaves IpLocalAddress empty, and the two families have independent port
    // spaces, so the family has to be part of the key.
    let (ipv6, port) = match data.layer {
        layer::Layer::AleAuthListenV4 => {
            type Fields = layer::FieldsAleAuthListenV4;
            (false, data.get_value_u16(Fields::IpLocalPort as usize))
        }
        layer::Layer::AleAuthListenV6 => {
            type Fields = layer::FieldsAleAuthListenV6;
            (true, data.get_value_u16(Fields::IpLocalPort as usize))
        }
        _ => return,
    };

    // The listen layers carry no IpProtocol field - reaching them at all means
    // TCP, since only a stream socket can listen.
    let protocol = IpProtocol::Tcp;

    // Port 0 is never a usable key. A listening socket always has a port by this
    // point, so a zero here means the field was not populated as expected.
    if port == 0 {
        return;
    }

    // Only store a PID that WFP actually supplied: storing 0 would overwrite a
    // good entry with a value that means "unknown".
    let Some(process_id) = data.get_process_id() else {
        return;
    };

    device
        .endpoint_pid_cache
        .insert(ipv6, protocol, port, process_id);
}


/// Records the owning process of a concrete accepted TCP flow.
///
/// Unlike the listen layer, receive-accept is indicated for listeners that were
/// already running when the driver loaded. This closes that startup gap without a
/// socket snapshot: measured with Firefox already running, WFP indicated
/// `127.0.0.1:18540 <- 127.0.0.1:18541` here with PID 6568 immediately before the
/// packet layer created the same key. No listen indication existed because the
/// listener predated the driver.
///
/// The endpoint table is keyed by the local port rather than the complete flow,
/// so the accepted flow refreshes the listener's entry. The indication identifies
/// the process that actually accepted this connection and arrives before packet
/// classification; the subsequent endpoint lookup therefore creates the
/// connection with this PID instead of 0.
///
/// Registered as an inspection callout, so it never alters the authorization
/// result. Only a usable TCP port and a PID WFP actually supplied are recorded.
pub fn ale_recv_accept_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    let (ipv6, local_port, protocol) = match data.layer {
        layer::Layer::AleAuthRecvAcceptV4 => {
            type Fields = layer::FieldsAleAuthRecvAcceptV4;
            (
                false,
                data.get_value_u16(Fields::IpLocalPort as usize),
                get_protocol(&data, Fields::IpProtocol as usize),
            )
        }
        layer::Layer::AleAuthRecvAcceptV6 => {
            type Fields = layer::FieldsAleAuthRecvAcceptV6;
            (
                true,
                data.get_value_u16(Fields::IpLocalPort as usize),
                get_protocol(&data, Fields::IpProtocol as usize),
            )
        }
        _ => return,
    };

    if protocol != IpProtocol::Tcp || local_port == 0 {
        return;
    }

    let Some(process_id) = data.get_process_id() else {
        return;
    };

    device
        .endpoint_pid_cache
        .insert(ipv6, protocol, local_port, process_id);
}

/// Records the owning process when a TCP flow completes its three-way handshake.
///
/// This layer fires after the SYN/SYN-ACK/ACK handshake completes, for both
/// outbound and inbound connections. It provides a second opportunity to attribute
/// flows that were created at the packet layer with PID=0 - either because the
/// packet layer saw them before any ALE indication, or because of a race between
/// the packet and ALE layers during connection setup.
///
/// For outbound flows, the connect layer already runs before any packet is sent,
/// so this adds no new information. For inbound flows, the packet layer creates
/// the connection when it sees the first packet (often the SYN), and this layer
/// fires immediately after the handshake completes, giving one more chance to
/// resolve a PID=0 entry before user-visible logging.
///
/// This does NOT help connections that were fully established before the driver
/// loaded - their handshake already finished, so this layer never fires for them.
///
/// Registered as an inspection callout.
pub fn ale_flow_established_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    let (ipv6, local_ip, local_port, remote_ip, remote_port, protocol) = match data.layer {
        layer::Layer::AleFlowEstablishedV4 => {
            type Fields = layer::FieldsAleFlowEstablishedV4;
            (
                false,
                get_ipv4_address(&data, Fields::IpLocalAddress as usize),
                data.get_value_u16(Fields::IpLocalPort as usize),
                get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
                data.get_value_u16(Fields::IpRemotePort as usize),
                get_protocol(&data, Fields::IpProtocol as usize),
            )
        }
        layer::Layer::AleFlowEstablishedV6 => {
            type Fields = layer::FieldsAleFlowEstablishedV6;
            (
                true,
                get_ipv6_address(&data, Fields::IpLocalAddress as usize),
                data.get_value_u16(Fields::IpLocalPort as usize),
                get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
                data.get_value_u16(Fields::IpRemotePort as usize),
                get_protocol(&data, Fields::IpProtocol as usize),
            )
        }
        _ => return,
    };

    if protocol != IpProtocol::Tcp {
        return;
    }

    let Some(process_id) = data.get_process_id() else {
        return;
    };

    let key = Key {
        protocol,
        local_address: local_ip,
        local_port,
        remote_address: remote_ip,
        remote_port,
    };

    // Check if connection exists in cache with PID=0, and if so, update it.
    let cached_pid = if ipv6 {
        device
            .connection_cache
            .read_connection_v6(&key, |conn| -> Option<u64> { Some(conn.process_id) })
    } else {
        device
            .connection_cache
            .read_connection_v4(&key, |conn| -> Option<u64> { Some(conn.process_id) })
    };

    if let Some(0) = cached_pid {
        device.connection_cache.update_process_id(&key, process_id);
    }
}

pub fn ale_resource_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // Drop the endpoint -> PID entry for a released port.
    //
    // Done here, before the per-layer handling below, because that handling is
    // conditional on the connection cache having live connections on the port
    // while this entry exists independently of them: it is created on bind, and a
    // socket that never carried traffic has no connection to end. Leaving it
    // behind would attribute a later process's traffic to a dead PID once the
    // port is reused.
    //
    // Measured on Windows 11, one case per port, to establish when a release
    // actually arrives:
    //
    //   accept/close (tcp 1235) - one assignment at bind, one release at listener
    //     close, nothing in between for three accepted connections. An accepted
    //     socket shares the listener's port rather than acquiring it, so it is not
    //     a resource assignment event and the listener keeps its entry.
    //   abrupt termination (udp 1241) - TerminateProcess on the owner still
    //     produced a release with the correct PID. No stale entry.
    //   TCP TIME_WAIT (tcp 1242) - release arrives at close, and an immediate
    //     rebind of the same port succeeded, so the port was not held.
    //   SO_REUSEADDR (udp 1243, and 1245 across two processes) - both binds are
    //     indicated, and the first release arrives while the other owner is still
    //     bound. This is why the remove below is conditional on the PID.
    //   dual-stack [::] without IPV6_V6ONLY (udp 1244) - indicated on BOTH the v4
    //     and the v6 assignment layers, so each family gets its own entry and
    //     inbound IPv4 traffic to such a listener resolves correctly.
    //
    // IpProtocol and IpLocalPort were FwpUint8/FwpUint16 in every release
    // observed, never FWP_EMPTY, so the union reads above are sound at this layer.
    if matches!(
        data.layer,
        layer::Layer::AleResourceReleaseV4 | layer::Layer::AleResourceReleaseV6
    ) {
        type Fields = layer::FieldsAleResourceReleaseV4; // Same field order for V6.
        let ipv6 = matches!(data.layer, layer::Layer::AleResourceReleaseV6);
        let protocol = get_protocol(&data, Fields::IpProtocol as usize);
        let port = data.get_value_u16(Fields::IpLocalPort as usize);

        // Mirror the guards used when inserting. Without the protocol test a
        // release for a raw or other non-TCP/UDP socket would be folded into the
        // TCP plane by slot_index and could zero an unrelated live TCP entry;
        // port 0 is never a key on the insert side either.
        //
        // The PID is passed so the entry is only dropped for the owner it was
        // recorded for: with SO_REUSEADDR two processes share one endpoint, and an
        // unconditional remove let the first one to exit clear the survivor's
        // entry. See EndpointPidCache::remove.
        if matches!(protocol, IpProtocol::Udp | IpProtocol::Tcp) && port != 0 {
            if let Some(process_id) = data.get_process_id() {
                device
                    .endpoint_pid_cache
                    .remove(ipv6, protocol, port, process_id);
            }
        }
    }

    match data.layer {
        layer::Layer::AleResourceAssignmentV4Discard => {
            type Fields = layer::FieldsAleResourceAssignmentV4;
            if let Some(conns) = device.connection_cache.end_all_on_endpoint_v4(
                (
                    get_protocol(&data, Fields::IpProtocol as usize),
                    data.get_value_u16(Fields::IpLocalPort as usize),
                ),
                get_ipv4_address_if_present(&data, Fields::IpLocalAddress as usize),
                data.get_process_id().filter(|pid| *pid != 0),
            ) {
                let process_id = data.get_process_id().unwrap_or(0);
                info!(
                    "Port {}/{} Ipv4 assign request discarded pid={}",
                    data.get_value_u16(Fields::IpLocalPort as usize),
                    get_protocol(&data, Fields::IpProtocol as usize),
                    process_id,
                );
                for conn in conns {
                    let info = protocol::info::connection_end_event_v4_info(
                        process_id,
                        conn.get_direction() as u8,
                        data.get_value_u8(Fields::IpProtocol as usize),
                        conn.local_address.0,
                        conn.remote_address.0,
                        conn.local_port,
                        conn.remote_port,
                    );
                    let _ = device.event_queue.push(info);
                }
            }
        }
        layer::Layer::AleResourceAssignmentV6Discard => {
            type Fields = layer::FieldsAleResourceAssignmentV6;
            if let Some(conns) = device.connection_cache.end_all_on_endpoint_v6(
                (
                    get_protocol(&data, Fields::IpProtocol as usize),
                    data.get_value_u16(Fields::IpLocalPort as usize),
                ),
                get_ipv6_address_if_present(&data, Fields::IpLocalAddress as usize),
                data.get_process_id().filter(|pid| *pid != 0),
            ) {
                let process_id = data.get_process_id().unwrap_or(0);
                info!(
                    "Port {}/{} Ipv6 assign request discarded pid={}",
                    data.get_value_u16(Fields::IpLocalPort as usize),
                    get_protocol(&data, Fields::IpProtocol as usize),
                    process_id,
                );
                for conn in conns {
                    let info = protocol::info::connection_end_event_v6_info(
                        process_id,
                        conn.get_direction() as u8,
                        data.get_value_u8(Fields::IpProtocol as usize),
                        conn.local_address.0,
                        conn.remote_address.0,
                        conn.local_port,
                        conn.remote_port,
                    );
                    let _ = device.event_queue.push(info);
                }
            }
        }
        layer::Layer::AleResourceReleaseV4 => {
            type Fields = layer::FieldsAleResourceReleaseV4;
            if let Some(conns) = device.connection_cache.end_all_on_endpoint_v4(
                (
                    get_protocol(&data, Fields::IpProtocol as usize),
                    data.get_value_u16(Fields::IpLocalPort as usize),
                ),
                get_ipv4_address_if_present(&data, Fields::IpLocalAddress as usize),
                data.get_process_id().filter(|pid| *pid != 0),
            ) {
                let process_id = data.get_process_id().unwrap_or(0);
                info!(
                    "Port {}/{} released pid={}",
                    data.get_value_u16(Fields::IpLocalPort as usize),
                    get_protocol(&data, Fields::IpProtocol as usize),
                    process_id,
                );
                for conn in conns {
                    let info = protocol::info::connection_end_event_v4_info(
                        process_id,
                        conn.get_direction() as u8,
                        data.get_value_u8(Fields::IpProtocol as usize),
                        conn.local_address.0,
                        conn.remote_address.0,
                        conn.local_port,
                        conn.remote_port,
                    );
                    let _ = device.event_queue.push(info);
                }
            }
        }
        layer::Layer::AleResourceReleaseV6 => {
            type Fields = layer::FieldsAleResourceReleaseV6;
            if let Some(conns) = device.connection_cache.end_all_on_endpoint_v6(
                (
                    get_protocol(&data, Fields::IpProtocol as usize),
                    data.get_value_u16(Fields::IpLocalPort as usize),
                ),
                get_ipv6_address_if_present(&data, Fields::IpLocalAddress as usize),
                data.get_process_id().filter(|pid| *pid != 0),
            ) {
                let process_id = data.get_process_id().unwrap_or(0);
                info!(
                    "Port {}/{} released pid={}",
                    data.get_value_u16(Fields::IpLocalPort as usize),
                    get_protocol(&data, Fields::IpProtocol as usize),
                    process_id,
                );
                for conn in conns {
                    let info = protocol::info::connection_end_event_v6_info(
                        process_id,
                        conn.get_direction() as u8,
                        data.get_value_u8(Fields::IpProtocol as usize),
                        conn.local_address.0,
                        conn.remote_address.0,
                        conn.local_port,
                        conn.remote_port,
                    );
                    let _ = device.event_queue.push(info);
                }
            }
        }
        _ => {}
    }
}
