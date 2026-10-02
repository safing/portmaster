use alloc::string::{String, ToString};
use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv4Address, Ipv4Packet, Ipv6Address, Ipv6Packet, TcpPacket, UdpPacket,
    IPV4_HEADER_LEN, IPV6_HEADER_LEN,
};
use wdk::filter_engine::net_buffer::NetBufferList;

use crate::device::Packet;
use crate::ipv6_packet::{recalculate_ipv6_transport_checksum, rewrite_ipv6_tcp_udp, PortRewrite};
use crate::{
    connection::{Direction, RedirectInfo},
    dbg, err,
    packet_metadata::{
        inspect_packet as inspect_packet_bytes, PacketInspection, MAX_PACKET_INSPECT_LEN,
    },
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
        match self {
            Packet::Network(nbl, inject_info) => {
                redirect_net_buffer(nbl, inject_info.inbound, &redirect_info)
            }
            Packet::NetworkBatch(nbls, inject_info) => {
                for nbl in nbls {
                    redirect_net_buffer(nbl, inject_info.inbound, &redirect_info)?;
                }
                Ok(())
            }
            Packet::AleLayer(_) => Ok(()),
        }
    }
}

fn redirect_net_buffer(
    nbl: &mut NetBufferList,
    inbound: bool,
    redirect_info: &RedirectInfo,
) -> Result<(), String> {
    let Some(data) = nbl.get_data_mut() else {
        return Err("trying to redirect immutable NBL".to_string());
    };

    if inbound {
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
) -> Result<(), String> {
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
                let protocol = ip_packet.next_header();
                let source = IpAddress::Ipv4(ip_packet.src_addr());
                let destination = IpAddress::Ipv4(ip_packet.dst_addr());
                match protocol {
                    IpProtocol::Udp => {
                        if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut())
                        {
                            udp_packet.set_dst_port(remote_port);
                            udp_packet.fill_checksum(&source, &destination);
                        }
                    }
                    IpProtocol::Tcp => {
                        if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut())
                        {
                            tcp_packet.set_dst_port(remote_port);
                            tcp_packet.fill_checksum(&source, &destination);
                        }
                    }
                    _ => {}
                }
            }
            Ok(())
        }
        IpAddress::Ipv6(remote_address) => {
            // The base Next Header can name an extension header, so resolve the
            // upper-layer offset before changing either addresses or ports.
            let ip_packet = Ipv6Packet::new_checked(&*packet)
                .map_err(|_| "invalid outbound IPv6 packet".to_string())?;
            let original_source = ip_packet.src_addr();
            let destination = if unify {
                original_source
            } else {
                remote_address
            };
            let source = if !unify && remote_address.is_loopback() {
                Ipv6Address::LOOPBACK
            } else {
                original_source
            };

            rewrite_ipv6_tcp_udp(
                packet,
                source,
                destination,
                PortRewrite::Destination(remote_port),
            )
            .map_err(|error| error.to_string())
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
) -> Result<(), String> {
    match local_address {
        IpAddress::Ipv4(local_address) => {
            let IpAddress::Ipv4(original_remote_address) = original_remote_address else {
                return Err("IPv4 redirect has an IPv6 remote address".to_string());
            };

            if let Ok(mut ip_packet) = Ipv4Packet::new_checked(packet) {
                ip_packet.set_dst_addr(local_address);
                ip_packet.set_src_addr(original_remote_address);
                ip_packet.fill_checksum();
                let protocol = ip_packet.next_header();
                let source = IpAddress::Ipv4(ip_packet.src_addr());
                let destination = IpAddress::Ipv4(ip_packet.dst_addr());
                match protocol {
                    IpProtocol::Udp => {
                        if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut())
                        {
                            udp_packet.set_src_port(original_remote_port);
                            udp_packet.fill_checksum(&source, &destination);
                        }
                    }
                    IpProtocol::Tcp => {
                        if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut())
                        {
                            tcp_packet.set_src_port(original_remote_port);
                            tcp_packet.fill_checksum(&source, &destination);
                        }
                    }
                    _ => {}
                }
            }
            Ok(())
        }
        IpAddress::Ipv6(local_address) => {
            let IpAddress::Ipv6(original_remote_address) = original_remote_address else {
                return Err("IPv6 redirect has an IPv4 remote address".to_string());
            };

            rewrite_ipv6_tcp_udp(
                packet,
                original_remote_address,
                local_address,
                PortRewrite::Source(original_remote_port),
            )
            .map_err(|error| error.to_string())
        }
    }
}

pub fn recalc_header_checksums(packet: &mut [u8], ipv6: bool) -> Result<(), String> {
    if ipv6 {
        // TCP/UDP start after the complete extension-header chain, and the
        // pseudo-header length is the upper-layer length rather than the IPv6
        // payload length (which includes extensions).
        recalculate_ipv6_transport_checksum(packet).map_err(|error| error.to_string())?;
    } else {
        if let Ok(mut ip_packet) = Ipv4Packet::new_checked(packet) {
            ip_packet.fill_checksum();
            let protocol = ip_packet.next_header();
            let source = IpAddress::Ipv4(ip_packet.src_addr());
            let destination = IpAddress::Ipv4(ip_packet.dst_addr());
            match protocol {
                IpProtocol::Udp => {
                    if let Ok(mut udp_packet) = UdpPacket::new_checked(ip_packet.payload_mut()) {
                        udp_packet.fill_checksum(&source, &destination);
                    }
                }
                IpProtocol::Tcp => {
                    if let Ok(mut tcp_packet) = TcpPacket::new_checked(ip_packet.payload_mut()) {
                        tcp_packet.fill_checksum(&source, &destination);
                    }
                }
                _ => {}
            }
        }
    }

    Ok(())
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

/// Reads one bounded packet prefix from NDIS and derives all packet-callout
/// metadata from that same snapshot.
pub(crate) fn inspect_packet(
    nbl: &NetBufferList,
    ipv6: bool,
    direction: Direction,
) -> PacketInspection {
    // The IPv4 fallback reaches the TCP flags byte with a base-sized header and
    // also covers the complete ICMP header. IPv6 extension parsing retains its
    // established base-header fallback.
    const IPV4_FALLBACK_LEN: usize = IPV4_HEADER_LEN + 14;
    let fallback_length = if ipv6 {
        IPV6_HEADER_LEN
    } else {
        IPV4_FALLBACK_LEN
    };
    let mut packet = [0u8; MAX_PACKET_INSPECT_LEN];
    match nbl.read_bytes_up_to(&mut packet, fallback_length) {
        Ok(length) => inspect_packet_bytes(&packet[..length], ipv6, direction),
        Err(()) => PacketInspection::unreadable(),
    }
}
