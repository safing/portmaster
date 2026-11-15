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

    let err = conn.query(Query::a("example.com")).await.unwrap_err();
    assert!(matches!(err, ResolveError::Truncated));
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

    // Trigger failure 6 times to exceed FailThreshold (5)
    for _ in 0..6 {
        let err = conn.query(Query::a("example.com")).await.unwrap_err();
        assert!(matches!(err, ResolveError::Timeout));
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
#[ignore]
async fn test_live_dot_and_doh_cloudflare() {
    // 1. Live DoT test: dot://cloudflare-dns.com?ip=1.1.1.1 for example.com A
    let dot_url = "dot://cloudflare-dns.com?ip=1.1.1.1";
    let (res_dot, _) = create_resolver(dot_url, SERVER_SOURCE_CONFIGURED).unwrap();
    let dot_resolver = res_dot.unwrap();
    let dot_conn = TcpResolver::new_dot(dot_resolver);

    match dot_conn.query(Query::a("example.com")).await {
        Ok(dot_reply) => {
            assert_eq!(dot_reply.response_code, ResponseCode::NoError);
            assert!(!dot_reply.answers.is_empty());
        }
        Err(e) => {
            eprintln!(
                "DoT query skipped/failed (port 853 is likely blocked on this network): {}",
                e
            );
        }
    }

    // 2. Live DoH test: doh://cloudflare-dns.com/dns-query for example.com A
    let doh_url = "doh://cloudflare-dns.com/dns-query";
    let (res_doh, _) = create_resolver(doh_url, SERVER_SOURCE_CONFIGURED).unwrap();
    let doh_resolver = res_doh.unwrap();
    let doh_conn = HttpsResolver::new(doh_resolver).unwrap();

    let doh_reply = doh_conn.query(Query::a("example.com")).await.unwrap();
    assert_eq!(doh_reply.response_code, ResponseCode::NoError);
    assert!(!doh_reply.answers.is_empty());
}
