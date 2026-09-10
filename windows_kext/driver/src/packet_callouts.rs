use alloc::string::String;
use smoltcp::wire::{IPV4_HEADER_LEN, IPV6_HEADER_LEN};
use wdk::filter_engine::callout_data::CalloutData;
use wdk::filter_engine::layer;
use wdk::filter_engine::net_buffer::{NetBufferList, NetBufferListIter};
use wdk::filter_engine::packet::InjectInfo;

use crate::connection::{
    Connection, ConnectionV4, ConnectionV6, Direction, RedirectInfo, Verdict, PM_DNS_PORT,
    PM_SPN_PORT, PM_SPLIT_TUN_PORT,
};
use crate::connection_cache::ConnectionCache;
use crate::connection_map::Key;
use crate::device::{Device, Packet};
use crate::packet_util::{
    get_icmp_echo_from_nbl, get_key_from_nbl_v4, get_key_from_nbl_v6, is_fragment_v4,
    is_fragment_v6, recalc_header_checksums, Redirect,
};

// IP packet layers
pub fn ip_packet_layer_outbound_v4(data: CalloutData) {
    type Fields = layer::FieldsOutboundIppacketV4;
    let interface_index = data.get_value_u32(Fields::InterfaceIndex as usize);
    let sub_interface_index = data.get_value_u32(Fields::SubInterfaceIndex as usize);

    ip_packet_layer(
        data,
        false,
        Direction::Outbound,
        interface_index,
        sub_interface_index,
        Fields::Flags as usize,
    );
}

pub fn ip_packet_layer_inbound_v4(data: CalloutData) {
    type Fields = layer::FieldsInboundIppacketV4;
    let interface_index = data.get_value_u32(Fields::InterfaceIndex as usize);
    let sub_interface_index = data.get_value_u32(Fields::SubInterfaceIndex as usize);
    ip_packet_layer(
        data,
        false,
        Direction::Inbound,
        interface_index,
        sub_interface_index,
        Fields::Flags as usize,
    );
}

pub fn ip_packet_layer_outbound_v6(data: CalloutData) {
    type Fields = layer::FieldsOutboundIppacketV6;
    let interface_index = data.get_value_u32(Fields::InterfaceIndex as usize);
    let sub_interface_index = data.get_value_u32(Fields::SubInterfaceIndex as usize);

    ip_packet_layer(
        data,
        true,
        Direction::Outbound,
        interface_index,
        sub_interface_index,
        Fields::Flags as usize,
    );
}

pub fn ip_packet_layer_inbound_v6(data: CalloutData) {
    type Fields = layer::FieldsInboundIppacketV6;
    let interface_index = data.get_value_u32(Fields::InterfaceIndex as usize);
    let sub_interface_index = data.get_value_u32(Fields::SubInterfaceIndex as usize);

    ip_packet_layer(
        data,
        true,
        Direction::Inbound,
        interface_index,
        sub_interface_index,
        Fields::Flags as usize,
    );
}

/// Largest retreat accepted from WFP metadata.
///
/// `NdisRetreatNetBufferDataStart` can fail, and its return value is discarded in
/// `NetBufferList::retreat`, so an oversized request would silently not happen and
/// leave the buffer positioned wrongly in the other direction. IPv4 headers cap at
/// 60 bytes (IHL is 4 bits of 32-bit words); IPv6 base plus a realistic extension
/// header chain is bounded well below this.
const MAX_IP_HEADER_RETREAT: u32 = 128;

/// Retreats an inbound net buffer to the start of the IP header.
///
/// At the inbound packet layers the buffer starts past the IP header. The amount
/// to move back is *not* a constant: with IPv4 options the header is IHL*4 up to
/// 60 bytes, and for IPv6 the size reported by WFP includes any extension header
/// chain. Retreating a fixed 20 or 40 bytes leaves the buffer pointing inside the
/// header, so everything downstream parses option or extension bytes as an IP
/// header - which produced keys with protocol 0 and address 0.0.0.0.
///
/// `wfp_ip_header_size` is FWPS_METADATA_FIELD_IP_HEADER_SIZE, which is
/// authoritative for both families. It falls back to the fixed base header size
/// when absent, preserving the previous behaviour rather than guessing.
fn retreat_to_ip_header(
    nbl: &mut NetBufferList,
    ipv6: bool,
    wfp_ip_header_size: Option<u32>,
) {
    let base = if ipv6 { IPV6_HEADER_LEN } else { IPV4_HEADER_LEN } as u32;

    // A value below the base header size cannot be right; treat it as missing.
    let size = match wfp_ip_header_size {
        Some(size) if size >= base && size <= MAX_IP_HEADER_RETREAT => size,
        _ => base,
    };

    nbl.retreat(size, true);
}

/// Returns true if the packet described by this indication is an individual IP
/// fragment rather than a whole datagram.
///
/// Reads the fragment fields from the IP header itself. For inbound packets the
/// header sits before the current data pointer, so the buffer is retreated first;
/// the retreat is undone when the local `NetBufferList` goes out of scope.
///
/// For IPv6 the fragment information sits in an extension header after the base
/// header, so the chain is walked rather than reading a fixed field.
fn is_ip_fragment(
    data: &CalloutData,
    ipv6: bool,
    direction: Direction,
    wfp_ip_header_size: Option<u32>,
) -> bool {
    let Some(mut nbl) = NetBufferListIter::new(data.get_layer_data() as _).next() else {
        return false;
    };

    if let Direction::Inbound = direction {
        retreat_to_ip_header(&mut nbl, ipv6, wfp_ip_header_size);
    }

    if ipv6 {
        is_fragment_v6(&nbl)
    } else {
        is_fragment_v4(&nbl)
    }
}

struct ConnectionInfo {
    verdict: Verdict,
    process_id: u64,
    redirect_info: Option<RedirectInfo>,
}

impl ConnectionInfo {
    fn from_connection<T: Connection>(conn: &T) -> Self {
        ConnectionInfo {
            verdict: conn.get_verdict(),
            process_id: conn.get_process_id(),
            redirect_info: conn.redirect_info(),
        }
    }
}

/// Resolves the process that owns the local endpoint of `key`, using the table
/// filled at the bind layer.
///
/// Returns `None` when the port is unknown, which is the case for sockets that
/// were bound before the driver loaded.
///
/// The port to look up is the local one, and which side of the key that is
/// depends on the direction. `get_key_from_nbl_*` builds the key from the packet
/// as it appears on the wire: for an outbound packet the local endpoint is the
/// source, for an inbound packet it is the destination. Both end up in
/// `key.local_*` for that reason, so `local_port` is correct either way - the
/// direction is taken as an argument only to keep that reasoning checkable at
/// the call site rather than implied.
/// `ipv6` selects the address family, which is part of the key: the same port
/// number can be held by two unrelated processes at once, one on each family.
fn lookup_endpoint_pid(
    device: &Device,
    key: &Key,
    ipv6: bool,
    _direction: Direction,
) -> Option<u64> {
    let pid = device
        .endpoint_pid_cache
        .get(ipv6, key.protocol, key.local_port)?;

    // A stored 0 carries no information; report it as unknown so callers do not
    // treat it as a resolved process.
    if pid == 0 {
        return None;
    }

    Some(pid)
}

fn fast_track_pm_packets(key: &Key, _: Direction) -> bool {
    if key.local_port == PM_DNS_PORT || key.local_port == PM_SPN_PORT || key.local_port == PM_SPLIT_TUN_PORT {
        return key.local_address == key.remote_address;
    }

    return false;
}

fn ip_packet_layer(
    mut data: CalloutData,
    ipv6: bool,
    direction: Direction,
    interface_index: u32,
    sub_interface_index: u32,
    flags_index: usize,
) {
    // Make the default path as drop.
    data.block_and_absorb();

	// How far back an inbound buffer has to be moved to reach the IP header.
    // Read once here: it is needed both by the fragment check below and by every
    // retreat in the loop.
    let wfp_ip_header_size = data.get_ip_header_size();

    // A fragmented datagram is indicated twice at this layer: once per individual
    // fragment, and once more as the reassembled whole (verified on Windows 11:
    // the reassembled indication carries FWP_CONDITION_FLAG_IS_REASSEMBLED and the
    // full 3028-byte length, while the fragments carry only 1500).
    //
    // Only the reassembled indication has a usable transport header. Individual
    // fragments other than the first begin directly with payload bytes, so reading
    // ports at the transport offset returns payload data - that is where the bogus
    // `0 -> 0` connection keys came from.
    //
    // Skip the individual fragments and decide on the reassembled packet, which
    // gives Portmaster the correct ports and the true datagram size.
    //
    // Note: the fragment flag is not set on every fragment indication (the first
    // pass reports flags=0x0), so the IP header's own fragment fields are the
    // reliable discriminator. A packet is an individual fragment when it either
    // has a non-zero offset or has the more-fragments bit set; an unfragmented
    // packet has neither, and the reassembled one is explicitly flagged.
    if !data.is_reassembled(flags_index)
        && is_ip_fragment(&data, ipv6, direction, wfp_ip_header_size)
    {
        data.action_permit();
        return;
    }

    let Some(device) = crate::entry::get_device() else {
        return;
    };
    if device
        .injector
        .was_network_packet_injected_by_self(data.get_layer_data() as _, ipv6)
    {
        data.action_permit();
        return;
    }

    for mut nbl in NetBufferListIter::new(data.get_layer_data() as _) {
        if let Direction::Inbound = direction {
            // The header is not part of the NBL for incoming packets. Move the beginning of the buffer back so we get access to it.
            // The NBL will auto advance after it loses scope.
            retreat_to_ip_header(&mut nbl, ipv6, wfp_ip_header_size);
        }

        // Get key from packet.
        let key = match if ipv6 {
            get_key_from_nbl_v6(&nbl, direction)
        } else {
            get_key_from_nbl_v4(&nbl, direction)
        } {
            Ok(key) => key,
            Err(err) => {
                crate::err!("failed to get key from nbl: {}", err);
                return;
            }
        };

        if fast_track_pm_packets(&key, direction) {
            data.action_permit();
            return;
        }

        let mut send_request_to_portmaster = true;
        let mut process_id = 0;

        // For loopback ICMP echo reply, WFP reports it as OUTBOUND but it is
        // semantically INBOUND. Track the effective direction separately.
        let mut effective_direction = direction;

        // Protocols without ports - ICMP above all - are never resolved by the
        // machinery below: they are not classified at the ALE layers, and the
        // endpoint table is keyed by port, which they do not have. They used to be
        // reported with PID 0 for that reason.
        //
        // For an outbound packet the originator is available anyway, from the
        // thread this callout runs on. An application sending an echo request
        // travels down the stack synchronously on its own thread, so the current
        // process *is* the sender.
        //
        // Measured on Windows 11 with three concurrent `ping` processes: every
        // outbound ICMP indication carried the PID of the process that sent it, and
        // two pings to the same destination were told apart - which the destination
        // address alone cannot do. IRQL was DISPATCH_LEVEL throughout, where
        // PsGetCurrentProcessId is legal.
        //
        // Deliberately restricted to outbound. The same measurement showed inbound
        // indications carrying PID 0, System, and unrelated processes, because
        // receive processing happens in an arbitrary context - there the thread says
        // nothing about the packet. Measuring the transport and flow-established
        // layers did not help either: an echo reply is not indicated there at all,
        // because no socket is associated with it.
        //
        // An inbound echo reply is therefore matched against the request that caused
        // it, using the identifier the sender chose and the responder echoed back.
        if !matches!(
            key.protocol,
            smoltcp::wire::IpProtocol::Tcp | smoltcp::wire::IpProtocol::Udp
        ) {
            match direction {
                Direction::Outbound => {
                    if let Some(echo) = get_icmp_echo_from_nbl(&nbl, ipv6) {
                        if !echo.is_request {
                            // This is an echo reply reported as OUTBOUND. Two cases:
                            // 1. Reply to our own request (loopback or external): we
                            //    sent a request, this is the answer coming back. WFP
                            //    reports it as OUTBOUND (routing quirk). Semantically
                            //    it's inbound, and we have the request cached.
                            // 2. Our reply to someone else's request: they sent us a
                            //    request, this is our answer going out. WFP correctly
                            //    reports it as OUTBOUND, and we have no cached request.
                            //
                            // Distinguish by checking if we have a cached request.
                            let request_pid = device
                                .icmp_echo_cache
                                .take_request_pid(key.remote_address, echo.identifier);

                            if let Some(pid) = request_pid {
                                // Case 1: Found our request > this is a reply to us.
                                // Correct direction to INBOUND for semantic accuracy.
                                effective_direction = Direction::Inbound;
                                process_id = pid;
                            } else {
                                // Case 2: No cached request > this is our reply to them.
                                // This is a kernel stack reply (automatic ICMP response).
                                // current_process_id() would return arbitrary DPC context,
                                // so use 0 (System/kernel) instead.
                                process_id = 0;
                            }
                        } else {
                            // This is a request. Use the current process as the sender.
                            process_id = wdk::utils::current_process_id();

                            // Remember the request so its reply can be attributed.
                            device.icmp_echo_cache.insert_request(
                                key.remote_address,
                                echo.identifier,
                                process_id,
                            );
                        }
                    } else {
                        // Not an ICMP echo (request or reply), but still outbound
                        // non-TCP/UDP (e.g., ICMP destination unreachable, ICMPv6
                        // neighbor discovery, router advertisement). These are kernel
                        // stack originated. current_process_id() returns arbitrary
                        // DPC context, so use 0 (System/kernel).
                        process_id = 0;
                    }
                }
                Direction::Inbound => {
                    // Inbound ICMP echo replies are straightforward: someone sent us
                    // a request, they're getting their reply back. Try to attribute
                    // it to their original request if we cached it.
                    if let Some(echo) = get_icmp_echo_from_nbl(&nbl, ipv6) {
                        if !echo.is_request {
                            process_id = device
                                .icmp_echo_cache
                                .take_request_pid(key.remote_address, echo.identifier)
                                .unwrap_or(0);
                        }
                    }
                }
            }
        }

        if matches!(
            key.protocol,
            smoltcp::wire::IpProtocol::Tcp | smoltcp::wire::IpProtocol::Udp
        ) {
            if let Some(mut conn_info) =
                get_connection_info(&mut device.connection_cache, &key, ipv6)
            {
                process_id = conn_info.process_id;

                // A cached connection can carry PID 0 when this layer was the one
                // that created it - either before the owning process bound its
                // port, or on a build that had no way to resolve it at all. Fall
                // back to the bind-layer lookup so those connections stop
                // reporting 0 once the information becomes available.
                if process_id == 0 {
                    if let Some(pid) = lookup_endpoint_pid(device, &key, ipv6, direction) {
                        process_id = pid;
                        // Write it back, so the entry stops reporting 0 and later
                        // packets of this connection do not repeat the lookup. The
                        // update only applies to a stored 0, so a PID that is
                        // already known is never replaced.
                        device.connection_cache.update_process_id(&key, pid);
                    }
                }
                // Check if there is action for this connection.
                match conn_info.verdict {
                    Verdict::Undecided | Verdict::Accept | Verdict::Block | Verdict::Drop => {}
                    Verdict::PermanentAccept => {
                        send_request_to_portmaster = false;
                        data.action_permit();
                    }
                    Verdict::PermanentBlock => {
                        send_request_to_portmaster = false;
                        data.action_block_hard();
                    }
                    Verdict::Undeterminable | Verdict::PermanentDrop | Verdict::Failed => {
                        send_request_to_portmaster = false;
                        data.block_and_absorb();
                    }
                    Verdict::RedirectNameServer | Verdict::RedirectTunnel | Verdict::RedirectSplitTunnel => {
                        if let Some(redirect_info) = conn_info.redirect_info.take() {
                            match clone_packet(
                                device,
                                nbl,
                                effective_direction,
                                ipv6,
                                key.is_loopback(),
                                interface_index,
                                sub_interface_index,
                            ) {
                                Ok(mut packet) => {
                                    let _ = packet.redirect(redirect_info);
                                    if let Err(err) = device.inject_packet(packet, false) {
                                        crate::err!("failed to inject packet: {}", err);
                                    }
                                }
                                Err(err) => crate::err!("failed to clone packet: {}", err),
                            }
                        }

                        // This will block the original packet. Even if injection failed.
                        data.block_and_absorb();
                        continue;
                    }
                }
            } else {
                // Connection is not in the cache.
                //
                // This layer has no process ID of its own: no socket is
                // associated with the packet at the inbound IP packet layer, so
                // WFP supplies none. The owning process is resolved from the
                // endpoint ownership recorded by the ALE monitor instead. It stays 0
                // when no usable endpoint indication supplied a PID.
                process_id = lookup_endpoint_pid(device, &key, ipv6, effective_direction)
                    .unwrap_or(0);

                crate::dbg!(
                    "packet layer adding connection: {} PID: {}",
                    key,
                    process_id
                );
                if ipv6 {
                    let conn = ConnectionV6::from_key(&key, process_id, effective_direction).unwrap();
                    device.connection_cache.add_connection_v6(conn);
                } else {
                    let conn = ConnectionV4::from_key(&key, process_id, effective_direction).unwrap();
                    device.connection_cache.add_connection_v4(conn);
                }
            }
        }

        // Clone packet and send to Portmaster.
        if send_request_to_portmaster {
            let packet = match clone_packet(
                device,
                nbl,
                effective_direction,
                ipv6,
                key.is_loopback(),
                interface_index,
                sub_interface_index,
            ) {
                Ok(p) => p,
                Err(err) => {
                    crate::err!("failed to clone packet: {}", err);
                    return;
                }
            };

            let info = device
                .packet_cache
                .push((key, packet), process_id, effective_direction, false);

            // Send to Portmaster
            if let Some(info) = info {
                let _ = device.event_queue.push(info);
            }
            data.block_and_absorb();
        }
    }
}

fn clone_packet(
    device: &mut Device,
    nbl: NetBufferList,
    direction: Direction,
    ipv6: bool,
    loopback: bool,
    interface_index: u32,
    sub_interface_index: u32,
) -> Result<Packet, String> {
    let mut clones = nbl.clone_all(&device.network_allocator)?;
    let inbound = match direction {
        Direction::Outbound => false,
        Direction::Inbound => true,
    };

    for clone in &mut clones {
        if let Some(data) = clone.get_data_mut() {
            // Outbound packets intercepted at the IP layer may carry only a partial
            // pseudo-header checksum because the TCP/IP stack relies on NIC hardware
            // checksum offload to fill in the real value before transmission.
            // When this clone is later re-injected via FwpsInjectNetwork*Async (on
            // Accept/PermanentAccept verdict), it bypasses the NIC entirely, so offload
            // never runs. We must compute the full software checksum here.
            recalc_header_checksums(data, ipv6);
        }
    }

    Ok(Packet::PacketLayer(
        clones,
        InjectInfo {
            ipv6,
            inbound,
            loopback,
            interface_index,
            sub_interface_index,
        },
    ))
}

fn get_connection_info(
    connection_cache: &mut ConnectionCache,
    key: &Key,
    ipv6: bool,
) -> Option<ConnectionInfo> {
    if ipv6 {
        let conn_info = connection_cache.read_connection_v6(
            key,
            |conn: &ConnectionV6| -> Option<ConnectionInfo> {
                // Function is is behind spin lock. Just copy and return.
                Some(ConnectionInfo::from_connection(conn))
            },
        );
        return conn_info;
    } else {
        let conn_info = connection_cache.read_connection_v4(
            key,
            |conn: &ConnectionV4| -> Option<ConnectionInfo> {
                // Function is is behind spin lock. Just copy and return.
                Some(ConnectionInfo::from_connection(conn))
            },
        );
        return conn_info;
    }
}
