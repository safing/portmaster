//! Real `xray.app.stats.command.StatsService` client.
//!
//! Xray serves this service as stateless gRPC over cleartext HTTP/2: a unary
//! POST carries a single length-prefixed protobuf frame, the reply is another
//! frame followed by `grpc-status` in trailers (errors may put the status in
//! the initial headers as a trailers-only response). Only `GetStats` and
//! `QueryStats` are implemented — every other StatsService RPC is out of
//! scope and answers `Unimplemented` on the server side anyway.
//!
//! The wire codec below is a deliberately small protobuf reader/writer so the
//! GPL-3.0 core does not need to depend on `prost`/`tonic`. The HTTP/2
//! transport is injectable ([`GrpcTransport`]); the default implementation
//! ([`H2Transport`]) speaks h2c with prior knowledge. [`NoopTrafficStats`]
//! remains as an explicit fallback when no API port is configured.

use std::fmt;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use bytes::Bytes;
use http::{HeaderMap, Method, Request};

/// Default port of the Xray gRPC API inbound (`api` in the Xray config).
pub const DEFAULT_STATS_PORT: u16 = 62789;
/// Loopback address of the Xray API.
pub const DEFAULT_STATS_ADDR: ([u8; 4], u16) = ([127, 0, 0, 1], DEFAULT_STATS_PORT);
/// Full path of the `GetStats` RPC.
pub const GET_STATS_PATH: &str = "/xray.app.stats.command.StatsService/GetStats";
/// Full path of the `QueryStats` RPC.
pub const QUERY_STATS_PATH: &str = "/xray.app.stats.command.StatsService/QueryStats";
/// Default deadline for one unary RPC.
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(2);

/// One `Stat` message: a counter name and its signed value (protobuf `int64`).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
pub struct TrafficCounter {
    pub name: String,
    pub value: i64,
}

/// Traffic counters of one outbound, in bytes (non-negative totals).
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct TrafficSnapshot {
    pub uplink: u64,
    pub downlink: u64,
}

/// Canonical gRPC status codes (subset used by Xray plus the full code range).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum GrpcStatus {
    Ok,
    Cancelled,
    Unknown,
    InvalidArgument,
    DeadlineExceeded,
    NotFound,
    AlreadyExists,
    PermissionDenied,
    ResourceExhausted,
    FailedPrecondition,
    Aborted,
    OutOfRange,
    Unimplemented,
    Internal,
    Unavailable,
    DataLoss,
    Unauthenticated,
    /// A code outside the canonical 0..16 range.
    Code(u8),
}

impl GrpcStatus {
    pub fn from_code(code: u8) -> Self {
        match code {
            0 => Self::Ok,
            1 => Self::Cancelled,
            2 => Self::Unknown,
            3 => Self::InvalidArgument,
            4 => Self::DeadlineExceeded,
            5 => Self::NotFound,
            6 => Self::AlreadyExists,
            7 => Self::PermissionDenied,
            8 => Self::ResourceExhausted,
            9 => Self::FailedPrecondition,
            10 => Self::Aborted,
            11 => Self::OutOfRange,
            12 => Self::Unimplemented,
            13 => Self::Internal,
            14 => Self::Unavailable,
            15 => Self::DataLoss,
            16 => Self::Unauthenticated,
            other => Self::Code(other),
        }
    }

    pub fn code(self) -> u8 {
        match self {
            Self::Ok => 0,
            Self::Cancelled => 1,
            Self::Unknown => 2,
            Self::InvalidArgument => 3,
            Self::DeadlineExceeded => 4,
            Self::NotFound => 5,
            Self::AlreadyExists => 6,
            Self::PermissionDenied => 7,
            Self::ResourceExhausted => 8,
            Self::FailedPrecondition => 9,
            Self::Aborted => 10,
            Self::OutOfRange => 11,
            Self::Unimplemented => 12,
            Self::Internal => 13,
            Self::Unavailable => 14,
            Self::DataLoss => 15,
            Self::Unauthenticated => 16,
            Self::Code(other) => other,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Self::Ok => "OK",
            Self::Cancelled => "CANCELLED",
            Self::Unknown => "UNKNOWN",
            Self::InvalidArgument => "INVALID_ARGUMENT",
            Self::DeadlineExceeded => "DEADLINE_EXCEEDED",
            Self::NotFound => "NOT_FOUND",
            Self::AlreadyExists => "ALREADY_EXISTS",
            Self::PermissionDenied => "PERMISSION_DENIED",
            Self::ResourceExhausted => "RESOURCE_EXHAUSTED",
            Self::FailedPrecondition => "FAILED_PRECONDITION",
            Self::Aborted => "ABORTED",
            Self::OutOfRange => "OUT_OF_RANGE",
            Self::Unimplemented => "UNIMPLEMENTED",
            Self::Internal => "INTERNAL",
            Self::Unavailable => "UNAVAILABLE",
            Self::DataLoss => "DATA_LOSS",
            Self::Unauthenticated => "UNAUTHENTICATED",
            Self::Code(_) => "UNKNOWN_CODE",
        }
    }

    /// Maps a non-gRPC HTTP failure to a gRPC code, for servers that omit
    /// `grpc-status` entirely.
    fn from_http_status(status: u16) -> Self {
        match status {
            401 => Self::Unauthenticated,
            403 => Self::PermissionDenied,
            404 => Self::Unimplemented,
            429 | 502 | 503 | 504 => Self::Unavailable,
            _ => Self::Unknown,
        }
    }
}

impl fmt::Display for GrpcStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} ({})", self.name(), self.code())
    }
}

/// Errors of the StatsService client.
#[derive(Debug, thiserror::Error)]
pub enum StatsError {
    #[error("grpc {status}: {message}")]
    Grpc {
        status: GrpcStatus,
        message: String,
    },
    #[error("rpc timed out after {0:?}")]
    Timeout(Duration),
    #[error("transport: {0}")]
    Transport(String),
    #[error("protobuf: {0}")]
    Proto(String),
}

/// Raw result of one gRPC unary call, after frame handling and status parsing.
#[derive(Debug, Clone)]
pub struct GrpcResponse {
    pub status: GrpcStatus,
    pub message: String,
    /// The protobuf message extracted from the data frame (frame header removed).
    pub payload: Vec<u8>,
}

/// Injectable HTTP client used by [`GrpcStats`].
pub trait GrpcTransport: Send + Sync + fmt::Debug {
    /// Sends one unary RPC. `frame` is the complete gRPC frame (5-byte header
    /// plus protobuf payload); the returned response has its frame removed.
    fn unary(
        &self,
        path: &str,
        frame: &[u8],
        timeout: Duration,
    ) -> Result<GrpcResponse, StatsError>;
}

/// h2c (cleartext HTTP/2, prior knowledge) transport for the Xray API.
#[derive(Debug, Clone, Copy)]
pub struct H2Transport {
    pub addr: SocketAddr,
}

impl H2Transport {
    pub fn new(addr: SocketAddr) -> Self {
        Self { addr }
    }

    pub fn localhost(port: u16) -> Self {
        let ([a, b, c, d], _) = DEFAULT_STATS_ADDR;
        Self::new(SocketAddr::from(([a, b, c, d], port)))
    }
}

impl GrpcTransport for H2Transport {
    fn unary(
        &self,
        path: &str,
        frame: &[u8],
        timeout: Duration,
    ) -> Result<GrpcResponse, StatsError> {
        let addr = self.addr;
        let path = path.to_owned();
        let data = Bytes::copy_from_slice(frame);

        // The rest of the core is synchronous; drive the async h2 client on
        // a short-lived single-threaded runtime (one RPC per connection).
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|error| StatsError::Transport(format!("runtime: {error}")))?;
        runtime.block_on(async move {
            tokio::time::timeout(timeout, async move {
                let tcp = tokio::net::TcpStream::connect(addr)
                    .await
                    .map_err(|error| StatsError::Transport(format!("connect {addr}: {error}")))?;
                let (mut sender, connection) = h2::client::handshake(tcp)
                    .await
                    .map_err(|error| StatsError::Transport(format!("h2 handshake: {error}")))?;
                // The connection must be driven while the request is in
                // flight. Spawned tasks are cancelled when the runtime drops.
                let driver = tokio::spawn(async move {
                    let _ = connection.await;
                });

                let exchange = async {
                    let request = Request::builder()
                        .method(Method::POST)
                        .uri(path.as_str())
                        .header("content-type", "application/grpc")
                        .header("te", "trailers")
                        .body(())
                        .map_err(|error| StatsError::Transport(format!("request: {error}")))?;
                    let (response, mut stream) = sender
                        .send_request(request, false)
                        .map_err(|error| StatsError::Transport(format!("send request: {error}")))?;
                    stream
                        .send_data(data, true)
                        .map_err(|error| StatsError::Transport(format!("send body: {error}")))?;
                    let response = response
                        .await
                        .map_err(|error| StatsError::Transport(format!("response: {error}")))?;
                    let http_status = response.status();
                    let headers = response.headers().clone();
                    let mut body = response.into_body();
                    let mut payload = Vec::new();
                    while let Some(chunk) = body.data().await {
                        payload.extend_from_slice(
                            &chunk.map_err(|error| StatsError::Transport(format!("body: {error}")))?,
                        );
                    }
                    let trailers = body
                        .trailers()
                        .await
                        .map_err(|error| StatsError::Transport(format!("trailers: {error}")))?;
                    Ok::<_, StatsError>((http_status, headers, trailers, payload))
                };

                let result = exchange.await;
                driver.abort();
                let (http_status, headers, trailers, payload) = result?;

                let status = grpc_status(trailers.as_ref())
                    .or_else(|| grpc_status(Some(&headers)))
                    .unwrap_or_else(|| {
                        if http_status.is_success() {
                            GrpcStatus::Unknown
                        } else {
                            GrpcStatus::from_http_status(http_status.as_u16())
                        }
                    });
                let message = trailers
                    .as_ref()
                    .and_then(grpc_message)
                    .or_else(|| grpc_message(&headers))
                    .unwrap_or_default();
                Ok(GrpcResponse {
                    status,
                    message,
                    payload,
                })
            })
            .await
            .map_err(|_| StatsError::Timeout(timeout))?
        })
    }
}

fn grpc_status(headers: Option<&HeaderMap>) -> Option<GrpcStatus> {
    headers?
        .get("grpc-status")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.trim().parse::<u8>().ok())
        .map(GrpcStatus::from_code)
}

fn grpc_message(headers: &HeaderMap) -> Option<String> {
    headers
        .get("grpc-message")
        .and_then(|value| value.to_str().ok())
        .map(ToString::to_string)
}

/// Source of Xray traffic statistics.
///
/// The node/total view is derived from `QueryStats` counters. Counters are
/// named like `outbound>>>proxy>>>traffic>>>uplink`; `traffic(tag)` matches all
/// counters containing `tag>>>traffic>>>` and sums uplink/downlink values.
pub trait StatsService: Send + Sync {
    /// Whether live statistics are available at all (false for the fallback).
    fn available(&self) -> bool {
        true
    }

    /// Calls `GetStats`: returns exactly the named counter.
    fn get_stats(&self, name: &str) -> Result<TrafficCounter, StatsError>;

    /// Calls `QueryStats`: returns every counter whose name contains `pattern`.
    fn query_stats(&self, pattern: &str) -> Result<Vec<TrafficCounter>, StatsError>;

    /// Uplink/downlink totals for one outbound tag.
    fn traffic(&self, tag: &str) -> Result<TrafficSnapshot, String> {
        let pattern = format!("{tag}>>>traffic>>>");
        let counters = self.query_stats(&pattern).map_err(|error| error.to_string())?;
        let mut snapshot = TrafficSnapshot::default();
        for counter in counters {
            if counter.name.ends_with(">>>uplink") {
                snapshot.uplink = snapshot.uplink.saturating_add(counter.value.max(0) as u64);
            } else if counter.name.ends_with(">>>downlink") {
                snapshot.downlink = snapshot.downlink.saturating_add(counter.value.max(0) as u64);
            }
        }
        Ok(snapshot)
    }
}

/// Backwards-compatible narrow view used by the status handler.
pub trait TrafficStats: Send + Sync {
    fn query(&self, tag: &str) -> Result<TrafficSnapshot, String>;
}

impl<T: StatsService + ?Sized> TrafficStats for T {
    fn query(&self, tag: &str) -> Result<TrafficSnapshot, String> {
        self.traffic(tag)
    }
}

/// StatsService client over an injectable transport.
pub struct GrpcStats {
    transport: Arc<dyn GrpcTransport>,
    timeout: Duration,
}

impl fmt::Debug for GrpcStats {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GrpcStats")
            .field("transport", &self.transport)
            .field("timeout", &self.timeout)
            .finish()
    }
}

impl GrpcStats {
    /// Clients a real Xray API on `127.0.0.1:port`.
    pub fn new(port: u16) -> Self {
        Self::with_transport(Arc::new(H2Transport::localhost(port)))
    }

    pub fn with_addr(addr: SocketAddr) -> Self {
        Self::with_transport(Arc::new(H2Transport::new(addr)))
    }

    pub fn with_transport(transport: Arc<dyn GrpcTransport>) -> Self {
        Self {
            transport,
            timeout: DEFAULT_TIMEOUT,
        }
    }

    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.timeout = timeout;
        self
    }

    pub fn timeout(&self) -> Duration {
        self.timeout
    }

    /// Calls `GetStats` and decodes the returned `Stat`.
    pub fn get_stats(&self, name: &str) -> Result<TrafficCounter, StatsError> {
        let response = self.call(GET_STATS_PATH, encode_get_stats(name, false))?;
        parse_get_stats(unframe(&response.payload)?)
    }

    /// Calls `QueryStats` and decodes all returned `Stat`s.
    pub fn query_stats(&self, pattern: &str) -> Result<Vec<TrafficCounter>, StatsError> {
        let response = self.call(QUERY_STATS_PATH, encode_query_stats(pattern, false))?;
        parse_query_stats(unframe(&response.payload)?)
    }

    fn call(&self, path: &str, message: Vec<u8>) -> Result<GrpcResponse, StatsError> {
        let mut frame = Vec::with_capacity(5 + message.len());
        frame.push(0); // uncompressed
        frame.extend_from_slice(&(message.len() as u32).to_be_bytes());
        frame.extend_from_slice(&message);
        let response = self.transport.unary(path, &frame, self.timeout)?;
        if response.status != GrpcStatus::Ok {
            return Err(StatsError::Grpc {
                status: response.status,
                message: response.message,
            });
        }
        // A trailers-only error may carry no frame at all; that is fine. A
        // successful response, however, must contain one well-formed frame.
        if !response.payload.is_empty() {
            unframe(&response.payload)?;
        }
        Ok(response)
    }
}

impl StatsService for GrpcStats {
    fn get_stats(&self, name: &str) -> Result<TrafficCounter, StatsError> {
        GrpcStats::get_stats(self, name)
    }

    fn query_stats(&self, pattern: &str) -> Result<Vec<TrafficCounter>, StatsError> {
        GrpcStats::query_stats(self, pattern)
    }
}

/// Fallback used when no API port is configured, for tests and for a stopped
/// core. It never performs I/O.
#[derive(Debug, Clone, Copy, Default)]
pub struct NoopTrafficStats;

impl StatsService for NoopTrafficStats {
    fn available(&self) -> bool {
        false
    }

    fn get_stats(&self, name: &str) -> Result<TrafficCounter, StatsError> {
        Ok(TrafficCounter {
            name: name.to_owned(),
            value: 0,
        })
    }

    fn query_stats(&self, _pattern: &str) -> Result<Vec<TrafficCounter>, StatsError> {
        Ok(Vec::new())
    }
}

// ---------------------------------------------------------------------------
// Minimal protobuf wire codec.
// ---------------------------------------------------------------------------

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value as u8) | 0x80);
        value >>= 7;
    }
    out.push(value as u8);
}

fn get_varint(input: &[u8], pos: &mut usize) -> Option<u64> {
    let mut value = 0u64;
    for shift in (0..70).step_by(7) {
        let byte = *input.get(*pos)?;
        *pos += 1;
        if shift == 63 && byte > 1 {
            return None; // does not fit into a u64
        }
        value |= u64::from(byte & 0x7f) << shift;
        if byte & 0x80 == 0 {
            return Some(value);
        }
    }
    None
}

fn proto(message: &str) -> StatsError {
    StatsError::Proto(message.to_owned())
}

/// Strips the 5-byte gRPC message header: one compression byte (0 only) and
/// a big-endian payload length.
fn unframe(payload: &[u8]) -> Result<&[u8], StatsError> {
    if payload.len() < 5 || payload[0] != 0 {
        return Err(StatsError::Transport("invalid gRPC frame header".into()));
    }
    let length = u32::from_be_bytes(payload[1..5].try_into().unwrap()) as usize;
    if payload.len() != 5 + length {
        return Err(StatsError::Transport("gRPC frame length mismatch".into()));
    }
    Ok(&payload[5..])
}

/// Encodes `GetStatsRequest { name = 1, reset = 2 }`.
fn encode_get_stats(name: &str, reset: bool) -> Vec<u8> {
    let mut out = Vec::with_capacity(name.len() + 8);
    put_varint(&mut out, (1 << 3) | 2);
    put_varint(&mut out, name.len() as u64);
    out.extend_from_slice(name.as_bytes());
    if reset {
        put_varint(&mut out, 2 << 3);
        put_varint(&mut out, 1);
    }
    out
}

/// Encodes `QueryStatsRequest { pattern = 1, reset = 2 }`.
fn encode_query_stats(pattern: &str, reset: bool) -> Vec<u8> {
    let mut out = Vec::with_capacity(pattern.len() + 8);
    put_varint(&mut out, (1 << 3) | 2);
    put_varint(&mut out, pattern.len() as u64);
    out.extend_from_slice(pattern.as_bytes());
    if reset {
        put_varint(&mut out, 2 << 3);
        put_varint(&mut out, 1);
    }
    out
}

/// Decodes a `Stat { name = 1 (string), value = 2 (int64) }` message.
fn parse_stat(data: &[u8]) -> Result<TrafficCounter, StatsError> {
    let mut fields = Fields::new(data);
    let mut name = String::new();
    let mut value = 0i64;
    while let Some((tag, wire_type)) = fields.next()? {
        match (tag, wire_type) {
            (1, 2) => name = String::from_utf8(fields.bytes()?.to_vec())
                .map_err(|error| proto(&format!("stat name: {error}")))?,
            (2, 0) => value = fields.varint()?,
            _ => fields.skip(wire_type)?,
        }
    }
    Ok(TrafficCounter { name, value })
}

/// Decodes `GetStatsResponse { stat = 1 (message) }`.
fn parse_get_stats(data: &[u8]) -> Result<TrafficCounter, StatsError> {
    let mut fields = Fields::new(data);
    let mut stat = None;
    while let Some((tag, wire_type)) = fields.next()? {
        if tag == 1 && wire_type == 2 {
            stat = Some(parse_stat(fields.bytes()?)?);
        } else {
            fields.skip(wire_type)?;
        }
    }
    stat.ok_or_else(|| proto("GetStatsResponse has no stat"))
}

/// Decodes `QueryStatsResponse { stat = 1 (repeated message) }`.
fn parse_query_stats(data: &[u8]) -> Result<Vec<TrafficCounter>, StatsError> {
    let mut fields = Fields::new(data);
    let mut stats = Vec::new();
    while let Some((tag, wire_type)) = fields.next()? {
        if tag == 1 && wire_type == 2 {
            stats.push(parse_stat(fields.bytes()?)?);
        } else {
            fields.skip(wire_type)?;
        }
    }
    Ok(stats)
}

struct Fields<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Fields<'a> {
    fn new(data: &'a [u8]) -> Self {
        Self { data, pos: 0 }
    }

    fn next(&mut self) -> Result<Option<(u64, u8)>, StatsError> {
        if self.pos == self.data.len() {
            return Ok(None);
        }
        let key = get_varint(self.data, &mut self.pos).ok_or_else(|| proto("bad field key"))?;
        let tag = key >> 3;
        let wire_type = (key & 7) as u8;
        if tag == 0 {
            return Err(proto("field number 0"));
        }
        Ok(Some((tag, wire_type)))
    }

    fn varint(&mut self) -> Result<i64, StatsError> {
        get_varint(self.data, &mut self.pos)
            .map(|value| value as i64)
            .ok_or_else(|| proto("bad varint"))
    }

    fn bytes(&mut self) -> Result<&'a [u8], StatsError> {
        let length = get_varint(self.data, &mut self.pos)
            .ok_or_else(|| proto("bad length"))? as usize;
        let end = self
            .pos
            .checked_add(length)
            .ok_or_else(|| proto("length overflow"))?;
        let slice = self
            .data
            .get(self.pos..end)
            .ok_or_else(|| proto("truncated field"))?;
        self.pos = end;
        Ok(slice)
    }

    fn skip(&mut self, wire_type: u8) -> Result<(), StatsError> {
        match wire_type {
            0 => {
                self.varint()?;
            }
            1 => self.advance(8)?,
            2 => {
                self.bytes()?;
            }
            5 => self.advance(4)?,
            other => return Err(proto(&format!("unsupported wire type {other}"))),
        }
        Ok(())
    }

    fn advance(&mut self, count: usize) -> Result<(), StatsError> {
        let end = self
            .pos
            .checked_add(count)
            .ok_or_else(|| proto("length overflow"))?;
        if end > self.data.len() {
            return Err(proto("truncated field"));
        }
        self.pos = end;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn varint_round_trip() {
        for value in [0u64, 1, 127, 128, 300, u64::MAX] {
            let mut out = Vec::new();
            put_varint(&mut out, value);
            let mut pos = 0;
            assert_eq!(get_varint(&out, &mut pos), Some(value));
            assert_eq!(pos, out.len());
        }
        assert_eq!(get_varint(&[0x80], &mut 0), None);
        assert_eq!(get_varint(&[0xff, 0x02], &mut 0), Some(383));
    }

    #[test]
    fn encodes_requests() {
        assert_eq!(
            encode_get_stats("proxy", false),
            b"\x0a\x05proxy"
        );
        assert_eq!(
            encode_get_stats("proxy", true),
            b"\x0a\x05proxy\x10\x01"
        );
        assert_eq!(
            encode_query_stats(">>>traffic>>>", false),
            b"\x0a\x0d>>>traffic>>>"
        );
    }

    fn framed(stat: (&str, i64)) -> Vec<u8> {
        let mut nested = Vec::new();
        put_varint(&mut nested, (1 << 3) | 2);
        put_varint(&mut nested, stat.0.len() as u64);
        nested.extend_from_slice(stat.0.as_bytes());
        if stat.1 != 0 {
            put_varint(&mut nested, 2 << 3);
            put_varint(&mut nested, stat.1 as u64);
        }
        // GetStatsResponse { stat: Some(Stat) }.
        let mut message = Vec::new();
        put_varint(&mut message, (1 << 3) | 2);
        put_varint(&mut message, nested.len() as u64);
        message.extend_from_slice(&nested);
        let mut frame = vec![0];
        frame.extend_from_slice(&(message.len() as u32).to_be_bytes());
        frame.extend_from_slice(&message);
        frame
    }

    #[test]
    fn parses_responses() {
        let frame = framed(("outbound>>>proxy>>>traffic>>>uplink", 1234));
        let len = u32::from_be_bytes(frame[1..5].try_into().unwrap()) as usize;
        assert_eq!(frame.len(), 5 + len);
        let counter = parse_get_stats(unframe(&frame).unwrap()).unwrap();
        assert_eq!(counter.name, "outbound>>>proxy>>>traffic>>>uplink");
        assert_eq!(counter.value, 1234);

        assert!(unframe(&[1, 0, 0, 0, 0]).is_err()); // compressed
        assert!(unframe(&[0, 0, 0, 0, 5, 1]).is_err()); // truncated
    }

    #[test]
    fn parses_query_response_and_negative_values() {
        let mut message = Vec::new();
        for (name, value) in [
            ("outbound>>>proxy>>>traffic>>>uplink", 10i64),
            ("outbound>>>proxy>>>traffic>>>downlink", -7i64),
        ] {
            let mut stat = Vec::new();
            put_varint(&mut stat, (1 << 3) | 2);
            put_varint(&mut stat, name.len() as u64);
            stat.extend_from_slice(name.as_bytes());
            put_varint(&mut stat, 2 << 3);
            put_varint(&mut stat, value as u64);
            put_varint(&mut message, (1 << 3) | 2);
            put_varint(&mut message, stat.len() as u64);
            message.extend_from_slice(&stat);
        }
        let counters = parse_query_stats(&message).unwrap();
        assert_eq!(counters.len(), 2);
        assert_eq!(counters[1].value, -7);
    }

    #[test]
    fn rejects_malformed_messages() {
        assert!(parse_get_stats(&[]).is_err());
        assert!(parse_get_stats(&[0x0a, 5, 1, 2]).is_err()); // truncated
        assert!(parse_query_stats(&[0x0f]).is_err()); // unsupported wire type
    }

    #[test]
    fn status_codes_round_trip() {
        for code in [0u8, 2, 3, 5, 12, 14, 99] {
            assert_eq!(GrpcStatus::from_code(code).code(), code);
        }
        assert_eq!(GrpcStatus::from_code(5), GrpcStatus::NotFound);
        assert_eq!(GrpcStatus::from_http_status(404), GrpcStatus::Unimplemented);
    }
}
