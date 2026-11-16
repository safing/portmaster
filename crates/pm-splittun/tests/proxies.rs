//! Tests for the split tunnel proxy lifecycle (`splittun/proxies.go`).

use std::sync::Arc;
use std::time::Duration;

use pm_intel::{Entity, FilterLists, StaticLocations};
use pm_network::Connection;
use pm_splittun::{
    BestPhysicalDefaultInterfaces, InterfaceEnv, InterfaceInfo, Proxies, RequestQueue,
};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpSocket, TcpStream, UdpSocket};

struct FakeEnv {
    ipv6: bool,
}

impl InterfaceEnv for FakeEnv {
    fn ipv6_enabled(&self) -> bool {
        self.ipv6
    }

    fn get_interface(&self, _identifier: &str) -> Result<InterfaceInfo, String> {
        Ok(interface())
    }

    fn best_physical_default_interfaces(&self) -> Result<BestPhysicalDefaultInterfaces, String> {
        Ok(BestPhysicalDefaultInterfaces {
            for_ipv4: Some(interface()),
            for_ipv6: None,
        })
    }
}

fn interface() -> InterfaceInfo {
    InterfaceInfo {
        name: "lo".to_string(),
        ipv4: Some("127.0.0.1".parse().unwrap()),
        ipv6: Some("::1".parse().unwrap()),
    }
}

fn lists() -> Arc<FilterLists> {
    let databases = Arc::new(pm_database::Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register");
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .expect("inject");
    Arc::new(FilterLists::with_capacity(
        pm_intel::cache_interface(&databases),
        10,
        10,
        1,
        1,
        1,
    ))
}

fn connection(local: std::net::SocketAddr, dest: std::net::SocketAddr) -> Connection {
    let mut entity = Entity::new(lists(), Arc::new(StaticLocations::new()));
    entity.ip = Some(dest.ip());
    entity.port = dest.port();
    entity.domain = "example.com".to_string();
    entity.init(dest.port());

    Connection {
        ip_version: if local.is_ipv4() { 4 } else { 6 },
        local_ip: Some(local.ip()),
        local_port: local.port(),
        entity: Some(entity),
        ..Connection::default()
    }
}

#[tokio::test]
async fn tcp_proxy_serves_a_pending_request() {
    // Echo server.
    let echo = tokio::net::TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind echo");
    let echo_addr = echo.local_addr().expect("echo addr");
    tokio::spawn(async move {
        loop {
            let Ok((mut stream, _)) = echo.accept().await else {
                return;
            };
            tokio::spawn(async move {
                let mut buffer = [0u8; 1024];
                while let Ok(read) = stream.read(&mut buffer).await {
                    if read == 0 {
                        return;
                    }
                    let _ = stream.write_all(&buffer[..read]).await;
                }
            });
        }
    });

    let proxies = Proxies::new();
    let queue = Arc::new(RequestQueue::new());
    let env = FakeEnv { ipv6: false };
    proxies
        .start_on(0, Arc::clone(&queue), &env)
        .await
        .expect("start proxies");
    assert!(proxies.is_ready());
    let proxy_addr = proxies.tcp4_addr().expect("tcp4 address");

    // Bind the client first, so its local address is known before connecting.
    let socket = TcpSocket::new_v4().expect("socket");
    socket.bind("127.0.0.1:0".parse().unwrap()).expect("bind client");
    let client_local = socket.local_addr().expect("client addr");

    queue
        .await_request(&connection(client_local, echo_addr), "", "", &env)
        .expect("await request");
    assert_eq!(queue.len(), 1);

    let mut stream = socket.connect(proxy_addr).await.expect("connect proxy");
    stream.write_all(b"hello split tunnel").await.expect("write");
    let mut response = [0u8; 18];
    stream
        .read_exact(&mut response)
        .await
        .expect("read response");
    assert_eq!(&response, b"hello split tunnel");

    assert!(queue.is_empty(), "request was consumed");
    proxies.stop();
    assert!(!proxies.is_ready());
}

#[tokio::test]
async fn udp_proxy_serves_a_pending_request() {
    // Echo server.
    let echo = UdpSocket::bind("127.0.0.1:0").await.expect("bind echo");
    let echo_addr = echo.local_addr().expect("echo addr");
    tokio::spawn(async move {
        let mut buffer = [0u8; 512];
        while let Ok((read, peer)) = echo.recv_from(&mut buffer).await {
            let _ = echo.send_to(&buffer[..read], peer).await;
        }
    });

    let proxies = Proxies::new();
    let queue = Arc::new(RequestQueue::new());
    let env = FakeEnv { ipv6: false };
    proxies
        .start_on(0, Arc::clone(&queue), &env)
        .await
        .expect("start proxies");
    let proxy_addr = proxies.udp4_addr().expect("udp4 address");

    let client = UdpSocket::bind("127.0.0.1:0").await.expect("bind client");
    let client_local = client.local_addr().expect("client addr");

    queue
        .await_request(&connection(client_local, echo_addr), "", "", &env)
        .expect("await request");

    client
        .send_to(b"ping", proxy_addr)
        .await
        .expect("send datagram");
    let mut buffer = [0u8; 512];
    let (read, _) = tokio::time::timeout(Duration::from_secs(5), client.recv_from(&mut buffer))
        .await
        .expect("timeout")
        .expect("receive");
    assert_eq!(&buffer[..read], b"ping");

    assert!(queue.is_empty(), "request was consumed");
    proxies.stop();
}

#[tokio::test]
async fn unknown_connections_are_dropped() {
    let proxies = Proxies::new();
    let queue = Arc::new(RequestQueue::new());
    let env = FakeEnv { ipv6: false };
    proxies
        .start_on(0, Arc::clone(&queue), &env)
        .await
        .expect("start proxies");
    let proxy_addr = proxies.tcp4_addr().expect("tcp4 address");

    let mut stream = TcpStream::connect(proxy_addr).await.expect("connect");
    // The proxy closes the connection without a pending request.
    let _ = stream.write_all(b"nobody expects me").await;
    let mut buffer = [0u8; 16];
    let result = tokio::time::timeout(Duration::from_secs(5), stream.read(&mut buffer)).await;
    match result {
        Ok(Ok(0)) | Ok(Err(_)) => {}
        other => panic!("expected the connection to be closed, got {other:?}"),
    }

    proxies.stop();
}

#[tokio::test]
async fn ipv6_proxies_follow_the_environment() {
    let proxies = Proxies::new();
    let queue = Arc::new(RequestQueue::new());

    proxies
        .start_on(0, Arc::clone(&queue), &FakeEnv { ipv6: false })
        .await
        .expect("start without ipv6");
    assert!(proxies.tcp6_addr().is_none());
    assert!(proxies.udp6_addr().is_none());
    proxies.stop();

    proxies
        .start_on(0, queue, &FakeEnv { ipv6: true })
        .await
        .expect("start with ipv6");
    assert!(proxies.tcp6_addr().is_some());
    assert!(proxies.udp6_addr().is_some());
    proxies.stop();
}
