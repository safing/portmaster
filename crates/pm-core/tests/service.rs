//! Integration test: the core starts, serves the API and the database.

use std::sync::Arc;
use std::time::Duration;

use pm_core::cli::Options;
use pm_core::service::Service;
use serde_json::json;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio_tungstenite::tungstenite::client::IntoClientRequest;

fn test_options(data_dir: &std::path::Path) -> Options {
    Options {
        data_dir: data_dir.to_path_buf(),
        bin_dir: data_dir.join("bin"),
        log_dir: data_dir.join("logs"),
        log_to_stdout: true,
        log_level: "debug".to_string(),
        api_address: "127.0.0.1:0".to_string(),
        dev_mode: true,
        print_version: false,
        help: false,
    }
}

fn temp_dir(tag: &str) -> std::path::PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-core-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let _ = std::fs::remove_dir_all(&path);
    std::fs::create_dir_all(&path).unwrap();
    path
}

async fn http_get(address: std::net::SocketAddr, path: &str, headers: &str) -> String {
    let mut stream = tokio::net::TcpStream::connect(address).await.unwrap();
    let message =
        format!("GET {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n{headers}\r\n");
    stream.write_all(message.as_bytes()).await.unwrap();
    let mut response = Vec::new();
    stream.read_to_end(&mut response).await.unwrap();
    String::from_utf8_lossy(&response).into_owned()
}

#[tokio::test]
async fn core_serves_api_and_database() {
    let data_dir = temp_dir("serve");
    let options = test_options(&data_dir);
    let service = Arc::new(Service::start(&options).await.unwrap());
    let address = service.local_addr();
    assert!(address.ip().is_loopback());

    let server = tokio::spawn(Arc::clone(&service).run());

    // The UI root redirects to the default module, like in Go.
    let response = http_get(address, "/", "").await;
    assert!(response.starts_with("HTTP/1.1 307"), "{response}");
    assert!(
        response.contains("location: /ui/modules/portmaster/"),
        "{response}"
    );

    // Public endpoints.
    let response = http_get(address, "/api/v1/ping", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    assert!(response.ends_with("Pong.\n"), "{response}");

    let response = http_get(address, "/api/v1/ready", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    assert!(response.contains("Portmaster is ready."), "{response}");

    // Configuration is exposed.
    let response = http_get(address, "/api/v1/config/options", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    let body = response.split("\r\n\r\n").nth(1).unwrap();
    let options_json: serde_json::Value = serde_json::from_str(body).unwrap();
    let keys: Vec<&str> = options_json
        .as_array()
        .unwrap()
        .iter()
        .filter_map(|option| option["Key"].as_str())
        .collect();
    assert!(keys.contains(&"core/listenAddress"), "{keys:?}");
    assert!(keys.contains(&"core/apiKeys"), "{keys:?}");
    assert!(keys.contains(&"core/expertiseLevel"), "{keys:?}");

    // The databases were created on disk.
    assert!(data_dir.join("databases/core/sqlite/db.sqlite").exists());
    assert!(!data_dir.join("databases/config").exists());

    // The runtime registry is injected as its own database.
    assert!(service
        .databases
        .databases()
        .iter()
        .any(|info| info.name == "runtime"));
    assert_eq!(service.runtime.database_name(), "runtime");
    assert!(service.runtime.controller().is_some());

    // The database WebSocket API works end to end (dev mode grants admin).
    service
        .databases
        .internal_interface()
        .put(pm_database::Record {
            key: pm_database::Key::parse("core:test/key"),
            meta: pm_database::Meta::default(),
            format: pm_dsd::format::JSON,
            data: pm_dsd::gojson::to_vec(&json!({"Value": "banana"})).unwrap(),
        })
        .unwrap();

    let mut request = format!("ws://{address}/api/database/v1")
        .into_client_request()
        .unwrap();
    request.headers_mut().insert(
        "authorization",
        tokio_tungstenite::tungstenite::http::HeaderValue::from_static("Bearer dev"),
    );
    let (mut socket, upgrade) = tokio_tungstenite::connect_async(request).await.unwrap();
    assert_eq!(upgrade.status(), 101);

    use futures_util::{SinkExt, StreamExt};
    socket
        .send(tokio_tungstenite::tungstenite::Message::Binary(
            b"1|get|core:test/key".to_vec().into(),
        ))
        .await
        .unwrap();
    let message = tokio::time::timeout(Duration::from_secs(2), socket.next())
        .await
        .expect("timed out")
        .unwrap()
        .unwrap()
        .into_data();
    let frame = String::from_utf8_lossy(&message);
    assert!(frame.starts_with("1|ok|core:test/key|J"), "{frame}");
    assert!(frame.contains("\"Value\":\"banana\""), "{frame}");
    assert!(frame.contains("\"_meta\""), "{frame}");
    socket.close(None).await.unwrap();

    server.abort();
    service.shutdown().unwrap();
}

#[tokio::test]
async fn config_file_controls_api_keys_and_address() {
    let data_dir = temp_dir("config");
    std::fs::create_dir_all(&data_dir).unwrap();
    std::fs::write(
        data_dir.join("config.json"),
        r#"{
  "core": {
    "listenAddress": "127.0.0.1:0",
    "apiKeys": ["test-key?read=admin&write=admin"]
  }
}"#,
    )
    .unwrap();

    let mut options = test_options(&data_dir);
    options.api_address = "127.0.0.1:1".to_string();
    options.dev_mode = false;
    let service = Arc::new(Service::start(&options).await.unwrap());
    let address = service.local_addr();
    // The configuration file wins over the flag.
    assert!(address.port() != 1, "{address}");
    let server = tokio::spawn(Arc::clone(&service).run());

    assert_eq!(
        service.config.get_as_string("core/listenAddress", "")(),
        "127.0.0.1:0"
    );
    assert_eq!(
        service.config.get_as_string_array("core/apiKeys", &[])(),
        vec!["test-key?read=admin&write=admin".to_string()]
    );

    // Public endpoints stay open.
    let response = http_get(address, "/api/v1/ping", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");

    // The database API requires the configured admin key.
    let response = http_get(address, "/api/database/v1", "").await;
    assert!(response.starts_with("HTTP/1.1 401"), "{response}");

    let response = http_get(
        address,
        "/api/database/v1",
        "Authorization: Bearer test-key\r\n",
    )
    .await;
    assert!(
        response.starts_with("HTTP/1.1 400") || response.starts_with("HTTP/1.1 426"),
        "{response}"
    );

    // Unknown keys are rejected.
    let response = http_get(
        address,
        "/api/database/v1",
        "Authorization: Bearer wrong-key\r\n",
    )
    .await;
    assert!(response.starts_with("HTTP/1.1 401"), "{response}");

    server.abort();
    service.shutdown().unwrap();
}
