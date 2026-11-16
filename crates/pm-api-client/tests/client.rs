//! End-to-end tests against a real `pm-api` server.
//!
//! The server is the Rust implementation of `base/api`, so these tests cover
//! the client against the exact frame format and authentication it must speak.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use pm_api::{Api, AuthToken};
use futures_util::{SinkExt, StreamExt};
use pm_api_client::{msg, Client, Error, Message, Operation};
use pm_database::{Databases, Interface, Key, MemoryStorage, Meta, Record};
use serde_json::{json, Value};
use tokio::net::TcpListener;
use tokio::task::JoinHandle;
use tokio_tungstenite::tungstenite::Message as WsMessage;
use tokio_util::sync::CancellationToken;

const JSON: u8 = pm_dsd::format::JSON;
const ADMIN_KEY: &str = "admin-key?read=admin&write=admin";

fn databases() -> Arc<Databases> {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("test", "test database")
        .unwrap();
    databases
        .inject("test", Box::new(MemoryStorage::new()))
        .unwrap();
    databases
}

fn internal(databases: &Arc<Databases>) -> Interface {
    databases.internal_interface()
}

fn record(key: &str, value: Value) -> Record {
    Record {
        key: Key::parse(key),
        meta: Meta::default(),
        format: JSON,
        data: pm_dsd::gojson::to_vec(&value).unwrap(),
    }
}

fn put(databases: &Arc<Databases>, key: &str, value: Value) {
    internal(databases).put(record(key, value)).unwrap();
}

fn api_with(databases: Arc<Databases>) -> Arc<Api> {
    let api = Api::new("127.0.0.1:0");
    api.set_databases(databases);
    api.update_api_keys(&[ADMIN_KEY.to_string()]);
    api
}

async fn bind() -> (TcpListener, SocketAddr) {
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    (listener, address)
}

async fn spawn_server(databases: Arc<Databases>) -> (Arc<Api>, JoinHandle<()>, SocketAddr) {
    let api = api_with(databases);
    let (listener, address) = bind().await;
    let server_api = Arc::clone(&api);
    let server = tokio::spawn(async move {
        let _ = pm_api::serve(server_api, listener).await;
    });
    (api, server, address)
}

/// A controllable single-connection server built on the real `DatabaseApi`.
/// Cancelling `kill` drops the WebSocket, which lets tests drive reconnects.
async fn mock_server(databases: Arc<Databases>, listener: TcpListener, kill: CancellationToken) {
    let (stream, _) = listener.accept().await.expect("accept failed");
    let websocket = tokio_tungstenite::accept_async(stream)
        .await
        .expect("handshake failed");
    let (api, mut frames) = pm_api::DatabaseApi::new(databases);
    let (mut write, mut read) = websocket.split();

    loop {
        tokio::select! {
            _ = kill.cancelled() => break,
            frame = frames.recv() => {
                let Some(frame) = frame else { break };
                if write.send(WsMessage::Binary(frame.into())).await.is_err() {
                    break;
                }
            }
            message = read.next() => {
                match message {
                    Some(Ok(WsMessage::Binary(data))) => api.handle(&data),
                    Some(Ok(WsMessage::Text(text))) => api.handle(text.as_bytes()),
                    Some(Ok(WsMessage::Close(_))) | Some(Err(_)) | None => break,
                    _ => {}
                }
            }
        }
    }
    api.shutdown();
}

fn client(address: SocketAddr) -> Client {
    Client::new(address.to_string()).with_token("admin-key")
}

async fn recv(operation: &mut Operation) -> Message {
    tokio::time::timeout(Duration::from_secs(3), operation.recv())
        .await
        .expect("timed out waiting for a reply")
        .expect("operation closed")
}

async fn wait_online(client: &Client, expected: bool) {
    let mut status = client.online();
    tokio::time::timeout(Duration::from_secs(6), async {
        while *status.borrow() != expected {
            status.changed().await.expect("status channel closed");
        }
    })
    .await
    .unwrap_or_else(|_| panic!("timed out waiting for online={expected}"));
}

#[tokio::test]
async fn get_create_update_query_delete() {
    let databases = databases();
    let (_api, server, address) = spawn_server(databases).await;
    let client = client(address);
    let runner = client.clone();
    let connection = tokio::spawn(async move { runner.connect().await });
    wait_online(&client, true).await;

    // create
    let mut created = client.create("test:key", json!({"Value": 1}));
    let reply = recv(&mut created).await;
    assert_eq!(reply.msg_type, msg::SUCCESS);

    // get
    let mut got = client.get("test:key");
    let reply = recv(&mut got).await;
    assert_eq!((reply.op_id.as_str(), reply.msg_type.as_str()), (got.id(), msg::OK));
    assert_eq!(reply.key, "test:key");
    assert_eq!(reply.value_json().unwrap()["Value"], 1);
    assert_eq!(reply.value_json().unwrap()["_meta"]["Key"], "test:key");

    // update replaces the value
    let mut updated = client.update("test:key", json!({"Value": 2}));
    assert_eq!(recv(&mut updated).await.msg_type, msg::SUCCESS);
    let mut got = client.get("test:key");
    assert_eq!(recv(&mut got).await.value_json().unwrap()["Value"], 2);

    // query yields one record and then done
    let mut query = client.query("query test:");
    let record_reply = recv(&mut query).await;
    assert_eq!(record_reply.msg_type, msg::OK);
    assert_eq!(record_reply.key, "test:key");
    assert_eq!(recv(&mut query).await.msg_type, msg::DONE);

    // delete, then get reports an error
    let mut deleted = client.delete("test:key");
    assert_eq!(recv(&mut deleted).await.msg_type, msg::SUCCESS);
    let mut missing = client.get("test:key");
    let reply = recv(&mut missing).await;
    assert_eq!(reply.msg_type, msg::ERROR);
    assert_eq!(reply.key, "database entry not found");

    client.shutdown();
    connection.await.unwrap().unwrap();
    server.abort();
}

#[tokio::test]
async fn sub_and_qsub_receive_live_updates() {
    let databases = databases();
    put(&databases, "test:existing", json!({"Value": 1}));
    let (_api, server, address) = spawn_server(databases.clone()).await;
    let client = client(address);
    let runner = client.clone();
    let connection = tokio::spawn(async move { runner.connect().await });
    wait_online(&client, true).await;

    let mut sub = client.sub("query test:");
    // The ordered replies guarantee the server processed `sub` first.
    let mut barrier = client.get("test:existing");
    assert_eq!(recv(&mut barrier).await.msg_type, msg::OK);

    put(&databases, "test:live", json!({"Value": 7}));
    let reply = recv(&mut sub).await;
    assert_eq!((reply.op_id.as_str(), reply.msg_type.as_str()), (sub.id(), msg::NEW));
    assert_eq!(reply.key, "test:live");
    assert_eq!(reply.value_json().unwrap()["Value"], 7);

    let mut qsub = client.qsub("query test:");
    // One `ok` per matching record, then `done`.
    loop {
        let reply = recv(&mut qsub).await;
        if reply.msg_type == msg::DONE {
            break;
        }
        assert_eq!(reply.msg_type, msg::OK);
    }

    let mut changed = client.update("test:existing", json!({"Value": 9}));
    assert_eq!(recv(&mut changed).await.msg_type, msg::SUCCESS);
    let reply = recv(&mut qsub).await;
    // The server stamps put records with a fresh meta, so this is `new`.
    assert!(reply.is_record(), "unexpected frame: {}", reply.msg_type);
    assert_eq!(reply.key, "test:existing");
    assert_eq!(reply.value_json().unwrap()["Value"], 9);

    sub.cancel();
    qsub.cancel();
    client.shutdown();
    connection.await.unwrap().unwrap();
    server.abort();
}

#[tokio::test]
async fn reconnect_resuscitates_subscription() {
    let databases = databases();
    let (listener, address) = bind().await;
    let kill = CancellationToken::new();
    let server = tokio::spawn(mock_server(
        Arc::clone(&databases),
        listener,
        kill.clone(),
    ));

    let client = Arc::new(client(address));
    let connection = client.stay_connected();
    wait_online(&client, true).await;

    let mut sub = client.sub("query test:");
    // A get reply after the sub proves the subscription was registered.
    let mut barrier = client.get("test:none");
    assert_eq!(recv(&mut barrier).await.msg_type, msg::ERROR);

    // Drop the WebSocket and wait for the client to notice.
    kill.cancel();
    let _ = server.await;
    wait_online(&client, false).await;

    // Bring the same endpoint back; stay_connected retries every second.
    let listener = TcpListener::bind(address).await.expect("rebind failed");
    let kill2 = CancellationToken::new();
    let restarted = tokio::spawn(mock_server(
        Arc::clone(&databases),
        listener,
        kill2.clone(),
    ));
    wait_online(&client, true).await;

    // A fresh get reply proves the resuscitated `sub` reached the new server.
    let mut barrier = client.get("test:none");
    assert_eq!(recv(&mut barrier).await.msg_type, msg::ERROR);

    // The subscription was resent on reconnect, so it sees the new record.
    put(&databases, "test:after", json!({"Value": 5}));
    let reply = loop {
        let reply = recv(&mut sub).await;
        if reply.msg_type == msg::OFFLINE {
            continue; // delivered when the old connection dropped
        }
        break reply;
    };
    assert_eq!(
        (reply.op_id.as_str(), reply.msg_type.as_str()),
        (sub.id(), msg::NEW)
    );
    assert_eq!(reply.key, "test:after");

    sub.cancel();
    client.shutdown();
    let _ = connection.await;
    kill2.cancel();
    let _ = restarted.await;
}

#[tokio::test]
async fn authentication_errors_and_session_cookie() {
    let databases = databases();
    let api = api_with(databases);
    let session = api.sessions().create(AuthToken::full()).unwrap();
    let (listener, address) = bind().await;
    let server = tokio::spawn(pm_api::serve(api, listener));

    // No credentials: the handshake is rejected by the server.
    let anonymous = Client::new(address.to_string());
    assert!(matches!(
        anonymous.connect().await,
        Err(Error::WebSocket(_))
    ));
    assert!(!anonymous.is_online());

    // Wrong token: also rejected.
    let wrong = Client::new(address.to_string()).with_token("wrong-key");
    assert!(wrong.connect().await.is_err());

    // A valid session cookie authenticates and permits database requests.
    let authenticated = Client::new(address.to_string()).with_session_cookie(&session);
    let runner = authenticated.clone();
    let connection = tokio::spawn(async move { runner.connect().await });
    wait_online(&authenticated, true).await;

    let mut missing = authenticated.get("test:none");
    let reply = recv(&mut missing).await;
    assert_eq!(reply.msg_type, msg::ERROR, "request reached the database API");

    authenticated.shutdown();
    connection.await.unwrap().unwrap();
    server.abort();
}
