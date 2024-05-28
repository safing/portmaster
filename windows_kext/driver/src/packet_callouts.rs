use alloc::string::String;
use smoltcp::wire::{IPV4_HEADER_LEN, IPV6_HEADER_LEN};
use wdk::filter_engine::callout_data::CalloutData;
use wdk::filter_engine::layer;
use wdk::filter_engine::net_buffer::{NetBufferList, NetBufferListIter};
use wdk::filter_engine::packet::InjectInfo;

use crate::connection::{
    Connection, ConnectionV4, ConnectionV6, Direction, RedirectInfo, Verdict, PM_DNS_PORT,
    PM_SPN_PORT,
};
use crate::connection_cache::ConnectionCache;
use crate::connection_map::Key;
use crate::device::{Device, Packet};
use crate::packet_util::{get_key_from_nbl_v4, get_key_from_nbl_v6, Redirect};
use crate::{err, warn};

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
    );
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

fn fast_track_pm_packets(key: &Key, direction: Direction) -> bool {
    match direction {
        Direction::Outbound => {
            if key.local_port == PM_DNS_PORT || key.local_port == PM_SPN_PORT {
                return key.local_address == key.remote_address;
            }
        }
        Direction::Inbound => {
            if key.local_port == PM_DNS_PORT || key.local_port == PM_SPN_PORT {
                return key.local_address == key.remote_address;
            }
        }
    }

    return false;
}

fn ip_packet_layer(
    mut data: CalloutData,
    ipv6: bool,
    direction: Direction,
    interface_index: u32,
    sub_interface_index: u32,
) {
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
            if ipv6 {
                nbl.retreat(IPV6_HEADER_LEN as u32, true);
            } else {
                nbl.retreat(IPV4_HEADER_LEN as u32, true);
            }
        }

        // Get key from packet.
        let key = match if ipv6 {
            get_key_from_nbl_v6(&nbl, direction)
        } else {
            get_key_from_nbl_v4(&nbl, direction)
        } {
            Ok(key) => key,
            Err(err) => {
                warn!("failed to get key from nbl: {}", err);
                return;
            }
        };

        if fast_track_pm_packets(&key, direction) {
            data.action_permit();
            return;
        }

        let mut is_tmp_verdict = false;
        let mut process_id = 0;

        if matches!(
            key.protocol,
            smoltcp::wire::IpProtocol::Tcp | smoltcp::wire::IpProtocol::Udp
        ) {
            if let Some(mut conn_info) =
                get_connection_info(&mut device.connection_cache, &key, ipv6)
            {
                process_id = conn_info.process_id;
                // Check if there is action for this connection.
                match conn_info.verdict {
                    Verdict::Undecided | Verdict::Accept | Verdict::Block | Verdict::Drop => {
                        is_tmp_verdict = true
                    }
                    Verdict::PermanentAccept => data.action_permit(),
                    Verdict::PermanentBlock => data.action_block(),
                    Verdict::Undeterminable | Verdict::PermanentDrop | Verdict::Failed => {
                        data.block_and_absorb()
                    }
                    Verdict::RedirectNameServer | Verdict::RedirectTunnel => {
                        if let Some(redirect_info) = conn_info.redirect_info.take() {
                            match clone_packet(
                                device,
                                nbl,
                                direction,
                                ipv6,
                                key.is_loopback(),
                                interface_index,
                                sub_interface_index,
                            ) {
                                Ok(mut packet) => {
                                    let _ = packet.redirect(redirect_info);
                                    if let Err(err) = device.inject_packet(packet, false) {
                                        err!("failed to inject packet: {}", err);
                                    }
                                }
                                Err(err) => err!("failed to clone packet: {}", err),
                            }
                        }

                        // This will block the original packet. Even if injection failed.
                        data.block_and_absorb();
                        continue;
                    }
                }
            } else {
                // TCP and UDP always need to go through ALE layer first.
                if matches!(direction, Direction::Inbound) {
                    // If it's an inbound packet and the connection is not found, we need to continue to ALE layer
                    data.action_permit();
                    return;
                } else {
                    // This happens sometimes. Leave the decision for portmaster. TODO(vladimir): Find out why.
                    err!("Invalid state for: {}", key);
                    is_tmp_verdict = true;
                }
            }
        } else {
            // Every other protocol treat as a tmp verdict.
            is_tmp_verdict = true;
        }

        // Clone packet and send to Portmaster if it's a temporary verdict.
        if is_tmp_verdict {
            let packet = match clone_packet(
                device,
                nbl,
                direction,
                ipv6,
                key.is_loopback(),
                interface_index,
                sub_interface_index,
            ) {
                Ok(p) => p,
                Err(err) => {
                    err!("failed to clone packet: {}", err);
                    return;
                }
            };

            let info = device
                .packet_cache
                .push((key, packet), process_id, direction, false);
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
    let clone = nbl.clone(&device.network_allocator)?;
    let inbound = match direction {
        Direction::Outbound => false,
        Direction::Inbound => true,
    };
    Ok(Packet::PacketLayer(
        clone,
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
            &key,
            |conn: &ConnectionV6| -> Option<ConnectionInfo> {
                // Function is is behind spin lock. Just copy and return.
                Some(ConnectionInfo::from_connection(conn))
            },
        );
        return conn_info;
    } else {
        let conn_info = connection_cache.read_connection_v4(
            &key,
            |conn: &ConnectionV4| -> Option<ConnectionInfo> {
                // Function is is behind spin lock. Just copy and return.
                Some(ConnectionInfo::from_connection(conn))
            },
        );
        return conn_info;
    }
}
