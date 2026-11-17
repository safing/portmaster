//! Outbound socket helpers for the local binding selected by the decider.
//!
//! This is the Rust counterpart of Go `newDialer` and `applyBindToDevice`:
//! the source address is chosen to match the remote IP version (falling back
//! to the alternate address), and on Linux the socket is bound to the selected
//! interface with `SO_BINDTODEVICE` before it is connected.

use std::io;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};
use std::time::Duration;

use tokio::net::{TcpSocket, TcpStream, UdpSocket};

use crate::binding::LocalBinding;

/// Selects the local source address to bind to for a remote address.
///
/// Mirrors Go `newDialer`: an explicit `ip` is replaced by `alt_ip` when its IP
/// version does not match the remote's, and a binding with no usable address of
/// the remote's version is an error. Without any binding the OS picks the
/// source address.
pub(crate) fn local_ip_for(
    binding: Option<&LocalBinding>,
    remote_ip: IpAddr,
) -> io::Result<Option<IpAddr>> {
    let Some(binding) = binding else {
        return Ok(None);
    };
    let Some(ip) = binding.ip else {
        return Ok(None);
    };
    if same_ip_version(ip, remote_ip) {
        return Ok(Some(ip));
    }
    if let Some(alt) = binding.alt_ip {
        if same_ip_version(alt, remote_ip) {
            return Ok(Some(alt));
        }
    }
    Err(io::Error::new(
        io::ErrorKind::InvalidInput,
        format!("no local address of the same IP version as {remote_ip} to bind to"),
    ))
}

fn same_ip_version(a: IpAddr, b: IpAddr) -> bool {
    a.is_ipv4() == b.is_ipv4()
}

fn wildcard(ip: IpAddr) -> SocketAddr {
    match ip {
        IpAddr::V4(_) => SocketAddr::from((Ipv4Addr::UNSPECIFIED, 0)),
        IpAddr::V6(_) => SocketAddr::from((Ipv6Addr::UNSPECIFIED, 0)),
    }
}

fn timeout_error(message: &'static str) -> io::Error {
    io::Error::new(io::ErrorKind::TimedOut, message)
}

/// Connects a TCP socket to `remote`, bound according to `binding`.
pub(crate) async fn connect_tcp(
    remote: SocketAddr,
    binding: Option<&LocalBinding>,
    timeout: Duration,
) -> io::Result<TcpStream> {
    let socket = match remote.ip() {
        IpAddr::V4(_) => TcpSocket::new_v4()?,
        IpAddr::V6(_) => TcpSocket::new_v6()?,
    };

    // SO_BINDTODEVICE has to be applied before the local bind and connect.
    // The Go port is a no-op on non-Linux platforms.
    #[cfg(target_os = "linux")]
    {
        if let Some(iface) = binding.and_then(|b| b.interface.as_deref()) {
            socket2::SockRef::from(&socket).bind_device(Some(iface.as_bytes()))?;
        }
    }

    if let Some(local_ip) = local_ip_for(binding, remote.ip())? {
        socket.bind(SocketAddr::new(local_ip, 0))?;
    }

    tokio::time::timeout(timeout, socket.connect(remote))
        .await
        .map_err(|_| timeout_error("tcp dial timed out"))
}

/// Resolves `host:port` to all its addresses, like Go `UpstreamProxy.resolve`.
pub(crate) async fn resolve_peers(
    host: &str,
    port: u16,
    timeout: Duration,
) -> io::Result<Vec<SocketAddr>> {
    let target = format!("{}:{}", host, port);
    let addresses = tokio::time::timeout(timeout, tokio::net::lookup_host(target.as_str()))
        .await
        .map_err(|_| timeout_error("proxy address resolution timed out"))??;
    Ok(addresses.collect())
}

/// Connects a UDP socket to `remote`, bound according to `binding`.
pub(crate) async fn connect_udp(
    remote: SocketAddr,
    binding: Option<&LocalBinding>,
) -> io::Result<UdpSocket> {
    let domain = match remote.ip() {
        IpAddr::V4(_) => socket2::Domain::IPV4,
        IpAddr::V6(_) => socket2::Domain::IPV6,
    };
    let socket = socket2::Socket::new(domain, socket2::Type::DGRAM, None)?;

    #[cfg(target_os = "linux")]
    {
        if let Some(iface) = binding.and_then(|b| b.interface.as_deref()) {
            socket.bind_device(Some(iface.as_bytes()))?;
        }
    }

    let bind_addr = match local_ip_for(binding, remote.ip())? {
        Some(ip) => SocketAddr::new(ip, 0),
        None => wildcard(remote.ip()),
    };
    socket.bind(&bind_addr.into())?;
    socket.set_nonblocking(true)?;

    let std_socket: std::net::UdpSocket = socket.into();
    let udp = UdpSocket::from_std(std_socket)?;
    udp.connect(remote).await?;
    Ok(udp)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn no_binding_leaves_the_source_unspecified() {
        assert_eq!(local_ip_for(None, "1.1.1.1".parse().unwrap()).unwrap(), None);
    }

    #[test]
    fn alt_ip_is_used_when_the_versions_differ() {
        let binding = LocalBinding::new()
            .with_ip("192.168.1.10".parse::<IpAddr>().unwrap())
            .with_alt_ip("2001:db8::10".parse().unwrap());
        let selected = local_ip_for(Some(&binding), "2001:db8::1".parse().unwrap())
            .unwrap()
            .unwrap();
        assert_eq!(selected, "2001:db8::10".parse::<IpAddr>().unwrap());
    }

    #[test]
    fn incompatible_binding_is_an_error() {
        let binding = LocalBinding::new().with_ip("192.168.1.10".parse::<IpAddr>().unwrap());
        assert!(local_ip_for(Some(&binding), "2001:db8::1".parse().unwrap()).is_err());
    }
}
