use std::io;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;

const SOCKS5_VERSION: u8 = 0x05;
const SOCKS5_AUTH_NONE: u8 = 0x00;
const SOCKS5_AUTH_USER_PASS: u8 = 0x02;
const SOCKS5_AUTH_NO_ACCEPTABLE: u8 = 0xFF;
const SOCKS5_USER_PASS_VERSION: u8 = 0x01;
const SOCKS5_CMD_CONNECT: u8 = 0x01;
const SOCKS5_ADDR_IPV4: u8 = 0x01;
const SOCKS5_ADDR_DOMAIN: u8 = 0x03;
const SOCKS5_ADDR_IPV6: u8 = 0x04;

pub async fn socks5_connect(
    stream: &mut TcpStream,
    username: Option<&str>,
    password: Option<&str>,
    host: &str,
    port: u16,
) -> io::Result<()> {
    // 1. Negotiation
    if let (Some(user), pass) = (username, password) {
        // Offer both User/Pass and None
        stream.write_all(&[SOCKS5_VERSION, 2, SOCKS5_AUTH_NONE, SOCKS5_AUTH_USER_PASS]).await?;
        let mut resp = [0u8; 2];
        stream.read_exact(&mut resp).await?;
        if resp[0] != SOCKS5_VERSION {
            return Err(io::Error::new(io::ErrorKind::InvalidData, "invalid socks5 version in auth response"));
        }
        if resp[1] == SOCKS5_AUTH_NO_ACCEPTABLE {
            return Err(io::Error::new(io::ErrorKind::PermissionDenied, "socks5: no acceptable authentication methods"));
        }
        if resp[1] == SOCKS5_AUTH_USER_PASS {
            let user_bytes = user.as_bytes();
            let pass_bytes = pass.unwrap_or("").as_bytes();
            if user_bytes.len() > 255 || pass_bytes.len() > 255 {
                return Err(io::Error::new(io::ErrorKind::InvalidInput, "socks5: credentials too long"));
            }
            let mut auth_req = Vec::with_capacity(3 + user_bytes.len() + pass_bytes.len());
            auth_req.push(SOCKS5_USER_PASS_VERSION);
            auth_req.push(user_bytes.len() as u8);
            auth_req.extend_from_slice(user_bytes);
            auth_req.push(pass_bytes.len() as u8);
            auth_req.extend_from_slice(pass_bytes);

            stream.write_all(&auth_req).await?;
            let mut auth_resp = [0u8; 2];
            stream.read_exact(&mut auth_resp).await?;
            if auth_resp[1] != 0 {
                return Err(io::Error::new(io::ErrorKind::PermissionDenied, "socks5: authentication failed"));
            }
        }
    } else {
        // No authentication
        stream.write_all(&[SOCKS5_VERSION, 1, SOCKS5_AUTH_NONE]).await?;
        let mut resp = [0u8; 2];
        stream.read_exact(&mut resp).await?;
        if resp[0] != SOCKS5_VERSION || resp[1] != SOCKS5_AUTH_NONE {
            return Err(io::Error::new(io::ErrorKind::PermissionDenied, "socks5: server requires unsupported authentication"));
        }
    }

    // 2. CONNECT Request
    let mut req = Vec::with_capacity(32);
    req.extend_from_slice(&[SOCKS5_VERSION, SOCKS5_CMD_CONNECT, 0x00]);

    if let Ok(ip) = host.parse::<std::net::IpAddr>() {
        match ip {
            std::net::IpAddr::V4(ipv4) => {
                req.push(SOCKS5_ADDR_IPV4);
                req.extend_from_slice(&ipv4.octets());
            }
            std::net::IpAddr::V6(ipv6) => {
                req.push(SOCKS5_ADDR_IPV6);
                req.extend_from_slice(&ipv6.octets());
            }
        }
    } else {
        let host_bytes = host.as_bytes();
        if host_bytes.is_empty() || host_bytes.len() > 255 {
            return Err(io::Error::new(io::ErrorKind::InvalidInput, "socks5: host name empty or too long"));
        }
        req.push(SOCKS5_ADDR_DOMAIN);
        req.push(host_bytes.len() as u8);
        req.extend_from_slice(host_bytes);
    }
    req.extend_from_slice(&port.to_be_bytes());

    stream.write_all(&req).await?;

    // 3. Read Reply
    let mut header = [0u8; 4];
    stream.read_exact(&mut header).await?;
    if header[0] != SOCKS5_VERSION {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "socks5: invalid reply version"));
    }
    if header[1] != 0x00 {
        let msg = match header[1] {
            0x01 => "general SOCKS server failure",
            0x02 => "connection not allowed by ruleset",
            0x03 => "network unreachable",
            0x04 => "host unreachable",
            0x05 => "connection refused",
            0x06 => "TTL expired",
            0x07 => "command not supported",
            0x08 => "address type not supported",
            other => return Err(io::Error::other(format!("socks5 error code 0x{:02x}", other))),
        };
        return Err(io::Error::new(io::ErrorKind::ConnectionRefused, msg));
    }

    // Skip bound address
    match header[3] {
        SOCKS5_ADDR_IPV4 => {
            let mut discard = [0u8; 4 + 2];
            stream.read_exact(&mut discard).await?;
        }
        SOCKS5_ADDR_IPV6 => {
            let mut discard = [0u8; 16 + 2];
            stream.read_exact(&mut discard).await?;
        }
        SOCKS5_ADDR_DOMAIN => {
            let len = stream.read_u8().await?;
            let mut discard = vec![0u8; len as usize + 2];
            stream.read_exact(&mut discard).await?;
        }
        _ => return Err(io::Error::new(io::ErrorKind::InvalidData, "socks5: invalid address type in reply")),
    }

    Ok(())
}
