use std::net::Ipv4Addr;
use std::sync::Arc;
use std::time::Duration;

use hickory_proto::op::{Message, MessageType, OpCode, ResponseCode};
use hickory_proto::rr::rdata::A;
use hickory_proto::rr::{RData, Record};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, UdpSocket};

use pm_resolver::*;

fn create_dns_response(query: &Message, answer_ip: Ipv4Addr) -> Message {
    let mut resp = Message::new(query.id, MessageType::Response, OpCode::Query);
    resp.metadata.recursion_desired = query.recursion_desired;
    resp.metadata.recursion_available = true;
    resp.metadata.response_code = ResponseCode::NoError;

    if let Some(q) = query.queries.first() {
        resp.add_query(q.clone());
        let record = Record::from_rdata(q.name.clone(), 60, RData::A(A(answer_ip)));
        resp.add_answer(record);
    }
    resp
}

#[tokio::test]
async fn test_plain_udp_transport_success() {
    let server_socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let server_addr = server_socket.local_addr().unwrap();

    // Spawn fake UDP DNS server
    tokio::spawn(async move {
        let mut buf = [0u8; 1024];
        let (len, client_addr) = server_socket.recv_from(&mut buf).await.unwrap();
        let query = Message::from_vec(&buf[..len]).unwrap();
        let resp = create_dns_response(&query, Ipv4Addr::new(93, 184, 216, 34));
        let resp_bytes = resp.to_vec().unwrap();
        server_socket.send_to(&resp_bytes, client_addr).await.unwrap();
    });

    let url = format!("dns://{}", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let resolver = res.unwrap();
    let conn = PlainResolver::new(resolver);

    let query = Query::a("example.com");
    let reply = conn.query(query).await.unwrap();

    assert_eq!(reply.response_code, ResponseCode::NoError);
    assert_eq!(reply.answers.len(), 1);
    if let RData::A(a) = &reply.answers[0].data {
        assert_eq!(a.0, Ipv4Addr::new(93, 184, 216, 34));
    } else {
        panic!("expected A record");
    }
}

#[tokio::test]
async fn test_plain_udp_transport_truncation() {
    let server_socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let server_addr = server_socket.local_addr().unwrap();

    tokio::spawn(async move {
        let mut buf = [0u8; 1024];
        let (len, client_addr) = server_socket.recv_from(&mut buf).await.unwrap();
        let query = Message::from_vec(&buf[..len]).unwrap();
        let mut resp = create_dns_response(&query, Ipv4Addr::new(93, 184, 216, 34));
        resp.metadata.truncation = true;
        let resp_bytes = resp.to_vec().unwrap();
        server_socket.send_to(&resp_bytes, client_addr).await.unwrap();
    });

    let url = format!("dns://{}", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = PlainResolver::new(res.unwrap());

    // Like miekg/dns, a truncated reply is returned as an answer, not an error.
    let reply = conn.query(Query::a("example.com")).await.unwrap();
    assert!(reply.truncation);
    assert_eq!(reply.answers.len(), 1);
}

#[tokio::test]
async fn test_plain_udp_transport_blocked_upstream() {
    let server_socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let server_addr = server_socket.local_addr().unwrap();

    tokio::spawn(async move {
        let mut buf = [0u8; 1024];
        let (len, client_addr) = server_socket.recv_from(&mut buf).await.unwrap();
        let query = Message::from_vec(&buf[..len]).unwrap();
        // Respond with 0.0.0.0 (blocked)
        let resp = create_dns_response(&query, Ipv4Addr::new(0, 0, 0, 0));
        let resp_bytes = resp.to_vec().unwrap();
        server_socket.send_to(&resp_bytes, client_addr).await.unwrap();
    });

    let url = format!("dns://{}?blockedif=zeroip", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = PlainResolver::new(res.unwrap());

    let err = conn.query(Query::a("example.com")).await.unwrap_err();
    assert!(matches!(err, ResolveError::BlockedUpstream(_)));
}

#[tokio::test]
async fn test_plain_udp_transport_timeout_and_failing() {
    let server_socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    let server_addr = server_socket.local_addr().unwrap();

    // Server stays alive and reads packets without answering
    tokio::spawn(async move {
        let mut buf = [0u8; 1024];
        loop {
            if server_socket.recv_from(&mut buf).await.is_err() {
                break;
            }
        }
    });

    let url = format!("dns://{}", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = PlainResolver::with_timeouts(
        res.unwrap(),
        Duration::from_millis(50),
        Duration::from_millis(50),
    );

    assert!(!conn.is_failing());

    // Transports never report failures themselves; the orchestrator does.
    for _ in 0..6 {
        let err = conn.query(Query::a("example.com")).await.unwrap_err();
        assert!(matches!(err, ResolveError::Timeout));
    }
    assert!(!conn.is_failing());

    // Explicit reports exceed FailThreshold (5).
    for _ in 0..6 {
        conn.report_failure();
    }
    assert!(conn.is_failing());
    conn.reset_failure();
    assert!(!conn.is_failing());
}

#[tokio::test]
async fn test_tcp_transport_success_and_pipelining() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let server_addr = listener.local_addr().unwrap();

    // Server receives 2 queries and replies out-of-order!
    tokio::spawn(async move {
        let (mut stream, _) = listener.accept().await.unwrap();

        // Read query 1
        let mut len_buf1 = [0u8; 2];
        stream.read_exact(&mut len_buf1).await.unwrap();
        let len1 = u16::from_be_bytes(len_buf1) as usize;
        let mut msg_buf1 = vec![0u8; len1];
        stream.read_exact(&mut msg_buf1).await.unwrap();
        let query1 = Message::from_vec(&msg_buf1).unwrap();

        // Read query 2
        let mut len_buf2 = [0u8; 2];
        stream.read_exact(&mut len_buf2).await.unwrap();
        let len2 = u16::from_be_bytes(len_buf2) as usize;
        let mut msg_buf2 = vec![0u8; len2];
        stream.read_exact(&mut msg_buf2).await.unwrap();
        let query2 = Message::from_vec(&msg_buf2).unwrap();

        // Send response for query 2 FIRST
        let resp2 = create_dns_response(&query2, Ipv4Addr::new(2, 2, 2, 2));
        let wire2 = resp2.to_vec().unwrap();
        let len2 = wire2.len() as u16;
        stream.write_all(&len2.to_be_bytes()).await.unwrap();
        stream.write_all(&wire2).await.unwrap();

        // Brief delay, then send response for query 1 SECOND
        tokio::time::sleep(Duration::from_millis(10)).await;
        let resp1 = create_dns_response(&query1, Ipv4Addr::new(1, 1, 1, 1));
        let wire1 = resp1.to_vec().unwrap();
        let len1 = wire1.len() as u16;
        stream.write_all(&len1.to_be_bytes()).await.unwrap();
        stream.write_all(&wire1).await.unwrap();
    });

    let url = format!("tcp://{}", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = Arc::new(TcpResolver::new_tcp(res.unwrap()));

    let conn1 = conn.clone();
    let task1 = tokio::spawn(async move {
        conn1.query(Query::a("one.example.com")).await.unwrap()
    });

    let conn2 = conn.clone();
    let task2 = tokio::spawn(async move {
        conn2.query(Query::a("two.example.com")).await.unwrap()
    });

    let (reply1, reply2) = tokio::try_join!(task1, task2).unwrap();

    assert_eq!(reply1.answers.len(), 1);
    if let RData::A(a) = &reply1.answers[0].data {
        assert_eq!(a.0, Ipv4Addr::new(1, 1, 1, 1));
    } else {
        panic!("expected A record 1.1.1.1");
    }

    assert_eq!(reply2.answers.len(), 1);
    if let RData::A(a) = &reply2.answers[0].data {
        assert_eq!(a.0, Ipv4Addr::new(2, 2, 2, 2));
    } else {
        panic!("expected A record 2.2.2.2");
    }
}

#[tokio::test]
async fn test_tcp_transport_timeout() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let server_addr = listener.local_addr().unwrap();

    tokio::spawn(async move {
        let (mut stream, _) = listener.accept().await.unwrap();
        let mut buf = [0u8; 100];
        let _ = stream.read(&mut buf).await;
        // sleep without responding
        tokio::time::sleep(Duration::from_secs(1)).await;
    });

    let url = format!("tcp://{}", server_addr);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::with_timeouts(
        res.unwrap(),
        false,
        Duration::from_millis(50),
        Duration::from_millis(50),
    );

    let err = conn.query(Query::a("example.com")).await.unwrap_err();
    assert!(matches!(err, ResolveError::Timeout));
}

#[tokio::test]
async fn test_tcp_truncated_reply_is_returned() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let server_addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let (mut stream, _) = listener.accept().await.unwrap();
        let query = read_framed(&mut stream).await.unwrap();
        let mut resp = create_dns_response(&query, Ipv4Addr::new(1, 2, 3, 4));
        resp.metadata.truncation = true;
        write_framed(&mut stream, &resp).await;
        tokio::time::sleep(Duration::from_millis(200)).await;
    });

    let (res, _) = create_resolver(&format!("tcp://{}", server_addr), SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::new_tcp(res.unwrap());
    let reply = conn.query(Query::a("example.com")).await.unwrap();
    assert!(reply.truncation);
    assert!(!conn.is_failing());
}

async fn read_framed<S: AsyncReadExt + Unpin>(stream: &mut S) -> Option<Message> {
    let mut len_buf = [0u8; 2];
    stream.read_exact(&mut len_buf).await.ok()?;
    let mut buf = vec![0u8; u16::from_be_bytes(len_buf) as usize];
    stream.read_exact(&mut buf).await.ok()?;
    Message::from_vec(&buf).ok()
}

async fn write_framed<S: AsyncWriteExt + Unpin>(stream: &mut S, msg: &Message) {
    let wire = msg.to_vec().unwrap();
    stream.write_all(&(wire.len() as u16).to_be_bytes()).await.unwrap();
    stream.write_all(&wire).await.unwrap();
}

/// Runs `body` on a current-thread runtime in a separate thread. A task that
/// busy-loops without yielding starves that runtime, so the watchdog fails the test.
fn run_with_watchdog<F>(body: F)
where
    F: std::future::Future<Output = ()> + Send + 'static,
{
    let (done_tx, done_rx) = std::sync::mpsc::channel();
    std::thread::spawn(move || {
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        rt.block_on(body);
        let _ = done_tx.send(());
    });
    done_rx
        .recv_timeout(Duration::from_secs(10))
        .expect("runtime starved: a task is busy-looping");
}

/// CPU time (user+system, clock ticks of 10ms) of the calling thread. Linux only.
fn thread_cpu_ticks() -> u64 {
    let stat = std::fs::read_to_string("/proc/thread-self/stat").unwrap();
    let after_comm = &stat[stat.rfind(')').unwrap() + 2..];
    let fields: Vec<&str> = after_comm.split_whitespace().collect();
    // utime and stime are fields 14 and 15; after_comm starts at field 3.
    fields[11].parse::<u64>().unwrap() + fields[12].parse::<u64>().unwrap()
}

#[test]
fn test_tcp_ttl_expiry_with_inflight_query_does_not_spin() {
    run_with_watchdog(async {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let server_addr = listener.local_addr().unwrap();
        // Accepts and reads, never answers.
        tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            while read_framed(&mut stream).await.is_some() {}
        });

        let (res, _) =
            create_resolver(&format!("tcp://{}", server_addr), SERVER_SOURCE_CONFIGURED).unwrap();
        let conn = TcpResolver::with_timeouts(
            res.unwrap(),
            false,
            Duration::from_millis(500),
            Duration::from_millis(300),
        )
        .with_client_ttl(Duration::from_millis(50));

        // The TTL fires (50ms) while the query is in flight (300ms timeout).
        // A writer that keeps polling the expired timer burns CPU (tokio's
        // cooperative budget keeps the runtime responsive, so measure CPU time).
        let conn = Arc::new(conn);
        let c = conn.clone();
        let query = tokio::spawn(async move { c.query(Query::a("example.com")).await });
        tokio::time::sleep(Duration::from_millis(100)).await;
        let before = thread_cpu_ticks();
        tokio::time::sleep(Duration::from_millis(150)).await;
        let burned = thread_cpu_ticks() - before;
        assert!(burned < 5, "writer spins after TTL: {burned} ticks of CPU in 150ms idle");

        let err = query.await.unwrap().unwrap_err();
        assert!(matches!(err, ResolveError::Timeout));
        // The timed-out query must not stay registered.
        assert_eq!(conn.in_flight_count(), 0);
    });
}

#[tokio::test]
async fn test_tcp_timed_out_query_is_removed_from_in_flight() {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let server_addr = listener.local_addr().unwrap();
    tokio::spawn(async move {
        let (mut stream, _) = listener.accept().await.unwrap();
        while read_framed(&mut stream).await.is_some() {}
    });

    let (res, _) = create_resolver(&format!("tcp://{}", server_addr), SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::with_timeouts(
        res.unwrap(),
        false,
        Duration::from_millis(500),
        Duration::from_millis(100),
    );
    for _ in 0..3 {
        let err = conn.query(Query::a("example.com")).await.unwrap_err();
        assert!(matches!(err, ResolveError::Timeout));
        assert_eq!(conn.in_flight_count(), 0);
    }
}

#[tokio::test]
async fn test_tcp_idle_connection_is_recycled_after_ttl() {
    use std::sync::atomic::{AtomicUsize, Ordering};

    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let server_addr = listener.local_addr().unwrap();
    let accepted = Arc::new(AtomicUsize::new(0));
    let closed_by_client = Arc::new(AtomicUsize::new(0));
    {
        let accepted = accepted.clone();
        let closed = closed_by_client.clone();
        tokio::spawn(async move {
            loop {
                let (mut stream, _) = listener.accept().await.unwrap();
                accepted.fetch_add(1, Ordering::SeqCst);
                let closed = closed.clone();
                tokio::spawn(async move {
                    while let Some(q) = read_framed(&mut stream).await {
                        let resp = create_dns_response(&q, Ipv4Addr::new(9, 9, 9, 9));
                        write_framed(&mut stream, &resp).await;
                    }
                    closed.fetch_add(1, Ordering::SeqCst);
                });
            }
        });
    }

    let (res, _) = create_resolver(&format!("tcp://{}", server_addr), SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::new_tcp(res.unwrap()).with_client_ttl(Duration::from_millis(100));

    conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(accepted.load(Ordering::SeqCst), 1);
    // Reuse within the TTL.
    conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(accepted.load(Ordering::SeqCst), 1);

    tokio::time::sleep(Duration::from_millis(400)).await;
    assert_eq!(closed_by_client.load(Ordering::SeqCst), 1, "idle connection not closed");

    conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(accepted.load(Ordering::SeqCst), 2);
}

struct TestPki {
    cert_der: tokio_rustls::rustls::pki_types::CertificateDer<'static>,
    key_der: Vec<u8>,
}

fn test_pki(domain: &str) -> TestPki {
    let key = rcgen::KeyPair::generate().unwrap();
    let params = rcgen::CertificateParams::new(vec![domain.to_string()]).unwrap();
    let cert = params.self_signed(&key).unwrap();
    TestPki {
        cert_der: cert.der().clone(),
        key_der: key.serialize_der(),
    }
}

fn server_tls(pki: &TestPki) -> tokio_rustls::TlsAcceptor {
    use tokio_rustls::rustls::{self, pki_types::PrivatePkcs8KeyDer, ServerConfig};
    let cfg = ServerConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
        .with_safe_default_protocol_versions()
        .unwrap()
        .with_no_client_auth()
        .with_single_cert(
            vec![pki.cert_der.clone()],
            PrivatePkcs8KeyDer::from(pki.key_der.clone()).into(),
        )
        .unwrap();
    tokio_rustls::TlsAcceptor::from(Arc::new(cfg))
}

fn client_tls(pki: &TestPki) -> Arc<tokio_rustls::rustls::ClientConfig> {
    use tokio_rustls::rustls::{self, ClientConfig, RootCertStore};
    let mut roots = RootCertStore::empty();
    roots.add(pki.cert_der.clone()).unwrap();
    Arc::new(
        ClientConfig::builder_with_provider(Arc::new(rustls::crypto::ring::default_provider()))
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_root_certificates(roots)
            .with_no_client_auth(),
    )
}

#[tokio::test]
async fn test_dot_local_server_sni_and_verification() {
    let pki = test_pki("dns.example.test");
    let acceptor = server_tls(&pki);
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    let (sni_tx, mut sni_rx) = tokio::sync::mpsc::unbounded_channel();
    tokio::spawn(async move {
        loop {
            let (tcp, _) = listener.accept().await.unwrap();
            let acceptor = acceptor.clone();
            let sni_tx = sni_tx.clone();
            tokio::spawn(async move {
                let Ok(mut tls) = acceptor.accept(tcp).await else { return };
                let _ = sni_tx.send(tls.get_ref().1.server_name().map(str::to_string));
                while let Some(q) = read_framed(&mut tls).await {
                    let resp = create_dns_response(&q, Ipv4Addr::new(7, 7, 7, 7));
                    write_framed(&mut tls, &resp).await;
                }
            });
        }
    });

    // SNI and verification use the verify domain while connecting to the IP.
    let url = format!("dot://127.0.0.1:{}?verify=dns.example.test", port);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::new_dot(res.unwrap()).with_tls_config(client_tls(&pki));
    let reply = conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(reply.answers.len(), 1);
    assert_eq!(sni_rx.recv().await.unwrap().as_deref(), Some("dns.example.test"));

    // A certificate for another name is rejected.
    let url = format!("dot://127.0.0.1:{}?verify=other.example.test", port);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = TcpResolver::new_dot(res.unwrap()).with_tls_config(client_tls(&pki));
    let err = conn.query(Query::a("example.com")).await.unwrap_err();
    assert!(matches!(err, ResolveError::Tls(_)), "{err:?}");
}

#[tokio::test]
async fn test_doh_local_server_sni_path_and_no_block_detection() {
    use base64::prelude::*;

    let pki = test_pki("dns.example.test");
    let acceptor = server_tls(&pki);
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    let (seen_tx, mut seen_rx) = tokio::sync::mpsc::unbounded_channel();
    tokio::spawn(async move {
        loop {
            let (tcp, _) = listener.accept().await.unwrap();
            let acceptor = acceptor.clone();
            let seen_tx = seen_tx.clone();
            tokio::spawn(async move {
                let Ok(mut tls) = acceptor.accept(tcp).await else { return };
                let sni = tls.get_ref().1.server_name().map(str::to_string);
                let mut req = Vec::new();
                let mut chunk = [0u8; 1024];
                while !req.windows(4).any(|w| w == b"\r\n\r\n") {
                    let n = tls.read(&mut chunk).await.unwrap_or(0);
                    if n == 0 {
                        return;
                    }
                    req.extend_from_slice(&chunk[..n]);
                }
                let head = String::from_utf8_lossy(&req).to_string();
                let target = head.split_whitespace().nth(1).unwrap().to_string();
                let (path, query) = target.split_once('?').unwrap();
                let b64 = query.strip_prefix("dns=").unwrap();
                let q = Message::from_vec(&BASE64_URL_SAFE_NO_PAD.decode(b64).unwrap()).unwrap();
                let _ = seen_tx.send((sni, path.to_string()));
                // 0.0.0.0 would be "blocked" for the plain/TCP transports; DoH must not care.
                let body = create_dns_response(&q, Ipv4Addr::new(0, 0, 0, 0)).to_vec().unwrap();
                let resp = format!(
                    "HTTP/1.1 200 OK\r\ncontent-type: application/dns-message\r\ncontent-length: {}\r\nconnection: close\r\n\r\n",
                    body.len()
                );
                tls.write_all(resp.as_bytes()).await.unwrap();
                tls.write_all(&body).await.unwrap();
                let _ = tls.shutdown().await;
            });
        }
    });

    // Domain for SNI/verification, ip= for the connection target, path as given.
    let url = format!("https://dns.example.test:{}/custom/path?ip=127.0.0.1", port);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let resolver = res.unwrap();
    assert_eq!(resolver.server_address, format!("127.0.0.1:{}", port));
    let conn = HttpsResolver::with_extra_root_der(
        resolver,
        Duration::from_secs(5),
        false,
        &pki.cert_der,
    )
    .unwrap();

    let reply = conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(reply.answers.len(), 1);
    let (sni, path) = seen_rx.recv().await.unwrap();
    assert_eq!(sni.as_deref(), Some("dns.example.test"));
    assert_eq!(path, "/custom/path");
    assert!(!conn.is_failing());

    // No path configured: request path is "/" (Go uses Resolver.Path unchanged).
    let url = format!("https://dns.example.test:{}?ip=127.0.0.1", port);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = HttpsResolver::with_extra_root_der(
        res.unwrap(),
        Duration::from_secs(5),
        false,
        &pki.cert_der,
    )
    .unwrap();
    conn.query(Query::a("example.com")).await.unwrap();
    let (_, path) = seen_rx.recv().await.unwrap();
    assert_eq!(path, "/");
}

#[tokio::test]
async fn test_doh_certificate_for_other_domain_is_rejected() {
    let pki = test_pki("dns.example.test");
    let acceptor = server_tls(&pki);
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    tokio::spawn(async move {
        loop {
            let (tcp, _) = listener.accept().await.unwrap();
            let acceptor = acceptor.clone();
            tokio::spawn(async move {
                let _ = acceptor.accept(tcp).await;
            });
        }
    });

    // Connects to 127.0.0.1 but must verify "other.example.test".
    let url = format!("doh://127.0.0.1:{}/q?verify=other.example.test", port);
    let (res, _) = create_resolver(&url, SERVER_SOURCE_CONFIGURED).unwrap();
    let conn = HttpsResolver::with_extra_root_der(
        res.unwrap(),
        Duration::from_secs(5),
        false,
        &pki.cert_der,
    )
    .unwrap();
    let err = conn.query(Query::a("example.com")).await.unwrap_err();
    assert!(matches!(err, ResolveError::Http(_)), "{err:?}");
}

/// Live test: `cargo test -p pm-resolver -- --ignored`. Needs outbound 853 and 443.
#[tokio::test]
#[ignore]
async fn test_live_dot_and_doh_cloudflare() {
    let (res_dot, _) = create_resolver("dot://cloudflare-dns.com?ip=1.1.1.1", SERVER_SOURCE_CONFIGURED).unwrap();
    let dot_conn = TcpResolver::new_dot(res_dot.unwrap());
    let dot_reply = dot_conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(dot_reply.response_code, ResponseCode::NoError);
    assert!(!dot_reply.answers.is_empty());

    let (res_doh, _) = create_resolver("doh://cloudflare-dns.com/dns-query", SERVER_SOURCE_CONFIGURED).unwrap();
    let doh_conn = HttpsResolver::new(res_doh.unwrap()).unwrap();
    let doh_reply = doh_conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(doh_reply.response_code, ResponseCode::NoError);
    assert!(!doh_reply.answers.is_empty());
}
