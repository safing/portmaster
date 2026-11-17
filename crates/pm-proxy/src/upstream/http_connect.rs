use std::io;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::TcpStream;

pub async fn http_connect(
    stream: &mut TcpStream,
    username: Option<&str>,
    password: Option<&str>,
    host: &str,
    port: u16,
) -> io::Result<()> {
    // Defense in depth: the host is validated before it reaches this point,
    // but never let control characters into the CONNECT request line.
    if host.is_empty()
        || host.len() > 253
        || host.bytes().any(|b| b.is_ascii_control() || b.is_ascii_whitespace())
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "invalid host in CONNECT request",
        ));
    }

    let authority = if host.contains(':') && !host.starts_with('[') {
        format!("[{}]:{}", host, port)
    } else {
        format!("{}:{}", host, port)
    };

    let mut req = format!("CONNECT {} HTTP/1.1\r\nHost: {}\r\n", authority, authority);

    if let Some(user) = username {
        let auth_raw = format!("{}:{}", user, password.unwrap_or(""));
        let encoded = base64_simple(auth_raw.as_bytes());
        req.push_str(&format!("Proxy-Authorization: Basic {}\r\n", encoded));
    }
    req.push_str("Proxy-Connection: Keep-Alive\r\n\r\n");

    stream.write_all(req.as_bytes()).await?;

    // Read response line-by-line until empty line
    let mut reader = BufReader::new(stream);
    let mut status_line = String::new();
    reader.read_line(&mut status_line).await?;

    let parts: Vec<&str> = status_line.split_whitespace().collect();
    if parts.len() < 2 || !parts[0].starts_with("HTTP/") {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "malformed HTTP response from proxy"));
    }

    let status_code: u16 = parts[1].parse().map_err(|_| {
        io::Error::new(io::ErrorKind::InvalidData, "invalid status code in proxy response")
    })?;

    if !(200..300).contains(&status_code) {
        return Err(io::Error::new(
            io::ErrorKind::ConnectionRefused,
            format!("proxy returned HTTP status {}", status_code),
        ));
    }

    // Read remaining headers until \r\n
    let mut line = String::new();
    loop {
        line.clear();
        let bytes_read = reader.read_line(&mut line).await?;
        if bytes_read == 0 || line == "\r\n" || line == "\n" {
            break;
        }
    }

    Ok(())
}

fn base64_simple(input: &[u8]) -> String {
    const TABLE: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::new();
    for chunk in input.chunks(3) {
        let b0 = chunk[0];
        let b1 = if chunk.len() > 1 { chunk[1] } else { 0 };
        let b2 = if chunk.len() > 2 { chunk[2] } else { 0 };

        out.push(TABLE[(b0 >> 2) as usize] as char);
        out.push(TABLE[(((b0 & 0x03) << 4) | (b1 >> 4)) as usize] as char);
        if chunk.len() > 1 {
            out.push(TABLE[(((b1 & 0x0f) << 2) | (b2 >> 6)) as usize] as char);
        } else {
            out.push('=');
        }
        if chunk.len() > 2 {
            out.push(TABLE[(b2 & 0x3f) as usize] as char);
        } else {
            out.push('=');
        }
    }
    out
}
