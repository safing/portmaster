use alloc::string::{String, ToString};
use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv4Address, Ipv4Packet, Ipv6Address, Ipv6Packet, TcpPacket, UdpPacket,
};
use wdk::filter_engine::net_buffer::NetBufferList;

use crate::connection_map::Key;
use crate::device::Packet;
use crate::{
    connection::{Direction, RedirectInfo},
    dbg, err,
};

/// `Redirect` is a trait that defines a method for redirecting network packets.
///
/// This trait is used to implement different strategies for redirecting packets,
/// depending on the specific requirements of the application.
pub trait Redirect {
    /// Redirects a network packet based on the provided `RedirectInfo`.
    ///
    /// # Arguments
    ///
    /// * `redirect_info` - A struct containing information about how to redirect the packet.
    ///
    /// # Returns
    ///
    /// * `Ok(())` if the packet was successfully redirected.
    /// * `Err(String)` if there was an error redirecting the packet.
    fn redirect(&mut self, redirect_info: RedirectInfo) -> Result<(), String>;
}

impl Redirect for Packet {
    fn redirect(&mut self, redirect_info: RedirectInfo) -> Result<(), String> {
        if let Packet::PacketLayer(nbl, inject_info) = self {
            let Some(data) = nbl.get_data_mut() else {
                return Err("trying to redirect immutable NBL".to_string());
            };

            if inject_info.inbound {
                redirect_inbound_packet(
                    data,
                    redirect_info.local_address,
                    redirect_info.remote_address,
                    redirect_info.remote_port,
                )
            } else {
                redirect_outbound_packet(
                    data,
                    redirect_info.redirect_address,
                    redirect_info.redirect_port,
                    redirect_info.unify,
                )
            }
            return Ok(());
        }
        // return Err("can't redirect from non packet layer".to_string());
        return Ok(());
    }
}

/// Redirects an outbound packet to a specified remote address and port.
///
/// # Arguments
///
/// * `packet` - A mutable reference to the packet data.
/// * `remote_address` - The IP address to redirect the packet to.
/// * `remote_port` - The port to redirect the packet to.
/// * `unify` - If true, the source and destination addresses of the packet will be set to the same value.
///
/// This function modifies the packet in-place to change its destination address and port.
/// It also updates the checksums for the IP and transport layer headers.
/// If the `unify` parameter is true, it sets the source and destination addresses to be the same.
/// If the remote address is a loopback address, it sets the source address to the loopback address.
fn redirect_outbound_packet(
    packet: &mut [u8],
    remote_address: IpAddress,
    remote_port: u16,
    unify: bool,
) {
    match remote_address {
        IpAddress::Ipv4(remote_address) => {
            if let Ok(mut ip_packet) = Ipv4Packet::new_checked(packet) {
                if unify {
                    ip_packet.set_dst_addr(ip_packet.src_addr());
                } else {
                    ip_packet.set_dst_addr(remote_address);
                    if remote_address.is_loopback() {
                        ip_packet.set_src_addr(Ipv4Address::new(127, 0, 0, 1));
                    }
                }
                ip_packet.fill_checksum();
                let src_addr = ip_packet.src_addr();
                let dst_addr = ip_packet.dst_addr();
                if ip_packet.next_header() == IpProtocol::Udp {
                    if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut()) {
                        udp_packet.set_dst_port(remote_port);
                        udp_packet
                            .fill_checksum(&IpAddress::Ipv4(src_addr), &IpAddress::Ipv4(dst_addr));
                    }
                }
                if ip_packet.next_header() == IpProtocol::Tcp {
                    if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut()) {
                        tcp_packet.set_dst_port(remote_port);
                        tcp_packet
                            .fill_checksum(&IpAddress::Ipv4(src_addr), &IpAddress::Ipv4(dst_addr));
                    }
                }
            }
        }
        IpAddress::Ipv6(remote_address) => {
            if let Ok(mut ip_packet) = Ipv6Packet::new_checked(packet) {
                ip_packet.set_dst_addr(remote_address);
                if unify {
                    ip_packet.set_dst_addr(ip_packet.src_addr());
                } else {
                    ip_packet.set_dst_addr(remote_address);
                    if remote_address.is_loopback() {
                        ip_packet.set_src_addr(Ipv6Address::LOOPBACK);
                    }
                }
                let src_addr = ip_packet.src_addr();
                let dst_addr = ip_packet.dst_addr();
                if ip_packet.next_header() == IpProtocol::Udp {
                    if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut()) {
                        udp_packet.set_dst_port(remote_port);
                        udp_packet
                            .fill_checksum(&IpAddress::Ipv6(src_addr), &IpAddress::Ipv6(dst_addr));
                    }
                }
                if ip_packet.next_header() == IpProtocol::Tcp {
                    if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut()) {
                        tcp_packet.set_dst_port(remote_port);
                        tcp_packet
                            .fill_checksum(&IpAddress::Ipv6(src_addr), &IpAddress::Ipv6(dst_addr));
                    }
                }
            }
        }
    }
}

/// Redirects an inbound packet to a local address.
///
/// This function takes a mutable reference to a packet and modifies it in place.
/// It changes the destination address to the provided local address and the source address
/// to the original remote address. It also sets the source port to the original remote port.
/// The function handles both IPv4 and IPv6 addresses.
///
/// # Arguments
///
/// * `packet` - A mutable reference to the packet data.
/// * `local_address` - The local IP address to redirect the packet to.
/// * `original_remote_address` - The original remote IP address of the packet.
/// * `original_remote_port` - The original remote port of the packet.
///
fn redirect_inbound_packet(
    packet: &mut [u8],
    local_address: IpAddress,
    original_remote_address: IpAddress,
    original_remote_port: u16,
) {
    match local_address {
        IpAddress::Ipv4(local_address) => {
            let IpAddress::Ipv4(original_remote_address) = original_remote_address else {
                return;
            };

            if let Ok(mut ip_packet) = Ipv4Packet::new_checked(packet) {
                ip_packet.set_dst_addr(local_address);
                ip_packet.set_src_addr(original_remote_address);
                ip_packet.fill_checksum();
                let src_addr = ip_packet.src_addr();
                let dst_addr = ip_packet.dst_addr();
                if ip_packet.next_header() == IpProtocol::Udp {
                    if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut()) {
                        udp_packet.set_src_port(original_remote_port);
                        udp_packet
                            .fill_checksum(&IpAddress::Ipv4(src_addr), &IpAddress::Ipv4(dst_addr));
                    }
                }
                if ip_packet.next_header() == IpProtocol::Tcp {
                    if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut()) {
                        tcp_packet.set_src_port(original_remote_port);
                        tcp_packet
                            .fill_checksum(&IpAddress::Ipv4(src_addr), &IpAddress::Ipv4(dst_addr));
                    }
                }
            }
        }
        IpAddress::Ipv6(local_address) => {
            if let Ok(mut ip_packet) = Ipv6Packet::new_checked(packet) {
                let IpAddress::Ipv6(original_remote_address) = original_remote_address else {
                    return;
                };
                ip_packet.set_dst_addr(local_address);
                ip_packet.set_src_addr(original_remote_address);
                let src_addr = ip_packet.src_addr();
                let dst_addr = ip_packet.dst_addr();
                if ip_packet.next_header() == IpProtocol::Udp {
                    if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut()) {
                        udp_packet.set_src_port(original_remote_port);
                        udp_packet
                            .fill_checksum(&IpAddress::Ipv6(src_addr), &IpAddress::Ipv6(dst_addr));
                    }
                }
                if ip_packet.next_header() == IpProtocol::Tcp {
                    if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut()) {
                        tcp_packet.set_src_port(original_remote_port);
                        tcp_packet
                            .fill_checksum(&IpAddress::Ipv6(src_addr), &IpAddress::Ipv6(dst_addr));
                    }
                }
            }
        }
    }
}

#[allow(dead_code)]
fn print_packet(packet: &[u8]) {
    if let Ok(ip_packet) = Ipv4Packet::new_checked(packet) {
        if ip_packet.next_header() == IpProtocol::Udp {
            if let Ok(udp_packet) = UdpPacket::new_checked(ip_packet.payload()) {
                dbg!("packet {} {}", ip_packet, udp_packet);
            }
        }
        if ip_packet.next_header() == IpProtocol::Tcp {
            if let Ok(tcp_packet) = TcpPacket::new_checked(ip_packet.payload()) {
                dbg!("packet {} {}", ip_packet, tcp_packet);
            }
        }
    } else {
        err!("failed to print packet: invalid ip header: {:?}", packet);
    }
}

/// This function extracts a key from a given IPv4 network buffer list (NBL).
/// The key contains the protocol, local and remote addresses and ports.
///
/// # Arguments
///
/// * `nbl` - A reference to the network buffer list from which the key will be extracted.
/// * `direction` - The direction of the packet (Inbound or Outbound).
///
/// # Returns
///
/// * `Ok(Key)` - A key containing the protocol, local and remote addresses and ports.
/// * `Err(String)` - An error message if the function fails to get net_buffer data.
fn get_ports(packet: &[u8], protocol: smoltcp::wire::IpProtocol) -> (u16, u16) {
    match protocol {
        smoltcp::wire::IpProtocol::Tcp => {
            let tcp_packet = TcpPacket::new_unchecked(packet);
            (tcp_packet.src_port(), tcp_packet.dst_port())
        }
        smoltcp::wire::IpProtocol::Udp => {
            let udp_packet = UdpPacket::new_unchecked(packet);
            (udp_packet.src_port(), udp_packet.dst_port())
        }
        _ => (0, 0), // No ports for other protocols
    }
}

pub fn get_key_from_nbl_v4(nbl: &NetBufferList, direction: Direction) -> Result<Key, String> {
    // Get first bytes of the packet. IP header + src port (2 bytes) + dst port (2 bytes)
    let mut headers = [0; smoltcp::wire::IPV4_HEADER_LEN + 4];
    if nbl.read_bytes(&mut headers).is_err() {
        return Err("failed to get net_buffer data".to_string());
    }

    // This will panic in debug mode, probably because of runtime checks.
    // Parse packet
    let ip_packet = Ipv4Packet::new_unchecked(&headers);
    let (src_port, dst_port) = get_ports(
        &headers[smoltcp::wire::IPV4_HEADER_LEN..],
        ip_packet.next_header(),
    );

    // Build key
    match direction {
        Direction::Outbound => Ok(Key {
            protocol: ip_packet.next_header(),
            local_address: IpAddress::Ipv4(ip_packet.src_addr()),
            local_port: src_port,
            remote_address: IpAddress::Ipv4(ip_packet.dst_addr()),
            remote_port: dst_port,
        }),
        Direction::Inbound => Ok(Key {
            protocol: ip_packet.next_header(),
            local_address: IpAddress::Ipv4(ip_packet.dst_addr()),
            local_port: dst_port,
            remote_address: IpAddress::Ipv4(ip_packet.src_addr()),
            remote_port: src_port,
        }),
    }
}

/// This function extracts a key from a given IPv6 network buffer list (NBL).
/// The key contains the protocol, local and remote addresses and ports.
///
/// # Arguments
///
/// * `nbl` - A reference to the network buffer list from which the key will be extracted.
/// * `direction` - The direction of the packet (Inbound or Outbound).
///
/// # Returns
///
/// * `Ok(Key)` - A key containing the protocol, local and remote addresses and ports.
/// * `Err(String)` - An error message if the function fails to get net_buffer data.
pub fn get_key_from_nbl_v6(nbl: &NetBufferList, direction: Direction) -> Result<Key, String> {
    // Get first bytes of the packet. IP header + src port (2 bytes) + dst port (2 bytes)
    let mut headers = [0; smoltcp::wire::IPV6_HEADER_LEN + 4];
    let Ok(()) = nbl.read_bytes(&mut headers) else {
        return Err("failed to get net_buffer data".to_string());
    };

    // This will panic in debug mode, probably because of runtime checks.
    // Parse packet
    let ip_packet = Ipv6Packet::new_unchecked(&headers);
    let (src_port, dst_port) = get_ports(
        &headers[smoltcp::wire::IPV6_HEADER_LEN..],
        ip_packet.next_header(),
    );

    // Build key
    match direction {
        Direction::Outbound => Ok(Key {
            protocol: ip_packet.next_header(),
            local_address: IpAddress::Ipv6(ip_packet.src_addr()),
            local_port: src_port,
            remote_address: IpAddress::Ipv6(ip_packet.dst_addr()),
            remote_port: dst_port,
        }),
        Direction::Inbound => Ok(Key {
            protocol: ip_packet.next_header(),
            local_address: IpAddress::Ipv6(ip_packet.dst_addr()),
            local_port: dst_port,
            remote_address: IpAddress::Ipv6(ip_packet.src_addr()),
            remote_port: src_port,
        }),
    }
}

// Converts a given key into connection information.
//
// This function takes a key, packet id, process id, and direction as input.
// It then uses these to create a new `ConnectionInfoV6` or `ConnectionInfoV4` object,
// depending on whether the IP addresses in the key are IPv6 or IPv4 respectively.
//
// # Arguments
//
// * `key` - A reference to the key object containing the connection details.
// * `packet_id` - The id of the packet.
// * `process_id` - The id of the process.
// * `direction` - The direction of the connection.
//
// # Returns
//
// * `Some(Box<dyn Info>)` - A boxed `Info` trait object if the key contains valid IPv4 or IPv6 addresses.
// * `None` - If the key does not contain valid IPv4 or IPv6 addresses.
// pub fn key_to_connection_info(
//     key: &Key,
//     packet_id: u64,
//     process_id: u64,
//     direction: Direction,
//     payload: &[u8],
// ) -> Option<Info> {
//     let (local_port, remote_port) = match key.protocol {
//         IpProtocol::Tcp | IpProtocol::Udp => (key.local_port, key.remote_port),
//         _ => (0, 0),
//     };

//     match (key.local_address, key.remote_address) {
//         (IpAddress::Ipv6(local_ip), IpAddress::Ipv6(remote_ip)) if key.is_ipv6() => {
//             Some(protocol::info::connection_info_v6(
//                 packet_id,
//                 process_id,
//                 direction as u8,
//                 u8::from(key.protocol),
//                 local_ip.0,
//                 remote_ip.0,
//                 local_port,
//                 remote_port,
//                 payload,
//             ))
//         }
//         (IpAddress::Ipv4(local_ip), IpAddress::Ipv4(remote_ip)) => {
//             Some(protocol::info::connection_info_v4(
//                 packet_id,
//                 process_id,
//                 direction as u8,
//                 u8::from(key.protocol),
//                 local_ip.0,
//                 remote_ip.0,
//                 local_port,
//                 remote_port,
//                 payload,
//             ))
//         }
//         _ => None,
//     }
// }
