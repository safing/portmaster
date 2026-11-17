use std::io;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;

const SOCKS4_VERSION: u8 = 0x04;
const SOCKS4_CMD_CONNECT: u8 = 0x01;
const SOCKS4_REQ_GRANTED: u8 = 0x5a;
const SOCKS4_REPLY_VERSION: u8 = 0;

pub async fn socks4_connect(
    stream: &mut TcpStream,
    user_id: Option<&str>,
    host: &str,
    port: u16,
    is_4a: bool,
) -> io::Result<()> {
    let mut req = Vec::with_capacity(32);
    req.push(SOCKS4_VERSION);
    req.push(SOCKS4_CMD_CONNECT);
    req.extend_from_slice(&port.to_be_bytes());

    let (ip_octets, append_host) = if let Ok(ip) = host.parse::<std::net::IpAddr>() {
        match ip {
            std::net::IpAddr::V4(ipv4) => (ipv4.octets(), None),
            std::net::IpAddr::V6(_) => {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidInput,
                    "socks4 does not support IPv6 destinations",
                ));
            }
        }
    } else if is_4a {
        // SOCKS4a: IP 0.0.0.x with x non-zero indicates domain follows
        ([0, 0, 0, 1], Some(host))
    } else {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "socks4 requires an IPv4 address; use socks4a for domain names",
        ));
    };

    req.extend_from_slice(&ip_octets);
    if let Some(user) = user_id {
        req.extend_from_slice(user.as_bytes());
    }
    req.push(0x00); // NUL terminator for user ID

    if let Some(hostname) = append_host {
        req.extend_from_slice(hostname.as_bytes());
        req.push(0x00); // NUL terminator for hostname
    }

    stream.write_all(&req).await?;

    // Read 8-byte response: VN (0), CD (0x5A = granted), Port (2), IP (4)
    let mut resp = [0u8; 8];
    stream.read_exact(&mut resp).await?;
    if resp[0] != SOCKS4_REPLY_VERSION {
        return Err(io::Error::other(format!("unexpected SOCKS4 reply version {}", resp[0])));
    }
    if resp[1] != SOCKS4_REQ_GRANTED {
        let msg = match resp[1] {
            0x5b => "request rejected or failed",
            0x5c => "request rejected because SOCKS server cannot connect to identd on client",
            0x5d => "request rejected because client program and identd report different user IDs",
            other => return Err(io::Error::other(format!("socks4 error code 0x{:02x}", other))),
        };
        return Err(io::Error::new(io::ErrorKind::ConnectionRefused, msg));
    }

    Ok(())
}
