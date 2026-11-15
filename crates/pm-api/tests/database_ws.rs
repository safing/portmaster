//! Tests of the `/api/database/v1` frame protocol.

use std::sync::Arc;
use std::time::Duration;

use pm_api::DatabaseApi;
use pm_database::{Databases, Interface, Key, MemoryStorage, Meta, Record};
use serde_json::{json, Value};
use tokio::sync::mpsc::UnboundedReceiver;

const JSON: u8 = pm_dsd::format::JSON;

fn record(key: &str, value: Value) -> Record {
    Record {
        key: Key::parse(key),
        meta: Meta::default(),
        format: JSON,
        data: pm_dsd::gojson::to_vec(&value).unwrap(),
    }
}

fn setup() -> (Arc<Databases>, Arc<DatabaseApi>, UnboundedReceiver<Vec<u8>>) {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("test", "test database")
        .unwrap();
    databases
        .inject("test", Box::new(MemoryStorage::new()))
        .unwrap();
    let (api, frames) = DatabaseApi::new(Arc::clone(&databases));
    (databases, api, frames)
}

fn internal(databases: &Arc<Databases>) -> Interface {
    databases.internal_interface()
}

#[derive(Debug)]
struct Frame {
    op: String,
    kind: String,
    key: Option<String>,
    data: Option<Vec<u8>>,
}

fn parse_frame(frame: &[u8]) -> Frame {
    let mut parts = frame.splitn(4, |&byte| byte == b'|');
    let op = String::from_utf8_lossy(parts.next().unwrap_or_default()).into_owned();
    let kind = String::from_utf8_lossy(parts.next().unwrap_or_default()).into_owned();
    let key = parts
        .next()
        .map(|part| String::from_utf8_lossy(part).into_owned());
    let data = parts.next().map(<[u8]>::to_vec);
    Frame {
        op,
        kind,
        key,
        data,
    }
}

fn next_frame(frames: &mut UnboundedReceiver<Vec<u8>>) -> Frame {
    let frame = frames.try_recv().expect("expected a frame");
    parse_frame(&frame)
}

async fn next_frame_async(frames: &mut UnboundedReceiver<Vec<u8>>) -> Frame {
    let frame = tokio::time::timeout(Duration::from_secs(2), frames.recv())
        .await
        .expect("timed out waiting for a frame")
        .expect("frame channel closed");
    parse_frame(&frame)
}

fn json_of(frame: &Frame) -> Value {
    let data = frame.data.as_ref().expect("frame has data");
    assert_eq!(data[0], b'J', "record frames start with the JSON byte");
    serde_json::from_slice(&data[1..]).unwrap()
}

#[test]
fn get_frames() {
    let (databases, api, mut frames) = setup();
    internal(&databases)
        .put(record("test:answer", json!({"Value": 42, "Text": "<&>"})))
        .unwrap();

    api.handle(b"1|get|test:answer");
    let frame = next_frame(&mut frames);
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("1", "ok"));
    assert_eq!(frame.key.as_deref(), Some("test:answer"));
    let json = json_of(&frame);
    assert_eq!(json["Value"], 42);
    assert_eq!(json["Text"], "<&>");
    assert_eq!(json["_meta"]["Key"], "test:answer");
    assert!(json["_meta"]["Created"].as_i64().unwrap() > 0);

    api.handle(b"2|get|test:missing");
    let frame = next_frame(&mut frames);
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("2", "error"));
    assert_eq!(frame.key.as_deref(), Some("database entry not found"));
}

#[test]
fn query_frames() {
    let (databases, api, mut frames) = setup();
    databases
        .register_injected("other", "other database")
        .unwrap();
    databases
        .inject("other", Box::new(MemoryStorage::new()))
        .unwrap();
    let internal = internal(&databases);
    internal
        .put(record("test:a", json!({"Image": "a"})))
        .unwrap();
    internal
        .put(record("test:b", json!({"Image": "b"})))
        .unwrap();
    internal
        .put(record("other:c", json!({"Image": "c"})))
        .unwrap();

    api.handle(b"3|query|query test:");
    let first = next_frame(&mut frames);
    let second = next_frame(&mut frames);
    let done = next_frame(&mut frames);
    assert_eq!(first.kind, "ok");
    assert_eq!(second.kind, "ok");
    assert_eq!(done.kind, "done");
    assert_eq!(done.key, None);
    let keys = [first.key.unwrap(), second.key.unwrap()];
    assert_eq!(keys, ["test:a".to_string(), "test:b".to_string()]);

    // Parse errors are reported without a done frame.
    api.handle(b"4|query|query");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.kind, "error");
    assert_eq!(frame.key.as_deref(), Some("unexpected end at position 5"));
}

#[test]
fn create_update_insert_delete_frames() {
    let (_databases, api, mut frames) = setup();

    // create: 128|create|<key>|<format><json>
    let mut message = b"10|create|test:new|".to_vec();
    message.push(JSON);
    message.extend_from_slice(br#"{"Value":1}"#);
    api.handle(&message);
    let frame = next_frame(&mut frames);
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("10", "success"));

    api.handle(b"11|get|test:new");
    let json = json_of(&next_frame(&mut frames));
    assert_eq!(json["Value"], 1);

    // update replaces the value.
    let mut message = b"12|update|test:new|".to_vec();
    message.push(JSON);
    message.extend_from_slice(br#"{"Value":2}"#);
    api.handle(&message);
    assert_eq!(next_frame(&mut frames).kind, "success");

    // insert merges attributes into the existing record.
    api.handle(br#"13|insert|test:new|{"Extra":"x","Nested":{"a":1}}"#);
    assert_eq!(next_frame(&mut frames).kind, "success");
    api.handle(b"14|get|test:new");
    let frame = next_frame(&mut frames);
    let json = json_of(&frame);
    assert_eq!(json["Value"], 2);
    assert_eq!(json["Extra"], "x");
    assert_eq!(json["Nested"]["a"], 1);
    assert_eq!(json["_meta"]["Key"], "test:new");

    // insert errors.
    api.handle(b"15|insert|test:new|[1,2]");
    assert_eq!(next_frame(&mut frames).kind, "error", "arrays are not maps");
    api.handle(b"16|insert|test:new|{}");
    let frame = next_frame(&mut frames);
    assert_eq!(
        frame.key.as_deref(),
        Some("could not find any valid values")
    );
    api.handle(b"17|insert|test:new|{\"Null\":null}");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.key.as_deref(), Some("non-existent value"));

    // delete.
    api.handle(b"18|delete|test:new");
    assert_eq!(next_frame(&mut frames).kind, "success");
    api.handle(b"19|get|test:new");
    assert_eq!(next_frame(&mut frames).kind, "error");

    // Short create payloads are malformed.
    api.handle(b"20|create|test:x|J");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.key.as_deref(), Some("bad request: malformed message"));
}

#[test]
fn malformed_frames() {
    let (_databases, api, mut frames) = setup();

    api.handle(b"garbage");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.op, "");
    assert_eq!(frame.kind, "error");
    assert_eq!(frame.key.as_deref(), Some("bad request: malformed message"));

    api.handle(b"1|unknown|payload");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.op, "1");
    assert_eq!(frame.key.as_deref(), Some("bad request: unknown method"));

    api.handle(b"2|create|only-key");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.key.as_deref(), Some("bad request: malformed message"));

    api.handle(b"3|cancel");
    let frame = next_frame(&mut frames);
    assert_eq!(frame.op, "3");
    assert_eq!(frame.key.as_deref(), Some("could not find subscription"));
}

#[tokio::test]
async fn subscriptions() {
    let (databases, api, mut frames) = setup();
    let internal = internal(&databases);

    api.handle(b"20|sub|query test:s");
    // Subscriptions start silently.
    assert!(frames.try_recv().is_err());

    // A new record produces a "new" frame.
    internal.put(record("test:s", json!({"Value": 1}))).unwrap();
    let frame = next_frame_async(&mut frames).await;
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("20", "new"));
    assert_eq!(frame.key.as_deref(), Some("test:s"));
    assert_eq!(json_of(&frame)["Value"], 1);

    // Updating an older record produces an "upd" frame.
    let mut updated = record("test:s", json!({"Value": 2}));
    updated.meta.created = 1;
    internal.put(updated).unwrap();
    let frame = next_frame_async(&mut frames).await;
    assert_eq!(frame.kind, "upd");
    assert_eq!(json_of(&frame)["Value"], 2);

    // Deleting produces a "del" frame without data.
    internal.delete("test:s").unwrap();
    let frame = next_frame_async(&mut frames).await;
    assert_eq!(frame.kind, "del");
    assert_eq!(frame.key.as_deref(), Some("test:s"));
    assert!(frame.data.is_none());

    // Non-matching writes are not delivered.
    internal
        .put(record("test:other", json!({"Value": 3})))
        .unwrap();
    tokio::time::sleep(Duration::from_millis(20)).await;
    assert!(frames.try_recv().is_err());

    // Cancel ends the subscription with a done frame.
    api.handle(b"20|cancel");
    let frame = next_frame_async(&mut frames).await;
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("20", "done"));
}

#[tokio::test]
async fn query_subscriptions() {
    let (databases, api, mut frames) = setup();
    let internal = internal(&databases);
    internal.put(record("test:a", json!({"Value": 1}))).unwrap();

    api.handle(b"21|qsub|query test:");
    let ok = next_frame_async(&mut frames).await;
    let done = next_frame_async(&mut frames).await;
    assert_eq!(ok.kind, "ok");
    assert_eq!(done.kind, "done");

    internal.put(record("test:b", json!({"Value": 2}))).unwrap();
    let frame = next_frame_async(&mut frames).await;
    assert_eq!(frame.kind, "new");
    assert_eq!(frame.key.as_deref(), Some("test:b"));
}

#[tokio::test]
async fn shutdown_cancels_subscriptions() {
    let (databases, api, mut frames) = setup();
    let internal = internal(&databases);

    api.handle(b"30|sub|query test:");
    api.shutdown();
    let frame = next_frame_async(&mut frames).await;
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("30", "done"));

    internal
        .put(record("test:late", json!({"Value": 1})))
        .unwrap();
    tokio::time::sleep(Duration::from_millis(20)).await;
    assert!(frames.try_recv().is_err());
}

#[tokio::test]
async fn websocket_route_requires_admin() {
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    let api = pm_api::Api::new("127.0.0.1:0");
    let databases = Arc::new(Databases::without_disk());
    api.set_databases(databases);
    api.update_api_keys(&["admin-key?read=admin&write=admin".to_string()]);

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(Arc::clone(&api), listener));

    async fn request(address: std::net::SocketAddr, headers: &str) -> String {
        let mut stream = tokio::net::TcpStream::connect(address).await.unwrap();
        let message = format!(
            "GET /api/database/v1 HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n{headers}\r\n"
        );
        stream.write_all(message.as_bytes()).await.unwrap();
        let mut response = Vec::new();
        stream.read_to_end(&mut response).await.unwrap();
        String::from_utf8_lossy(&response).into_owned()
    }

    // Without credentials the WebSocket API is not reachable.
    let response = request(address, "").await;
    assert!(response.starts_with("HTTP/1.1 401"), "{response}");
    assert!(response.contains("Authorization required."), "{response}");

    // With an admin key the upgrade is attempted (and rejected as not a
    // WebSocket request).
    let response = request(address, "Authorization: Bearer admin-key\r\n").await;
    assert!(
        response.starts_with("HTTP/1.1 400") || response.starts_with("HTTP/1.1 426"),
        "{response}"
    );

    server.abort();
}

#[tokio::test]
async fn websocket_end_to_end() {
    use futures_util::{SinkExt, StreamExt};
    use tokio_tungstenite::tungstenite::client::IntoClientRequest;
    use tokio_tungstenite::tungstenite::{http, Message};

    let api = pm_api::Api::new("127.0.0.1:0");
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("test", "test database")
        .unwrap();
    databases
        .inject("test", Box::new(MemoryStorage::new()))
        .unwrap();
    internal(&databases)
        .put(record("test:key", json!({"Value": "banana"})))
        .unwrap();
    api.set_databases(Arc::clone(&databases));
    api.update_api_keys(&["admin-key?read=admin&write=admin".to_string()]);

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let server = tokio::spawn(pm_api::serve(api, listener));

    let mut request = format!("ws://{address}/api/database/v1")
        .into_client_request()
        .unwrap();
    request.headers_mut().insert(
        "authorization",
        http::HeaderValue::from_static("Bearer admin-key"),
    );
    let (mut socket, response) = tokio_tungstenite::connect_async(request).await.unwrap();
    assert_eq!(response.status(), 101);

    // get
    socket
        .send(Message::Binary(b"1|get|test:key".to_vec().into()))
        .await
        .unwrap();
    let message = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap();
    let frame = parse_frame(&message.into_data());
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("1", "ok"));
    assert_eq!(frame.key.as_deref(), Some("test:key"));
    assert_eq!(json_of(&frame)["Value"], "banana");

    // qsub: the initial query proves that the subscription is registered.
    socket
        .send(Message::Binary(b"2|qsub|query test:".to_vec().into()))
        .await
        .unwrap();
    let initial = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap();
    assert_eq!(parse_frame(&initial.into_data()).kind, "ok");
    let done = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap();
    assert_eq!(parse_frame(&done.into_data()).kind, "done");

    internal(&databases)
        .put(record("test:live", json!({"Value": 7})))
        .unwrap();
    let message = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap();
    let frame = parse_frame(&message.into_data());
    assert_eq!((frame.op.as_str(), frame.kind.as_str()), ("2", "new"));
    assert_eq!(frame.key.as_deref(), Some("test:live"));

    // cancel reply and clean close
    socket
        .send(Message::Binary(b"2|cancel".to_vec().into()))
        .await
        .unwrap();
    let message = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap();
    let frame = parse_frame(&message.into_data());
    assert_eq!(frame.kind, "done");

    socket.close(None).await.unwrap();
    server.abort();
}
