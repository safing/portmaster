use crate::ale_policy::{
    can_reuse_ended_tcp_policy, classify_ale_injection, self_injected_endpoint_identifies_socket,
    should_capture_ale_packet, should_skip_cross_direction_ale_clone,
    should_skip_injected_outbound_flow, AleInjectionAction, InjectionStatus,
};
use crate::connection::{Connection, ConnectionV4, ConnectionV6, Direction, Verdict};
use crate::connection_map::Key;
use crate::device::{Device, Packet};
use crate::udp_flow_cache::UdpFlowRegistration;
use alloc::{boxed::Box, string::String, vec::Vec};

use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv4Address, Ipv6Address, IPV4_HEADER_LEN, IPV6_HEADER_LEN,
};
use wdk::consts::{FWP_CONDITION_FLAG_IS_LOOPBACK, FWP_CONDITION_FLAG_IS_REAUTHORIZE};
use wdk::filter_engine::layer::{
    self, FieldsAleAuthConnectV4, FieldsAleAuthConnectV6, FieldsAleAuthRecvAcceptV4,
    FieldsAleAuthRecvAcceptV6, ValueType,
};
use wdk::filter_engine::net_buffer::NetBufferList;
use wdk::filter_engine::packet::{PacketInjectionOrigin, TransportPacketList, TransportProtocol};
use wdk::filter_engine::{callout_data::CalloutData, PacketDirection as WfpPacketDirection};
use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::FWP_DIRECTION_OUTBOUND;

// ALE Layers

#[derive(Debug)]
#[allow(dead_code)]
struct AleLayerData {
    is_ipv6: bool,
    reauthorize: bool,
    loopback: bool,
    process_id: u64,
    protocol: IpProtocol,
    /// Direction assigned to the ALE flow, derived from its authorization layer.
    connection_direction: Direction,
    /// Direction of the packet currently being classified. During reauthorization
    /// this can be opposite to `connection_direction`.
    packet_direction: Direction,
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

fn get_protocol_if_present(data: &CalloutData, index: usize) -> Option<IpProtocol> {
    matches!(data.get_value_type(index), ValueType::FwpUint8).then(|| get_protocol(data, index))
}

fn get_u16_if_present(data: &CalloutData, index: usize) -> Option<u16> {
    matches!(data.get_value_type(index), ValueType::FwpUint16).then(|| data.get_value_u16(index))
}

/// Reads a `FWP_UINT32` field, returning 0 when the field is not populated.
/// `CalloutData` validates both the field index and its type before reading the
/// native union, so a separate type lookup here would only repeat that work.
fn get_u32_or_zero(data: &CalloutData, index: usize) -> u32 {
    data.get_value_u32(index)
}

/// Reads the shared ALE flags field once for both classifications.
fn get_ale_flags(data: &CalloutData, index: usize) -> (bool, bool) {
    let flags = get_u32_or_zero(data, index);
    (
        flags & FWP_CONDITION_FLAG_IS_REAUTHORIZE != 0,
        flags & FWP_CONDITION_FLAG_IS_LOOPBACK != 0,
    )
}

/// Resolves the current packet direction without changing the direction assigned
/// to the ALE flow. WFP supplies packet-direction metadata only for
/// reauthorization; its absence means the authorization layer direction applies.
fn get_packet_direction(data: &CalloutData, connection_direction: Direction) -> Direction {
    match data.get_packet_direction() {
        Some(WfpPacketDirection::Outbound) => Direction::Outbound,
        Some(WfpPacketDirection::Inbound) => Direction::Inbound,
        None => connection_direction,
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
    let (reauthorize, loopback) = get_ale_flags(&data, Fields::Flags as usize);
    let ale_data = AleLayerData {
        is_ipv6: false,
        reauthorize,
        loopback,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        connection_direction: Direction::Outbound,
        packet_direction: get_packet_direction(&data, Direction::Outbound),
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
    let (reauthorize, loopback) = get_ale_flags(&data, Fields::Flags as usize);

    let ale_data = AleLayerData {
        is_ipv6: true,
        reauthorize,
        loopback,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        connection_direction: Direction::Outbound,
        packet_direction: get_packet_direction(&data, Direction::Outbound),
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

pub fn ale_layer_recv_accept_v4(data: CalloutData) {
    type Fields = FieldsAleAuthRecvAcceptV4;
    let (reauthorize, loopback) = get_ale_flags(&data, Fields::Flags as usize);

    let ale_data = AleLayerData {
        is_ipv6: false,
        reauthorize,
        loopback,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        connection_direction: Direction::Inbound,
        packet_direction: get_packet_direction(&data, Direction::Inbound),
        local_ip: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        interface_index: get_u32_or_zero(&data, Fields::InterfaceIndex as usize),
        sub_interface_index: get_u32_or_zero(&data, Fields::SubInterfaceIndex as usize),
    };

    ale_layer_auth(data, ale_data);
}

pub fn ale_layer_recv_accept_v6(data: CalloutData) {
    type Fields = FieldsAleAuthRecvAcceptV6;
    let (reauthorize, loopback) = get_ale_flags(&data, Fields::Flags as usize);

    let ale_data = AleLayerData {
        is_ipv6: true,
        reauthorize,
        loopback,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        connection_direction: Direction::Inbound,
        packet_direction: get_packet_direction(&data, Direction::Inbound),
        local_ip: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        interface_index: get_u32_or_zero(&data, Fields::InterfaceIndex as usize),
        sub_interface_index: get_u32_or_zero(&data, Fields::SubInterfaceIndex as usize),
    };

    ale_layer_auth(data, ale_data);
}

fn transport_endpoint_handle(data: &CalloutData) -> Option<u64> {
    data.get_transport_endpoint_handle()
        .filter(|endpoint_handle| *endpoint_handle != 0)
}

fn parent_endpoint_handle(data: &CalloutData) -> Option<u64> {
    data.get_parent_endpoint_handle()
        .filter(|endpoint_handle| *endpoint_handle != 0)
}

/// Associates a TCP endpoint only while its exact connection-cache generation
/// is still live. Endpoint closure takes the same lock before ending the stored
/// generation, so a concurrent reauthorization cannot republish stale identity.
fn track_tcp_endpoint_instance(
    device: &Device,
    endpoint_handle: Option<u64>,
    parent_endpoint_handle: Option<u64>,
    key: Key,
    instance_id: u64,
) {
    let Some(endpoint_handle) = endpoint_handle else {
        return;
    };

    let now = wdk::utils::get_monotonic_timestamp_ms();
    let mut endpoint_cache = device.tcp_endpoint_cache.write_lock();
    let associated = device
        .connection_cache
        .with_live_connection_instance(&key, instance_id, |_| {
            Some(endpoint_cache.associate_instance_at(
                endpoint_handle,
                key,
                parent_endpoint_handle,
                instance_id,
                now,
            ))
        });
    if matches!(associated, Some(false)) {
        crate::err!(
            "TCP endpoint handle {} is already associated with another connection: {}",
            endpoint_handle,
            key
        );
    }
}

/// Associates an endpoint only after a concrete live connection-cache instance
/// exists. This avoids unbound peers that no flow callback or periodic cleanup can
/// identify precisely.
fn track_udp_endpoint_instance(
    device: &Device,
    endpoint_handle: Option<u64>,
    key: Key,
    instance_id: u64,
) {
    let Some(endpoint_handle) = endpoint_handle else {
        return;
    };

    let now = wdk::utils::get_monotonic_timestamp_ms();
    // Endpoint closure takes the same outer lock before ending connection state.
    // Revalidate the instance while that lock is held, so closure cannot pass an
    // empty endpoint cache and let a stale association be inserted afterwards.
    let associated = {
        let mut endpoint_cache = device.udp_endpoint_cache.write_lock();
        device
            .connection_cache
            .with_live_connection_instance(&key, instance_id, |_| {
                let _ = endpoint_cache.associate_instance_at(endpoint_handle, key, instance_id, now);
                Some(())
            })
            .is_some()
    };
    if associated {
        let _ = device
            .connection_cache
            .mark_connection_instance_tracked(&key, instance_id);
    }
}

fn track_endpoint_instance(
    device: &Device,
    endpoint_handle: Option<u64>,
    parent_endpoint_handle: Option<u64>,
    key: Key,
    instance_id: u64,
) {
    match key.protocol {
        IpProtocol::Tcp => track_tcp_endpoint_instance(
            device,
            endpoint_handle,
            parent_endpoint_handle,
            key,
            instance_id,
        ),
        IpProtocol::Udp => {
            track_udp_endpoint_instance(device, endpoint_handle, key, instance_id)
        }
        _ => {}
    }
}

fn ale_layer_auth(mut data: CalloutData, ale_data: AleLayerData) {
    // Fail closed until a later path explicitly permits the indication. Do not
    // make this provisional action hard: the callback still needs to replace it.
    data.set_default_block_and_absorb();

    let Some(device) = crate::entry::get_device() else {
        return;
    };

    // Only TCP and UDP are associated with an ALE connection. Avoid the two WFP
    // injection-state queries for protocols that are always handled at packet layer.
    if !matches!(ale_data.protocol, IpProtocol::Tcp | IpProtocol::Udp) {
        // Outbound: will be handled by packet layer next.
        // Inbound: was already handled by packet layer.
        data.action_permit();
        return;
    }

    // Network-layer reinjection is used by the packet path, while packets held at
    // ALE receive/accept are returned with the transport injector. Either kind can
    // be indicated here again and must be permitted without creating another pend.
    // Keep foreign injection distinct: it is handled below at the packet layer
    // without binding the injector's shared raw endpoint to an application flow.
    let layer_data = data.get_layer_data();
    let network_injection_origin = if layer_data.is_null() {
        PacketInjectionOrigin::Unknown
    } else {
        // SAFETY: A non-null ALE layer-data value is a WFP-owned NBL that remains
        // live for this classify callback. The synchronous query does not retain it.
        unsafe {
            device
                .injector
                .network_packet_injection_origin(layer_data as _, ale_data.is_ipv6)
        }
    };
    // A network self-injection is already sufficient to classify the indication as
    // ours. Avoid the second WFP query on the common network-reinjection path; the
    // transport origin cannot change either the self-injection decision or the
    // loopback receive exception.
    let injection_status = if network_injection_origin.is_self_injected() {
        InjectionStatus::new(true, false, false, false)
    } else {
        let transport_injection_origin = if layer_data.is_null() {
            PacketInjectionOrigin::Unknown
        } else {
            // SAFETY: The same WFP-owned NBL remains live for this synchronous query.
            unsafe {
                device
                    .injector
                    .transport_packet_injection_origin(layer_data as _)
            }
        };
        InjectionStatus::new(
            false,
            transport_injection_origin.is_self_injected(),
            network_injection_origin.is_injected_by_other(),
            transport_injection_origin.is_injected_by_other(),
        )
    };
    let injection_action = classify_ale_injection(
        injection_status,
        ale_data.protocol,
        ale_data.loopback,
        ale_data.connection_direction,
        ale_data.packet_direction,
    );
    let key = ale_data.as_key();

    // An outbound self-injected indication has no application send context, so its
    // transport endpoint handle belongs to the injector and is shared by every
    // packet it reinjects - runtime capture showed one handle offered for dozens of
    // unrelated tuples with different remote addresses. Borrowing it would both
    // report a false handle collision for each following connection and leave the
    // shared handle cached as an alias of whichever tuple claimed it first. Inbound
    // self-injected packets are delivered to a concrete receiving endpoint and keep
    // the native identity that closure needs.
    let endpoint_handle = transport_endpoint_handle(&data);
    let parent_endpoint_handle = if matches!(ale_data.protocol, IpProtocol::Tcp) {
        parent_endpoint_handle(&data)
    } else {
        None
    };

    match injection_action {
        AleInjectionAction::PermitSelfInjected => {
            if self_injected_endpoint_identifies_socket(ale_data.packet_direction) {
                if let Some(connection_instance_id) =
                    device.connection_cache.get_connection_instance_id(&key)
                {
                    track_endpoint_instance(
                        device,
                        endpoint_handle,
                        parent_endpoint_handle,
                        key,
                        connection_instance_id,
                    );
                }
            }
            data.action_permit();
            return;
        }
        AleInjectionAction::PermitOtherInjectedOutbound => {
            // A foreign network injector can create a synthetic System-owned ALE
            // flow and expose one shared raw endpoint handle for many unrelated
            // tuples. Let the outbound IP-packet callout apply policy instead.
            data.action_permit();
            return;
        }
        AleInjectionAction::Process => {}
    }

    // Keep the WFP endpoint identity now, but associate it only after this
    // authorization has selected a concrete live connection-cache generation.
    // Reauthorization of an already cached TCP connection may omit this metadata;
    // in that case the association saved by the initial indication remains valid.
    // The handles were read above so the self-injection fast path records them too.
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
    // permit and let the packet layer classify. During ordinary authorization,
    // inbound UDP does not take this branch. During cross-direction
    // reauthorization, the current packet direction deliberately controls it.
    if matches!(ale_data.protocol, IpProtocol::Udp)
        && matches!(ale_data.packet_direction, Direction::Outbound)
    {
        // Start as untracked. Endpoint association below promotes the exact live
        // instance only after it has installed the identity needed by closure. If
        // WFP omitted that metadata, the cached verdict remains usable but periodic
        // cleanup gives the otherwise unbounded fallback state an idle lifetime.
        match device.connection_cache.register_untracked_connection(
            &key,
            ale_data.process_id,
            ale_data.connection_direction,
        ) {
            Ok(registration) => {
                if registration.inserted {
                    crate::dbg!(
                        "ale layer registered udp connection for packet layer: {} PID: {}",
                        key,
                        ale_data.process_id
                    );
                }
                track_endpoint_instance(
                    device,
                    endpoint_handle,
                    parent_endpoint_handle,
                    key,
                    registration.instance_id,
                );
            }
            Err(err) => crate::err!("failed to build connection: {}", err),
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
    let cached = if ale_data.is_ipv6 {
        device
            .connection_cache
            .read_connection_v6(&key, |conn| -> Option<(Verdict, u64, bool)> {
                // Function is behind spin lock, just copy and return.
                Some((conn.verdict, conn.get_instance_id(), true))
            })
    } else {
        device
            .connection_cache
            .read_connection_v4(&key, |conn| -> Option<(Verdict, u64, bool)> {
                // Function is behind spin lock, just copy and return.
                Some((conn.verdict, conn.get_instance_id(), true))
            })
    };
    // Completing or reinjecting a pended operation can produce one final TCP
    // reauthorization after endpoint closure has ended its cache generation. Its
    // optional endpoint metadata may already be incomplete. Treat the retained exact
    // generation as policy history instead of registering a new live instance that
    // can receive neither FLOW_ESTABLISHED nor another CLOSURE.
    let cached = cached.or_else(|| {
        if !can_reuse_ended_tcp_policy(ale_data.reauthorize, ale_data.protocol) {
            return None;
        }

        if ale_data.is_ipv6 {
            device.connection_cache.read_ended_connection_v6(
                &key,
                |conn| -> Option<(Verdict, u64, bool)> {
                    Some((conn.verdict, conn.get_instance_id(), false))
                },
            )
        } else {
            device.connection_cache.read_ended_connection_v4(
                &key,
                |conn| -> Option<(Verdict, u64, bool)> {
                    Some((conn.verdict, conn.get_instance_id(), false))
                },
            )
        }
    });

    // Connection already in cache.
    if let Some((verdict, connection_instance_id, live)) = cached {
        if live {
            track_endpoint_instance(
                device,
                endpoint_handle,
                parent_endpoint_handle,
                key,
                connection_instance_id,
            );
        } else {
            crate::dbg!(
                "processing TCP reauthorization after endpoint closure: {}",
                key
            );
        }
        crate::dbg!("processing existing connection: {} {}", key, verdict);
        match verdict {
            // No verdict yet
            Verdict::Undecided if !live => {
                // Endpoint closure cannot leave a queued request behind: it either
                // retired the request or waited for its verdict before ending the
                // generation. A retained Undecided value is therefore teardown
                // residue, not a new policy decision.
                data.action_permit();
            }
            Verdict::Undecided => {
                crate::dbg!("saving packet: {}", key);
                // Connection is already pended. Save packet and wait for verdict.
                match save_packet(device, &mut data, &ale_data, false) {
                    Ok(packet) => {
                        if let Some(pending) = device.publish_pending_packet(
                            (key, packet),
                            Some(connection_instance_id),
                            ale_data.process_id,
                            ale_data.packet_direction,
                            true,
                        ) {
                            crate::dbg!(
                                "discarding packet queued after connection instance {} ended: {}",
                                connection_instance_id,
                                key
                            );
                            if let Err(err) = device.inject_packet(pending.packet, true) {
                                crate::err!("failed to complete stale ALE operation: {}", err);
                            }
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
                // Authorize at ALE. Outbound traffic continues to the packet
                // layer; inbound traffic has already passed it.
                data.action_permit();

                if device.is_owner_pid(ale_data.process_id as u32)
                    && matches!(ale_data.packet_direction, Direction::Outbound)
                {
                    // If this is Portmaster's own outbound packet, clear the write flag
                    // to prevent subsequent filters in the chain from overriding the permit action.
                    // This prevents other firewall applications from blocking Portmaster's own traffic.
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
                if let Direction::Outbound = ale_data.packet_direction {
                    // Handled by packet layer.
                    data.action_permit();
                } else {
                    // Inbound authorization is enforced here.
                    data.action_block_hard();
                }
            }
            Verdict::Drop => {
                if let Direction::Outbound = ale_data.packet_direction {
                    // Handled by packet layer.
                    data.action_permit();
                } else {
                    // Inbound authorization is enforced here.
                    data.block_and_absorb();
                }
            }
        }
    } else {
        // Creating TCP state without native endpoint identity would force closure
        // back to a tuple lookup and reintroduce stale-generation teardown. An
        // already cached connection was handled above and can safely reuse the
        // association saved by its initial authorization.
        if matches!(ale_data.protocol, IpProtocol::Tcp) && endpoint_handle.is_none() {
            crate::err!(
                "cannot register TCP authorization without an endpoint handle: {}",
                key
            );
            return;
        }

        crate::dbg!("pending connection: {} {}", key, ale_data.packet_direction);
        // Only first packet of a connection can be pended: reauthorize == false
        //
        // Outbound UDP never reaches this point - it returns above and is decided
        // at the packet layer, because holding a datagram at this layer breaks the
        // send status reported to the application. Inbound UDP still arrives here
        // and is safe to pend: there is no application send operation to freeze.
        let can_pend_connection = !ale_data.reauthorize;
        let packet = match save_packet(device, &mut data, &ale_data, can_pend_connection) {
            Ok(packet) => packet,
            Err(err) => {
                crate::err!("failed to pend packet: {}", err);
                return;
            }
        };

        // Register before publishing the request. The cache performs the live
        // lookup and insertion under one write guard, so a concurrent classify
        // callback cannot create another live entry for this tuple. UDP starts
        // untracked and is promoted only after endpoint association succeeds.
        let registration_result = if matches!(ale_data.protocol, IpProtocol::Udp) {
            device.connection_cache.register_untracked_connection(
                &key,
                ale_data.process_id,
                ale_data.connection_direction,
            )
        } else {
            device.connection_cache.register_connection(
                &key,
                ale_data.process_id,
                ale_data.connection_direction,
            )
        };
        let registration = match registration_result {
            Ok(registration) => {
                if registration.inserted {
                    crate::dbg!(
                        "ale layer added connection: {} PID: {}",
                        key,
                        ale_data.process_id
                    );
                } else {
                    crate::dbg!("connection registered concurrently: {}", key);
                }
                registration
            }
            Err(err) => {
                crate::err!("failed to build connection: {}", err);
                if let Err(complete_err) = device.inject_packet(packet, true) {
                    crate::err!("failed to complete ALE operation: {}", complete_err);
                }
                return;
            }
        };

        track_endpoint_instance(
            device,
            endpoint_handle,
            parent_endpoint_handle,
            key,
            registration.instance_id,
        );
        if let Some(pending) = device.publish_pending_packet(
            (key, packet),
            Some(registration.instance_id),
            ale_data.process_id,
            ale_data.packet_direction,
            true,
        ) {
            crate::dbg!(
                "discarding packet queued after connection instance {} ended: {}",
                registration.instance_id,
                key
            );
            if let Err(err) = device.inject_packet(pending.packet, true) {
                crate::err!("failed to complete stale ALE operation: {}", err);
            }
        }

        // Absorb this indication. A reusable packet clone, when WFP supplied one,
        // will be re-injected after Portmaster returns a verdict.
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
    // Initial outbound TCP authorization has no packet data. A later
    // reauthorization may carry a transport-header NBL, so preserve it when WFP
    // makes one available.
    let save_packet_list = should_capture_ale_packet(
        ale_data.protocol,
        ale_data.packet_direction,
        ale_data.reauthorize,
    );
    if save_packet_list {
        packet_list = create_packet_list(device, callout_data, ale_data)?;
    }
    if pend && matches!(ale_data.packet_direction, Direction::Inbound) && packet_list.is_none() {
        return Err("ALE receive/accept indication has no packet data".into());
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
) -> Result<Option<TransportPacketList>, String> {
    // An inbound packet can reauthorize an outbound ALE flow at AUTH_CONNECT.
    // WFP does not expose a valid IP header for packet inspection at that layer,
    // so it cannot be converted into the IP-header NBL required by transport
    // receive injection. Absorb this indication without a clone; after the verdict
    // forces another reauthorization, the next packet is classified normally.
    if should_skip_cross_direction_ale_clone(
        ale_data.reauthorize,
        ale_data.connection_direction,
        ale_data.packet_direction,
    ) {
        return Ok(None);
    }

    let layer_data = callout_data.get_layer_data();
    if layer_data.is_null() {
        return Ok(None);
    }

    // SAFETY: This function runs synchronously inside an ALE classify callback.
    // WFP owns the non-null layer-data NBL and keeps it live through callback
    // return; this borrowed wrapper is dropped after cloning and never escapes.
    let mut nbl = unsafe { NetBufferList::new(layer_data as _) };
    let mut inbound = false;
    let mut event_data_offset = 0;
    if let Direction::Inbound = ale_data.packet_direction {
        // At ALE_AUTH_RECV_ACCEPT the inbound data offset is at the payload.
        // Retreat over both headers so the clone passed to
        // FwpsInjectTransportReceiveAsync starts at the IP header.
        let base = if ale_data.is_ipv6 {
            IPV6_HEADER_LEN
        } else {
            IPV4_HEADER_LEN
        } as u32;
        let ip_header_size = callout_data
            .get_ip_header_size()
            .ok_or_else(|| String::from("missing ALE IP header size metadata"))?;
        if ip_header_size < base {
            return Err(alloc::format!(
                "invalid ALE IP header size: {}",
                ip_header_size
            ));
        }

        let transport_header_size = callout_data
            .get_transport_header_size()
            .ok_or_else(|| String::from("missing ALE transport header size metadata"))?;
        let minimum_transport_header_size = match ale_data.protocol {
            IpProtocol::Tcp => 20,
            IpProtocol::Udp => 8,
            _ => 0,
        };
        if transport_header_size < minimum_transport_header_size {
            return Err(alloc::format!(
                "invalid ALE transport header size: {}",
                transport_header_size
            ));
        }

        let header_size = ip_header_size
            .checked_add(transport_header_size)
            .ok_or_else(|| String::from("ALE header size overflow"))?;
        nbl.retreat(header_size, true)
            .map_err(|err| alloc::format!("failed to retreat ALE packet: {}", err))?;
        event_data_offset = ip_header_size as usize;
        inbound = true;
    }

    let address: &[u8] = match &ale_data.remote_ip {
        IpAddress::Ipv4(address) => &address.0,
        IpAddress::Ipv6(address) => &address.0,
    };
    let clone = nbl
        .clone(&device.network_allocator)
        .map_err(|err| alloc::format!("failed to clone ALE packet: {}", err))?;
    let transport_protocol = match ale_data.protocol {
        IpProtocol::Tcp => TransportProtocol::Tcp,
        IpProtocol::Udp => TransportProtocol::Udp,
        _ => return Err(String::from("unsupported ALE transport protocol")),
    };

    Ok(Some(device.injector.from_ale_callout(
        transport_protocol,
        ale_data.is_ipv6,
        callout_data,
        clone,
        event_data_offset,
        address,
        inbound,
        ale_data.loopback,
        ale_data.interface_index,
        ale_data.sub_interface_index,
    )?))
}

fn retire_pending_connections<T: Connection>(device: &Device, connections: &[T]) {
    match connections {
        [] => {}
        [connection] => {
            device.retire_pending_connection_instances(&[connection.get_instance_id()]);
        }
        _ => {
            let instance_ids: Vec<u64> = connections
                .iter()
                .map(Connection::get_instance_id)
                .collect();
            device.retire_pending_connection_instances_owned(instance_ids);
        }
    }
}

pub(crate) fn emit_connection_end_v4(device: &Device, conn: ConnectionV4, process_id: u64) {
    let info = protocol::info::connection_end_event_v4_info(
        if conn.process_id == 0 {
            process_id
        } else {
            conn.process_id
        },
        conn.get_direction() as u8,
        u8::from(conn.protocol),
        conn.local_address.0,
        conn.remote_address.0,
        conn.local_port,
        conn.remote_port,
    );
    let _ = device.event_queue.push(info);
}

pub(crate) fn emit_connection_end_v6(device: &Device, conn: ConnectionV6, process_id: u64) {
    let info = protocol::info::connection_end_event_v6_info(
        if conn.process_id == 0 {
            process_id
        } else {
            conn.process_id
        },
        conn.get_direction() as u8,
        u8::from(conn.protocol),
        conn.local_address.0,
        conn.remote_address.0,
        conn.local_port,
        conn.remote_port,
    );
    let _ = device.event_queue.push(info);
}

struct UdpFlowContext {
    key: Key,
    process_id: u64,
    connection_instance_id: u64,
    endpoint_handle: Option<u64>,
}

pub(crate) fn reclaim_udp_flow_context(
    device: &Device,
    flow_context: u64,
    connection_instance_id: u64,
) {
    if device
        .udp_flow_cache
        .cancel_registration(flow_context, connection_instance_id)
    {
        unsafe {
            drop(Box::from_raw(flow_context as *mut UdpFlowContext));
        }
    }
}

/// Associates the cached UDP tuple with WFP's native ALE-flow lifetime.
///
/// Endpoint lifetime tracking is established independently at authorization and
/// remains valid even if WFP rejects this flow-context association.
///
/// The registration table owns the identifiers needed to retire stale contexts
/// during periodic cleanup and to remove every remaining context during unload.
/// WFP owns `flow_context` after association succeeds and returns it exactly once
/// through `udp_flow_delete` when the peer flow expires or its socket closes.
fn associate_udp_flow_context(
    device: &Device,
    data: &CalloutData,
    key: Key,
    process_id: u64,
    endpoint_handle: Option<u64>,
    connection_instance_id: u64,
) {
    if data.has_flow_context() {
        return;
    }
    let Some(flow_id) = data.get_flow_handle() else {
        crate::err!("UDP flow has no WFP flow handle: {}", key);
        return;
    };

    if connection_instance_id == 0 {
        return;
    }
    let registration = UdpFlowRegistration::new(
        flow_id,
        data.get_layer_id(),
        data.get_callout_id(),
        connection_instance_id,
    );
    let flow_context = Box::into_raw(Box::new(UdpFlowContext {
        key,
        process_id,
        connection_instance_id,
        endpoint_handle,
    })) as u64;

    if !device.udp_flow_cache.register(flow_context, registration) {
        unsafe {
            drop(Box::from_raw(flow_context as *mut UdpFlowContext));
        }
        return;
    }

    if let Err(err) = data.associate_flow_context(flow_context) {
        // Normally a failed FwpsFlowAssociateContext call never transfers
        // ownership to WFP. Keep the conditional as a guard for the termination
        // race where flowDeleteFn has already claimed the registration.
        reclaim_udp_flow_context(device, flow_context, connection_instance_id);
        crate::err!("failed to associate UDP flow {}: {}", key, err);
        return;
    }

    // From this point WFP owns the context. flowDeleteFn may already have claimed
    // it while FwpsFlowAssociateContext0 was returning.
    if device
        .udp_flow_cache
        .mark_associated(flow_context, connection_instance_id)
    {
        // This WFP-owned context now provides an exact flowDeleteFn lifetime even
        // when authorization omitted the transport endpoint handle. Promote only
        // the same live cache generation; a reused tuple cannot inherit it.
        let _ = device
            .connection_cache
            .mark_connection_instance_tracked(&key, connection_instance_id);
    }
}

/// WFP invokes this after a UDP ALE flow reaches its native idle timeout or its
/// socket closes.  The context is driver-owned again on entry; the normal path
/// claims and frees it while the callback barrier keeps Device alive.
pub(crate) unsafe extern "system" fn udp_flow_delete(
    layer_id: u16,
    callout_id: u32,
    flow_context: u64,
) {
    // Acquire the flow-delete half of the common callback barrier before
    // dereferencing either Device or the opaque flow context.  During unload
    // classify admission is closed first, but this half remains open while
    // prepare_unload removes WFP contexts and receives their callbacks.
    let Some(callback_admission) = wdk::callback_barrier::CALLBACK_BARRIER.enter_flow_delete()
    else {
        return;
    };
    if !callback_admission.is_active() {
        // PREPARED and final-drain callbacks are counted for code lifetime but
        // cannot safely resolve Device or interpret an opaque context.
        return;
    }

    if flow_context == 0 {
        return;
    }

    // The flow cache is part of Device.  Resolve Device while the barrier
    // reference is held, then validate/claim the context through that cache.
    // This intentionally avoids reading a flow-cache pointer out of an
    // untrusted raw context before the registry has accepted the callback.
    let Some(device) = crate::entry::get_device() else {
        // Flow contexts are created only after Device has been published.  A
        // missing pointer is therefore an abnormal teardown/init race; leaking
        // the opaque allocation is safer than touching memory whose owner may
        // already have been destroyed.
        return;
    };

    let Some(reclaim_only) =
        device
            .udp_flow_cache
            .begin_callback(flow_context, layer_id, callout_id)
    else {
        // A duplicate, mismatched, or already-reclaimed callback has no
        // ownership left to release. In particular, never reconstruct a Box for
        // an unknown ID or for a reused address belonging to another callout.
        return;
    };
    let context = unsafe { Box::from_raw(flow_context as *mut UdpFlowContext) };

    // Periodic cleanup and unload have already retired (or are discarding) the
    // corresponding cache state.  Their callbacks only reclaim the WFP
    // allocation; this also avoids re-entering Device while
    // FwpsFlowRemoveContext0 completes synchronously inside the cleanup path.
    if reclaim_only {
        device.udp_flow_cache.finish_callback();
        return;
    }

    if let Some(endpoint_handle) = context.endpoint_handle {
        let mut endpoint_cache = device.udp_endpoint_cache.write_lock();
        let _ =
            endpoint_cache.dissociate(endpoint_handle, context.key, context.connection_instance_id);
    }

    let key = context.key;
    if key.is_ipv6() {
        if let Some(conn) = device
            .connection_cache
            .end_connection_instance_v6(key, context.connection_instance_id)
        {
            retire_pending_connections(device, core::slice::from_ref(&conn));
            emit_connection_end_v6(device, conn, context.process_id);
        }
    } else if let Some(conn) = device
        .connection_cache
        .end_connection_instance_v4(key, context.connection_instance_id)
    {
        retire_pending_connections(device, core::slice::from_ref(&conn));
        emit_connection_end_v4(device, conn, context.process_id);
    }

    device.udp_flow_cache.finish_callback();
}

pub(crate) fn end_tcp_connection(
    device: &Device,
    endpoint: crate::tcp_endpoint_cache::TcpEndpointConnection,
    process_id: u64,
) {
    let key = endpoint.key;
    if key.is_ipv6() {
        if let Some(conn) = device
            .connection_cache
            .end_connection_instance_v6(key, endpoint.instance_id)
        {
            retire_pending_connections(device, core::slice::from_ref(&conn));
            emit_connection_end_v6(device, conn, process_id);
        }
        return;
    }

    if let Some(conn) = device
        .connection_cache
        .end_connection_instance_v4(key, endpoint.instance_id)
    {
        retire_pending_connections(device, core::slice::from_ref(&conn));
        emit_connection_end_v4(device, conn, process_id);
    }
}

fn close_tcp_endpoint(
    device: &Device,
    data: &mut CalloutData,
    endpoint_handle: u64,
    process_id: u64,
) -> bool {
    // Consume native endpoint identity immediately so duplicate or delayed closure
    // indications cannot target a tuple replacement. The connection generation
    // remains live while WFP holds a pended endpoint-closure classification.
    let endpoint = {
        let mut endpoint_cache = device.tcp_endpoint_cache.write_lock();
        endpoint_cache.take(endpoint_handle)
    };
    let Some(endpoint) = endpoint else {
        return false;
    };

    match device.defer_tcp_endpoint_closure(endpoint, process_id, data) {
        Ok(()) => {}
        Err(err) => {
            crate::err!("failed to pend TCP endpoint closure: {}", err);
            end_tcp_connection(device, endpoint, process_id);
        }
    }
    true
}

fn end_udp_endpoint(device: &Device, endpoint_handle: u64, process_id: u64) -> bool {
    let endpoint = {
        let mut endpoint_cache = device.udp_endpoint_cache.write_lock();
        endpoint_cache.take(endpoint_handle)
    };
    let Some(endpoint) = endpoint else {
        return false;
    };

    // Keep the common one-peer socket closure allocation-free. The batch path
    // below is for sockets that accumulated several remote tuples.
    if let [peer] = endpoint.as_slice() {
        if peer.key.is_ipv6() {
            if let Some(conn) = device
                .connection_cache
                .end_connection_instance_v6(peer.key, peer.instance_id)
            {
                retire_pending_connections(device, core::slice::from_ref(&conn));
                emit_connection_end_v6(device, conn, process_id);
            }
        } else if let Some(conn) = device
            .connection_cache
            .end_connection_instance_v4(peer.key, peer.instance_id)
        {
            retire_pending_connections(device, core::slice::from_ref(&conn));
            emit_connection_end_v4(device, conn, process_id);
        }
        return true;
    }

    let mut peers_v4 = Vec::new();
    let mut peers_v6 = Vec::new();
    for peer in endpoint {
        if peer.key.is_ipv6() {
            peers_v6.push((peer.key, peer.instance_id));
        } else {
            peers_v4.push((peer.key, peer.instance_id));
        }
    }

    // A UDP socket can own many remote peers. End them in family batches so the
    // connection cache takes its write lock once per family rather than once per
    // peer.
    let ended_v4 = if peers_v4.is_empty() {
        Vec::new()
    } else {
        device
            .connection_cache
            .end_connection_instances_v4(&peers_v4)
    };
    let ended_v6 = if peers_v6.is_empty() {
        Vec::new()
    } else {
        device
            .connection_cache
            .end_connection_instances_v6(&peers_v6)
    };

    retire_pending_connections(device, &ended_v4);
    retire_pending_connections(device, &ended_v6);
    for conn in ended_v4 {
        emit_connection_end_v4(device, conn, process_id);
    }
    for conn in ended_v6 {
        emit_connection_end_v6(device, conn, process_id);
    }
    true
}

fn end_local_endpoint_v4(
    device: &Device,
    protocol: IpProtocol,
    local_port: u16,
    local_address: Option<IpAddress>,
    process_id: u64,
) {
    if let Some(conns) = device.connection_cache.end_all_on_endpoint_v4(
        (protocol, local_port),
        local_address,
        (process_id != 0).then_some(process_id),
    ) {
        retire_pending_connections(device, &conns);
        for conn in conns {
            emit_connection_end_v4(device, conn, process_id);
        }
    }
}

fn end_local_endpoint_v6(
    device: &Device,
    protocol: IpProtocol,
    local_port: u16,
    local_address: Option<IpAddress>,
    process_id: u64,
) {
    if let Some(conns) = device.connection_cache.end_all_on_endpoint_v6(
        (protocol, local_port),
        local_address,
        (process_id != 0).then_some(process_id),
    ) {
        retire_pending_connections(device, &conns);
        for conn in conns {
            emit_connection_end_v6(device, conn, process_id);
        }
    }
}

fn end_untracked_local_endpoint_v4(
    device: &Device,
    local_port: u16,
    local_address: Option<IpAddress>,
    process_id: u64,
) {
    if let Some(conns) = device.connection_cache.end_untracked_on_endpoint_v4(
        (IpProtocol::Udp, local_port),
        local_address,
        (process_id != 0).then_some(process_id),
    ) {
        retire_pending_connections(device, &conns);
        for conn in conns {
            emit_connection_end_v4(device, conn, process_id);
        }
    }
}

fn end_untracked_local_endpoint_v6(
    device: &Device,
    local_port: u16,
    local_address: Option<IpAddress>,
    process_id: u64,
) {
    if let Some(conns) = device.connection_cache.end_untracked_on_endpoint_v6(
        (IpProtocol::Udp, local_port),
        local_address,
        (process_id != 0).then_some(process_id),
    ) {
        retire_pending_connections(device, &conns);
        for conn in conns {
            emit_connection_end_v6(device, conn, process_id);
        }
    }
}

pub(crate) fn expire_inactive_untracked_connections(device: &Device) {
    let (ended_v4, ended_v6) = device.connection_cache.end_inactive_untracked_connections();
    retire_pending_connections(device, &ended_v4);
    retire_pending_connections(device, &ended_v6);
    for conn in ended_v4 {
        emit_connection_end_v4(device, conn, 0);
    }
    for conn in ended_v6 {
        emit_connection_end_v6(device, conn, 0);
    }
}

const TCP_UNESTABLISHED_TIMEOUT_MS: u64 = 10_000;

/// Ends local TCP authorization generations that never reached FLOW_ESTABLISHED.
///
/// A process can terminate while a local self-address connect is still in SYN-SENT.
/// Windows may then omit the endpoint-closure indication for that provisional
/// endpoint. Once its bounded grace period expires, the authorization handle is no
/// longer useful: there is no established flow identity to preserve, and retaining
/// it would make the endpoint cache grow for every abrupt process termination.
/// Established flows are marked by `rebind_established` and are never selected.
pub(crate) fn expire_unestablished_tcp_connections(device: &Device) {
    let now = wdk::utils::get_monotonic_timestamp_ms();
    let cutoff = now.saturating_sub(TCP_UNESTABLISHED_TIMEOUT_MS);
    let expired = {
        let mut endpoint_cache = device.tcp_endpoint_cache.write_lock();
        endpoint_cache.take_unestablished_before(cutoff, |endpoint| {
            endpoint.key.is_loopback_like()
                && !matches!(
                    device
                        .connection_cache
                        .is_outbound_connection_instance(&endpoint.key, endpoint.instance_id),
                    Some(false)
                )
        })
    };

    for endpoint in expired {
        crate::warn!(
            "TCP endpoint timeout t={} i={} {}",
            now,
            endpoint.instance_id,
            endpoint.key
        );
        end_tcp_connection(device, endpoint, 0);
    }
}

pub fn endpoint_closure_v4(mut data: CalloutData) {
    type Fields = layer::FieldsAleEndpointClosureV4;
    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let process_id = data.get_process_id().unwrap_or(0);
    let protocol = get_protocol_if_present(&data, Fields::IpProtocol as usize);
    let endpoint_handle = transport_endpoint_handle(&data);

    match protocol {
        Some(IpProtocol::Tcp) => {
            let connected_endpoint = matches!(
                data.get_value_type(Fields::IpRemoteAddress as usize),
                ValueType::FwpUint32
            ) && matches!(
                data.get_value_type(Fields::IpRemotePort as usize),
                ValueType::FwpUint16
            );
            let Some(endpoint_handle) = endpoint_handle else {
                if connected_endpoint {
                    crate::err!("connected TCP closure has no endpoint handle");
                }
                return;
            };
            if !close_tcp_endpoint(device, &mut data, endpoint_handle, process_id)
                && connected_endpoint
            {
                crate::err!("TCP endpoint closure did not match a tracked connection");
            }
        }
        Some(IpProtocol::Udp) => {
            // UDP closure is socket-level and may omit its remote tuple. A concrete
            // endpoint handle ends exact tracked peers. Packet-layer fallbacks have
            // no handle by definition, so also end only those untracked entries by
            // the closure's local endpoint; tracked replacements remain protected.
            let local_port = get_u16_if_present(&data, Fields::IpLocalPort as usize);
            let local_address = get_ipv4_address_if_present(&data, Fields::IpLocalAddress as usize);
            if let Some(endpoint_handle) = endpoint_handle {
                let _ = end_udp_endpoint(device, endpoint_handle, process_id);
                if let Some(local_port) = local_port {
                    end_untracked_local_endpoint_v4(device, local_port, local_address, process_id);
                }
                return;
            }

            if let Some(local_port) = local_port {
                end_local_endpoint_v4(
                    device,
                    IpProtocol::Udp,
                    local_port,
                    local_address,
                    process_id,
                );
            }
        }
        None => {
            // A generic closure may omit the protocol. Endpoint handles remain
            // globally authoritative, so consult both protocol-specific maps
            // without attempting a tuple or local-port fallback.
            if let Some(endpoint_handle) = endpoint_handle {
                if !close_tcp_endpoint(device, &mut data, endpoint_handle, process_id) {
                    let _ = end_udp_endpoint(device, endpoint_handle, process_id);
                }
            }
        }
        _ => {}
    }
}

pub fn endpoint_closure_v6(mut data: CalloutData) {
    type Fields = layer::FieldsAleEndpointClosureV6;
    let Some(device) = crate::entry::get_device() else {
        return;
    };
    let process_id = data.get_process_id().unwrap_or(0);
    let protocol = get_protocol_if_present(&data, Fields::IpProtocol as usize);
    let endpoint_handle = transport_endpoint_handle(&data);

    match protocol {
        Some(IpProtocol::Tcp) => {
            let connected_endpoint = matches!(
                data.get_value_type(Fields::IpRemoteAddress as usize),
                ValueType::FwpByteArray16Type
            ) && matches!(
                data.get_value_type(Fields::IpRemotePort as usize),
                ValueType::FwpUint16
            );
            let Some(endpoint_handle) = endpoint_handle else {
                if connected_endpoint {
                    crate::err!("connected TCP closure has no endpoint handle");
                }
                return;
            };
            if !close_tcp_endpoint(device, &mut data, endpoint_handle, process_id)
                && connected_endpoint
            {
                crate::err!("TCP endpoint closure did not match a tracked connection");
            }
        }
        Some(IpProtocol::Udp) => {
            let local_port = get_u16_if_present(&data, Fields::IpLocalPort as usize);
            let local_address = get_ipv6_address_if_present(&data, Fields::IpLocalAddress as usize);
            if let Some(endpoint_handle) = endpoint_handle {
                let _ = end_udp_endpoint(device, endpoint_handle, process_id);
                if let Some(local_port) = local_port {
                    end_untracked_local_endpoint_v6(device, local_port, local_address, process_id);
                }
                return;
            }

            if let Some(local_port) = local_port {
                end_local_endpoint_v6(
                    device,
                    IpProtocol::Udp,
                    local_port,
                    local_address,
                    process_id,
                );
            }
        }
        None => {
            if let Some(endpoint_handle) = endpoint_handle {
                if !close_tcp_endpoint(device, &mut data, endpoint_handle, process_id) {
                    let _ = end_udp_endpoint(device, endpoint_handle, process_id);
                }
            }
        }
        _ => {}
    }
}

/// Checks injection state for a parsed TCP/UDP flow-established indication.
/// A missing or mistyped direction is not treated as outbound.
fn is_injected_outbound_transport_flow(
    device: &Device,
    data: &CalloutData,
    direction_index: usize,
    is_ipv6: bool,
) -> bool {
    if !matches!(data.get_value_type(direction_index), ValueType::FwpUint32)
        || data.get_value_u32(direction_index) != FWP_DIRECTION_OUTBOUND as u32
    {
        return false;
    }

    let layer_data = data.get_layer_data();
    if layer_data.is_null() {
        return false;
    }

    // SAFETY: FLOW_ESTABLISHED supplies this NBL for the duration of the classify
    // callback. The synchronous injection-state queries do not retain it.
    let network_origin = unsafe {
        device
            .injector
            .network_packet_injection_origin(layer_data as _, is_ipv6)
    };
    // Either injection handle is sufficient to identify a synthetic outbound
    // flow. Skip the transport query when the network handle already says so.
    if network_origin.is_injected() {
        return true;
    }

    let transport_origin = unsafe {
        device
            .injector
            .transport_packet_injection_origin(layer_data as _)
    };
    should_skip_injected_outbound_flow(true, false, transport_origin.is_injected())
}

/// Refreshes the owning process when a TCP or UDP ALE flow becomes active.
///
/// WFP indicates TCP here after the three-way handshake completes. UDP has no
/// handshake, so its flow is indicated immediately after `ALE_AUTH_CONNECT` or
/// `ALE_AUTH_RECV_ACCEPT` authorizes the first packet for a remote tuple.
///
/// The authorization layers normally cache the flow with its owning PID. This
/// layer provides a second opportunity to repair entries created by the packet
/// fallback with PID 0, or entries whose earlier attribution was less reliable
/// than the concrete application PID supplied for the established flow.
///
/// This does not help flows that were already active before the driver loaded:
/// successful reauthorization does not produce another flow-established
/// indication.
///
/// Registered as an inspection callout.
pub fn ale_flow_established_monitor(data: CalloutData) {
    let Some(device) = crate::entry::get_device() else {
        return;
    };

    let (key, direction_index, is_ipv6) = match data.get_layer() {
        layer::Layer::AleFlowEstablishedV4 => {
            type Fields = layer::FieldsAleFlowEstablishedV4;

            let Some(protocol) = get_protocol_if_present(&data, Fields::IpProtocol as usize) else {
                return;
            };
            if !matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp) {
                return;
            }

            let (Some(local_address), Some(local_port), Some(remote_address), Some(remote_port)) = (
                get_ipv4_address_if_present(&data, Fields::IpLocalAddress as usize),
                get_u16_if_present(&data, Fields::IpLocalPort as usize),
                get_ipv4_address_if_present(&data, Fields::IpRemoteAddress as usize),
                get_u16_if_present(&data, Fields::IpRemotePort as usize),
            ) else {
                return;
            };

            (
                Key {
                    protocol,
                    local_address,
                    local_port,
                    remote_address,
                    remote_port,
                },
                Fields::Direction as usize,
                false,
            )
        }
        layer::Layer::AleFlowEstablishedV6 => {
            type Fields = layer::FieldsAleFlowEstablishedV6;

            let Some(protocol) = get_protocol_if_present(&data, Fields::IpProtocol as usize) else {
                return;
            };
            if !matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp) {
                return;
            }

            let (Some(local_address), Some(local_port), Some(remote_address), Some(remote_port)) = (
                get_ipv6_address_if_present(&data, Fields::IpLocalAddress as usize),
                get_u16_if_present(&data, Fields::IpLocalPort as usize),
                get_ipv6_address_if_present(&data, Fields::IpRemoteAddress as usize),
                get_u16_if_present(&data, Fields::IpRemotePort as usize),
            ) else {
                return;
            };

            (
                Key {
                    protocol,
                    local_address,
                    local_port,
                    remote_address,
                    remote_port,
                },
                Fields::Direction as usize,
                true,
            )
        }
        _ => return,
    };

    // Network- or transport-layer injection can create an additional raw TCP or
    // UDP flow with the copied application tuple and a different, sometimes shared,
    // endpoint. It is not the application's native connection. Skip both our own
    // reinjection and another driver's synthetic flow before endpoint or tuple
    // resolution so neither can produce false correlation errors, overwrite
    // attribution, or own application lifecycle state. Injection state is the
    // discriminator; a raw-socket flag alone would also match legitimate traffic.
    if is_injected_outbound_transport_flow(device, &data, direction_index, is_ipv6) {
        return;
    }

    if matches!(key.protocol, IpProtocol::Udp) {
        let endpoint_handle = transport_endpoint_handle(&data);
        // Authorization normally records the exact cache instance under the
        // endpoint handle. A packet-layer fallback has no such association yet;
        // FLOW_ESTABLISHED is its one safe opportunity to acquire the endpoint and
        // native flow identity. Only an explicitly untracked exact tuple may use
        // that fallback, so a tracked replacement is never claimed by lookup alone.
        let connection_instance_id = if let Some(endpoint_handle) = endpoint_handle {
            let associated = {
                let endpoint_cache = device.udp_endpoint_cache.read_lock();
                endpoint_cache.with_instance_id(endpoint_handle, &key, |instance_id| {
                    device
                        .connection_cache
                        .with_live_connection_instance(&key, instance_id, Some)
                })
            };
            associated.or_else(|| {
                device
                    .connection_cache
                    .get_untracked_connection_instance_id(&key)
            })
        } else {
            device.connection_cache.get_connection_instance_id(&key)
        };
        let Some(connection_instance_id) = connection_instance_id else {
            return;
        };

        track_udp_endpoint_instance(device, endpoint_handle, key, connection_instance_id);

        // Refresh attribution before exposing the context to WFP: a flow can begin
        // terminating as soon as it has been associated. Use the same exact
        // instance selected above rather than a second tuple-only update.
        let process_id = data.get_process_id().unwrap_or(0);
        if process_id != 0 {
            device.connection_cache.update_process_id_instance(
                &key,
                connection_instance_id,
                process_id,
            );
        }
        associate_udp_flow_context(
            device,
            &data,
            key,
            process_id,
            endpoint_handle,
            connection_instance_id,
        );
    } else {
        let Some(established_endpoint_handle) = transport_endpoint_handle(&data) else {
            crate::err!("established TCP flow has no endpoint handle: {}", key);
            return;
        };
        let parent_endpoint_handle = parent_endpoint_handle(&data);

        // AUTH_RECV_ACCEPT can expose a provisional endpoint handle for the
        // accepted connection. Resolve that authorization through its tuple,
        // parent listener and still-live instance, then replace the provisional
        // handle with the child endpoint used by FLOW_ESTABLISHED and CLOSURE.
        let (endpoint, rebind_failed) = {
            let mut endpoint_cache = device.tcp_endpoint_cache.write_lock();
            let endpoint = endpoint_cache.resolve_live_instance(
                &key,
                parent_endpoint_handle,
                |instance_id| {
                    device
                        .connection_cache
                        .with_live_connection_instance(&key, instance_id, Some)
                        .is_some()
                },
            );
            let rebind_failed = endpoint.is_some_and(|endpoint| {
                !endpoint_cache.rebind_established(established_endpoint_handle, endpoint)
            });
            (endpoint, rebind_failed)
        };

        if rebind_failed {
            crate::err!("failed to rebind established TCP endpoint: {}", key);
            return;
        }
        let Some(endpoint) = endpoint else {
            crate::err!("failed to resolve TCP authorization for established flow: {}", key);
            return;
        };

        if let Some(process_id) = data.get_process_id().filter(|pid| *pid != 0) {
            device.connection_cache.update_process_id_instance(
                &endpoint.key,
                endpoint.instance_id,
                process_id,
            );
        }
    }
}
