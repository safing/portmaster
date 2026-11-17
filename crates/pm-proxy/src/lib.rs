#![forbid(unsafe_code)]

//! # pm-proxy
//!
//! High-performance async Layer-4 TCP/UDP proxy and upstream client for Portmaster,
//! supporting SOCKS5, SOCKS4/4a, and HTTP CONNECT forwarding.

pub mod binding;
pub mod dial;
pub mod decider;
pub mod egress;
pub mod metrics;
pub mod tcp;
pub mod udp;
pub mod upstream;

pub use binding::LocalBinding;
pub use decider::{Decider, Decision, SharedDecider};
pub use egress::{EgressCache, EgressGuard};
pub use metrics::{MetricsSnapshot, ProxyMetrics};
pub use tcp::{TcpProxy, TcpProxyConfig};
pub use udp::{UdpProxy, UdpProxyConfig};
pub use upstream::{UpstreamProxy, UpstreamScheme};

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::{TcpListener, TcpStream, UdpSocket};

    #[tokio::test]
    async fn test_upstream_proxy_url_parsing() {
        let p = UpstreamProxy::parse("socks5://127.0.0.1:1080").unwrap();
        assert_eq!(p.scheme, UpstreamScheme::Socks5);
        assert_eq!(p.host, "127.0.0.1");
        assert_eq!(p.port, 1080);
        assert!(p.username.is_none());

        let p2 = UpstreamProxy::parse("http://alice:secret@proxy.lan:8080").unwrap();
        assert_eq!(p2.scheme, UpstreamScheme::Http);
        assert_eq!(p2.host, "proxy.lan");
        assert_eq!(p2.port, 8080);
        assert_eq!(p2.username.as_deref(), Some("alice"));
        assert_eq!(p2.password.as_deref(), Some("secret"));

        assert!(UpstreamProxy::parse("invalid://foo:80").is_err());
    }

    #[tokio::test]
    async fn test_tcp_proxy_direct_forwarding() {
        // Start echo server
        let echo_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let echo_addr = echo_listener.local_addr().unwrap();

        tokio::spawn(async move {
            if let Ok((mut stream, _)) = echo_listener.accept().await {
                let mut buf = [0u8; 1024];
                if let Ok(n) = stream.read(&mut buf).await {
                    let _ = stream.write_all(&buf[..n]).await;
                }
            }
        });

        // Start proxy
        let decider: SharedDecider = Arc::new(move |_, _| {
            Ok(Decision::new(echo_addr.ip(), echo_addr.port()))
        });

        let proxy = TcpProxy::bind("127.0.0.1:0".parse().unwrap(), decider, TcpProxyConfig::default())
            .await
            .unwrap();
        let proxy_addr = proxy.local_addr().unwrap();

        let proxy_handle = tokio::spawn(async move {
            let _ = proxy.run().await;
        });

        // Connect client to proxy
        let mut client = TcpStream::connect(proxy_addr).await.unwrap();
        client.write_all(b"hello rust proxy").await.unwrap();

        let mut resp = [0u8; 16];
        client.read_exact(&mut resp).await.unwrap();
        assert_eq!(&resp, b"hello rust proxy");

        proxy_handle.abort();
    }

    #[tokio::test]
    async fn test_udp_proxy_forwarding() {
        // Echo UDP server
        let echo_server = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let echo_addr = echo_server.local_addr().unwrap();

        tokio::spawn(async move {
            let mut buf = [0u8; 512];
            if let Ok((len, peer)) = echo_server.recv_from(&mut buf).await {
                let _ = echo_server.send_to(&buf[..len], peer).await;
            }
        });

        // UDP Proxy
        let decider: SharedDecider = Arc::new(move |_, _| {
            Ok(Decision::new(echo_addr.ip(), echo_addr.port()))
        });

        let proxy = UdpProxy::bind("127.0.0.1:0".parse().unwrap(), decider, UdpProxyConfig::default())
            .await
            .unwrap();
        let proxy_addr = proxy.local_addr().unwrap();

        let proxy_handle = tokio::spawn(async move {
            let _ = proxy.run().await;
        });

        let client = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        client.send_to(b"ping udp", proxy_addr).await.unwrap();

        let mut buf = [0u8; 512];
        let (n, _) = client.recv_from(&mut buf).await.unwrap();
        assert_eq!(&buf[..n], b"ping udp");

        proxy_handle.abort();
    }

    #[tokio::test]
    async fn test_tcp_proxy_upstream_socks5() {
        // 1. Echo target
        let echo_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let echo_addr = echo_listener.local_addr().unwrap();

        tokio::spawn(async move {
            if let Ok((mut stream, _)) = echo_listener.accept().await {
                let mut buf = [0u8; 1024];
                if let Ok(n) = stream.read(&mut buf).await {
                    let _ = stream.write_all(&buf[..n]).await;
                }
            }
        });

        // 2. Mock SOCKS5 server
        let socks5_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let socks5_addr = socks5_listener.local_addr().unwrap();

        tokio::spawn(async move {
            if let Ok((mut stream, _)) = socks5_listener.accept().await {
                let mut greeting = [0u8; 3];
                let _ = stream.read_exact(&mut greeting).await;
                let _ = stream.write_all(&[0x05, 0x00]).await;

                let mut req_hdr = [0u8; 4];
                let _ = stream.read_exact(&mut req_hdr).await;
                let mut dest_ip = [0u8; 4];
                let _ = stream.read_exact(&mut dest_ip).await;
                let mut dest_port = [0u8; 2];
                let _ = stream.read_exact(&mut dest_port).await;
                let _ = stream.write_all(&[0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0]).await;

                let port = u16::from_be_bytes(dest_port);
                let target = format!("{}.{}.{}.{}:{}", dest_ip[0], dest_ip[1], dest_ip[2], dest_ip[3], port);
                if let Ok(mut target_stream) = TcpStream::connect(target).await {
                    let _ = tokio::io::copy_bidirectional(&mut stream, &mut target_stream).await;
                }
            }
        });

        // 3. Upstream proxy config
        let proxy_url = format!("socks5://{}", socks5_addr);
        let upstream = UpstreamProxy::parse(&proxy_url).unwrap();

        let decider: SharedDecider = Arc::new(move |_, _| {
            let binding = LocalBinding::new().with_upstream(upstream.clone());
            Ok(Decision::new(echo_addr.ip(), echo_addr.port()).with_binding(binding))
        });

        let proxy = TcpProxy::bind("127.0.0.1:0".parse().unwrap(), decider, TcpProxyConfig::default())
            .await
            .unwrap();
        let proxy_addr = proxy.local_addr().unwrap();

        let proxy_handle = tokio::spawn(async move {
            let _ = proxy.run().await;
        });

        let mut client = TcpStream::connect(proxy_addr).await.unwrap();
        client.write_all(b"via socks5 proxy").await.unwrap();

        let mut resp = [0u8; 16];
        client.read_exact(&mut resp).await.unwrap();
        assert_eq!(&resp, b"via socks5 proxy");

        proxy_handle.abort();
    }
}
