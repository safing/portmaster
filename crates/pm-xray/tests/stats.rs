//! End-to-end tests of the gRPC StatsService client against a mock HTTP/2
//! (h2c) server that emits real protobuf frames and gRPC statuses.

use std::net::SocketAddr;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::Duration;

use bytes::Bytes;
use http::{HeaderMap, HeaderValue};
use pm_xray::{
    GrpcStats, GrpcStatus, NoopTrafficStats, StatsError, StatsService, GET_STATS_PATH,
    QUERY_STATS_PATH,
};
use tokio::net::{TcpListener, TcpStream};

#[derive(Clone)]
enum Reply {
    Data(Vec<u8>),
    /// Behaves like Xray's `QueryStats`: filters counters by the requested pattern.
    QueryStats(Vec<(&'static str, i64)>),
    GrpcError { code: u8, message: &'static str },
    TrailerError { data: Vec<u8>, code: u8, message: &'static str },
    Slow { delay: Duration, data: Vec<u8> },
}

#[derive(Default, Clone)]
struct Captured {
    paths: Vec<String>,
    methods: Vec<String>,
    requests: Vec<Vec<u8>>,
    content_types: Vec<String>,
}

struct MockServer {
    addr: SocketAddr,
    hits: Arc<AtomicUsize>,
    captured: Arc<Mutex<Captured>>,
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
}

impl MockServer {
    fn start(reply: Reply) -> Self {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let addr = listener.local_addr().unwrap();
        let stop = Arc::new(AtomicBool::new(false));
        let hits = Arc::new(AtomicUsize::new(0));
        let captured = Arc::new(Mutex::new(Captured::default()));
        let thread_stop = Arc::clone(&stop);
        let thread_hits = Arc::clone(&hits);
        let thread_captured = Arc::clone(&captured);

        let thread = thread::spawn(move || {
            let runtime = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .unwrap();
            runtime.block_on(async move {
                let listener = TcpListener::from_std(listener).unwrap();
                while !thread_stop.load(Ordering::Relaxed) {
                    let accepted =
                        tokio::time::timeout(Duration::from_millis(50), listener.accept()).await;
                    let Ok(Ok((socket, _))) = accepted else {
                        continue;
                    };
                    let reply = reply.clone();
                    let hits = Arc::clone(&thread_hits);
                    let captured = Arc::clone(&thread_captured);
                    tokio::spawn(async move {
                        serve(socket, reply, hits, captured).await;
                    });
                }
            });
        });

        Self {
            addr,
            hits,
            captured,
            stop,
            thread: Some(thread),
        }
    }

    fn client(&self, timeout: Duration) -> GrpcStats {
        GrpcStats::with_addr(self.addr).with_timeout(timeout)
    }

    fn captured(&self) -> Captured {
        self.captured.lock().unwrap().to_owned()
    }
}

impl Drop for MockServer {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}

async fn serve(socket: TcpStream, reply: Reply, hits: Arc<AtomicUsize>, captured: Arc<Mutex<Captured>>) {
    let mut connection = match h2::server::handshake(socket).await {
        Ok(connection) => connection,
        Err(_) => return,
    };
    while let Some(accepted) = connection.accept().await {
        let Ok((request, mut respond)) = accepted else {
            return;
        };
        hits.fetch_add(1, Ordering::Relaxed);
        {
            let mut captured = captured.lock().unwrap();
            captured.paths.push(request.uri().path().to_owned());
            captured.methods.push(request.method().to_string());
            captured.content_types.push(
                request
                    .headers()
                    .get("content-type")
                    .and_then(|value| value.to_str().ok())
                    .unwrap_or_default()
                    .to_owned(),
            );
        }
        let mut body = request.into_body();
        let mut message = Vec::new();
        while let Some(chunk) = body.data().await {
            if let Ok(chunk) = chunk {
                message.extend_from_slice(&chunk);
            }
        }
        captured.lock().unwrap().requests.push(message);

        let headers = || {
            let mut response = http::Response::builder()
                .status(200)
                .header("content-type", "application/grpc")
                .body(())
                .unwrap();
            response
                .headers_mut()
                .insert("grpc-status", HeaderValue::from_static("0"));
            response
        };

        match reply.clone() {
            Reply::GrpcError { code, message } => {
                let mut response = headers();
                response
                    .headers_mut()
                    .insert("grpc-status", HeaderValue::from_str(&code.to_string()).unwrap());
                response
                    .headers_mut()
                    .insert("grpc-message", HeaderValue::from_str(message).unwrap());
                let _ = respond.send_response(response, true);
            }
            Reply::Slow { delay, data } => {
                tokio::time::sleep(delay).await;
                send_message(&mut respond, headers(), data);
            }
            Reply::Data(data) => send_message(&mut respond, headers(), data),
            Reply::QueryStats(counters) => {
                let pattern = request_string(&captured.lock().unwrap().requests);
                let filtered: Vec<_> = counters
                    .into_iter()
                    .filter(|(name, _)| name.contains(&pattern))
                    .collect();
                send_message(&mut respond, headers(), query_stats_response(&filtered));
            }
            Reply::TrailerError {
                data,
                code,
                message,
            } => {
                let response = headers();
                if let Ok(mut stream) = respond.send_response(response, false) {
                    let _ = stream.send_data(Bytes::from(data), false);
                    let mut trailers = HeaderMap::new();
                    trailers.insert(
                        "grpc-status",
                        HeaderValue::from_str(&code.to_string()).unwrap(),
                    );
                    trailers.insert("grpc-message", HeaderValue::from_str(message).unwrap());
                    let _ = stream.send_trailers(trailers);
                }
            }
        }
    }
}

fn send_message(
    respond: &mut h2::server::SendResponse<Bytes>,
    response: http::Response<()>,
    payload: Vec<u8>,
) {
    let Ok(mut stream) = respond.send_response(response, false) else {
        return;
    };
    let data: Bytes = Bytes::from(payload);
    let _ = stream.send_data(data, false);
    let mut trailers = HeaderMap::new();
    trailers.insert("grpc-status", HeaderValue::from_static("0"));
    let _ = stream.send_trailers(trailers);
}

fn request_string(requests: &[Vec<u8>]) -> String {
    let Some(framed) = requests.last() else {
        return String::new();
    };
    let message = framed.get(5..).unwrap_or(framed);
    if message.len() >= 2 && message[0] == 0x0a {
        let length = message[1] as usize;
        if message.len() >= 2 + length {
            return String::from_utf8_lossy(&message[2..2 + length]).into_owned();
        }
    }
    String::new()
}

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value as u8) | 0x80);
        value >>= 7;
    }
    out.push(value as u8);
}

fn frame(message: Vec<u8>) -> Vec<u8> {
    let mut out = vec![0];
    out.extend_from_slice(&(message.len() as u32).to_be_bytes());
    out.extend_from_slice(&message);
    out
}

fn stat(name: &str, value: i64) -> Vec<u8> {
    let mut out = Vec::new();
    put_varint(&mut out, (1 << 3) | 2);
    put_varint(&mut out, name.len() as u64);
    out.extend_from_slice(name.as_bytes());
    put_varint(&mut out, 2 << 3);
    put_varint(&mut out, value as u64);
    out
}

fn get_stats_response(name: &str, value: i64) -> Vec<u8> {
    let nested = stat(name, value);
    let mut out = Vec::new();
    put_varint(&mut out, (1 << 3) | 2);
    put_varint(&mut out, nested.len() as u64);
    out.extend_from_slice(&nested);
    frame(out)
}

fn query_stats_response(counters: &[(&str, i64)]) -> Vec<u8> {
    let mut out = Vec::new();
    for (name, value) in counters {
        let nested = stat(name, *value);
        put_varint(&mut out, (1 << 3) | 2);
        put_varint(&mut out, nested.len() as u64);
        out.extend_from_slice(&nested);
    }
    frame(out)
}

#[test]
fn get_stats_round_trip_over_h2() {
    let name = "outbound>>>proxy>>>traffic>>>uplink";
    let server = MockServer::start(Reply::Data(get_stats_response(name, 4096)));
    let client = server.client(Duration::from_secs(2));

    let counter = client.get_stats("proxy").unwrap();
    assert_eq!(counter.name, name);
    assert_eq!(counter.value, 4096);
    assert_eq!(server.hits.load(Ordering::Relaxed), 1);

    let captured = server.captured();
    assert_eq!(captured.paths, [GET_STATS_PATH]);
    assert_eq!(captured.methods, ["POST"]);
    assert_eq!(captured.content_types, ["application/grpc"]);
    // GetStatsRequest { name: "proxy", reset: false } inside a gRPC frame.
    assert_eq!(captured.requests, [frame(b"\x0a\x05proxy".to_vec())]);
}

#[test]
fn query_stats_round_trip_over_h2() {
    let counters = [
        ("outbound>>>proxy>>>traffic>>>uplink", 11i64),
        ("outbound>>>proxy>>>traffic>>>downlink", 22i64),
    ];
    let server = MockServer::start(Reply::Data(query_stats_response(&counters)));
    let client = server.client(Duration::from_secs(2));

    let result = client.query_stats(">>>traffic>>>").unwrap();
    assert_eq!(result.len(), 2);
    assert_eq!(result[0].name, counters[0].0);
    assert_eq!(result[1].value, 22);

    let captured = server.captured();
    assert_eq!(captured.paths, [QUERY_STATS_PATH]);
    let mut expected = vec![0x0a, 13];
    expected.extend_from_slice(b">>>traffic>>>");
    assert_eq!(captured.requests, [frame(expected)]);
}

#[test]
fn grpc_status_is_reported_from_headers() {
    let server = MockServer::start(Reply::GrpcError {
        code: 5,
        message: "proxy not found.",
    });
    let error = server
        .client(Duration::from_secs(2))
        .get_stats("proxy")
        .unwrap_err();
    match error {
        StatsError::Grpc { status, message } => {
            assert_eq!(status, GrpcStatus::NotFound);
            assert_eq!(message, "proxy not found.");
        }
        other => panic!("expected gRPC status error, got {other:?}"),
    }
}

#[test]
fn grpc_status_in_trailers_overrides_payload() {
    let server = MockServer::start(Reply::TrailerError {
        data: get_stats_response("proxy", 7),
        code: 5,
        message: "counter reset",
    });
    let error = server
        .client(Duration::from_secs(2))
        .get_stats("proxy")
        .unwrap_err();
    assert!(matches!(
        error,
        StatsError::Grpc {
            status: GrpcStatus::NotFound,
            ..
        }
    ));
}

#[test]
fn malformed_frames_are_rejected() {
    let server = MockServer::start(Reply::Data(vec![1, 0, 0, 0, 0]));
    let error = server
        .client(Duration::from_secs(2))
        .get_stats("proxy")
        .unwrap_err();
    assert!(matches!(error, StatsError::Transport(_)), "{error:?}");
}

#[test]
fn rpc_times_out() {
    let server = MockServer::start(Reply::Slow {
        delay: Duration::from_millis(400),
        data: get_stats_response("proxy", 7),
    });
    let error = server
        .client(Duration::from_millis(60))
        .get_stats("proxy")
        .unwrap_err();
    assert!(matches!(error, StatsError::Timeout(_)), "{error:?}");
}

#[test]
fn unreachable_service_and_noop_fallback() {
    let probe = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = probe.local_addr().unwrap();
    drop(probe);

    let error = GrpcStats::with_addr(addr)
        .with_timeout(Duration::from_millis(250))
        .get_stats("proxy")
        .unwrap_err();
    assert!(matches!(error, StatsError::Transport(_)), "{error:?}");

    let noop = NoopTrafficStats;
    assert!(!noop.available());
    assert_eq!(noop.get_stats("proxy").unwrap().value, 0);
    assert!(noop.query_stats(">>>traffic>>>").unwrap().is_empty());
    assert_eq!(noop.traffic("proxy").unwrap(), Default::default());
}

#[test]
fn traffic_sums_uplink_and_downlink() {
    let server = MockServer::start(Reply::QueryStats(vec![
        ("outbound>>>proxy>>>traffic>>>uplink", 10),
        ("outbound>>>proxy>>>traffic>>>downlink", 20),
        ("inbound>>>socks-in>>>traffic>>>uplink", 999),
    ]));
    let snapshot = server
        .client(Duration::from_secs(2))
        .traffic("proxy")
        .unwrap();
    assert_eq!(snapshot.uplink, 10);
    assert_eq!(snapshot.downlink, 20);
    let mut expected = vec![0x0a, 18];
    expected.extend_from_slice(b"proxy>>>traffic>>>");
    assert_eq!(server.captured().requests, [frame(expected)]);
}
