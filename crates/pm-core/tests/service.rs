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
        nameserver_address: "127.0.0.1:0".to_string(),
        dev_mode: true,
        allowed_clients: Vec::new(),
        packet_metrics_file: None,
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
    http_request("GET", address, path, headers).await
}

async fn http_request(
    method: &str,
    address: std::net::SocketAddr,
    path: &str,
    headers: &str,
) -> String {
    let mut stream = tokio::net::TcpStream::connect(address).await.unwrap();
    let message = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n{headers}\r\n"
    );
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
    // Firewall, split tunnel and custom list options are registered.
    for key in [
        "core/releaseChannel",
        "core/automaticUpdates",
        "core/automaticIntelUpdates",
        "filter/enable",
        "filter/askTimeout",
        "filter/customListFile",
        "splittun/enable",
        "splittun/use",
    ] {
        assert!(keys.contains(&key), "missing option {key}: {keys:?}");
    }

    // GeoIP country information is served from the static table.
    let response = http_get(address, "/api/v1/intel/geoip/countries", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    let body = response.split("\r\n\r\n").nth(1).expect("body");
    let countries: serde_json::Value = serde_json::from_str(body).expect("countries json");
    assert_eq!(countries["US"]["Name"], "United States of America");

    // The custom filter list endpoint reloads the configured file.
    let list_file = data_dir.join("custom-filter-list.txt");
    std::fs::write(&list_file, "example.com\n1.2.3.4\nUS\n").unwrap();
    service.custom_lists.set_file_path(list_file.to_str().unwrap());
    let response = http_request("POST", address, "/api/v1/customlists/update", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    assert!(
        response.contains("Custom filter list loaded successfully."),
        "{response}"
    );
    assert!(service.custom_lists.lists().is_loaded());

    // Update channels and automatic update switches are registered.
    let release_channel = options_json
        .as_array()
        .unwrap()
        .iter()
        .find(|option| option["Key"] == "core/releaseChannel")
        .expect("release channel option");
    assert_eq!(release_channel["DefaultValue"], "stable");
    assert_eq!(
        release_channel["PossibleValues"].as_array().unwrap().len(),
        4
    );

    // Both updaters exist; the binary updater checks the stable channel.
    assert_eq!(
        service.binary_updater.config().name,
        "Portmaster Binaries"
    );
    assert!(service.binary_updater.config().auto_check);
    assert!(!service.binary_updater.config().auto_download);
    assert!(service.binary_updater.config().needs_restart);
    assert_eq!(
        service.binary_updater.config().index_urls,
        vec!["https://updates.safing.io/stable.v3.json".to_string()]
    );
    assert!(service
        .api
        .endpoint("updates/check")
        .is_some());
    assert!(service.api.endpoint("updates/apply").is_some());
    assert!(service.api.endpoint("netquery/query").is_some());
    assert!(service.api.endpoint("netquery/query/batch").is_some());

    // The split tunnel interface environment is backed by netenv.
    use pm_splittun::InterfaceEnv;
    assert!(service
        .system_interfaces
        .best_physical_default_interfaces()
        .is_ok());
    assert!(service
        .system_interfaces
        .get_interface("definitely-not-an-interface")
        .is_err());

    // Firewall components are constructed even without root privileges.
    let firewall = &service.firewall;
    assert!(firewall.network.get_connection("does-not-exist").is_none());
    assert!(firewall.profiles.layered_for(&pm_process::Process::empty("1-0")).is_none());
    assert!(data_dir.join("databases/history.db").exists());
    assert!(service
        .databases
        .databases()
        .iter()
        .any(|info| info.name == "network"));

    // The synchronous prompt hook returns the Go timeout verdict.
    use pm_firewall::FirewallEnv;
    let mut conn = pm_network::Connection::default();
    firewall.env.prompt(&mut conn);
    assert_eq!(conn.verdict, pm_network::Verdict::Block);
    assert_eq!(
        conn.reason.msg,
        "prompting in progress, please respond to prompt"
    );
    assert!(firewall
        .env
        .is_my_ip("127.0.0.1".parse().unwrap())
        .expect("loopback is mine"));

    // GeoIP starts empty; the intel worker fills it from the index.
    assert!(!service.geoip.is_initialized(false));
    assert!(!service.geoip.is_initialized(true));
    assert_eq!(pm_geoip::V4_RESOURCE, "geoipv4.mmdb");
    assert_eq!(pm_geoip::V6_RESOURCE, "geoipv6.mmdb");

    // The network change monitor works against the real interfaces.
    assert!(service.network_monitor.check_once().is_ok());
    assert!(!service.split_proxies.is_ready(), "disabled by default");

    // The intel updater and filter lists module are wired.
    assert_eq!(service.intel_updater.config().name, "Portmaster Intel");
    assert!(service.intel_updater.config().verify.is_some());
    assert!(data_dir.join("intel").exists());
    assert!(!service.filter_lists.is_loaded(), "no cache yet");
    assert!(
        service
            .filter_lists_module
            .states()
            .export()
            .states
            .iter()
            .any(|state| state.id == "filterlists:disabled"),
        "initializing warning is shown"
    );

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

    // Notifications live in their own injected database.
    assert!(service
        .databases
        .databases()
        .iter()
        .any(|info| info.name == "notifications"));

    // The system status is published through the runtime database.
    let record = service
        .databases
        .internal_interface()
        .get("runtime:system/status")
        .unwrap();
    assert_eq!(record.key.key, "system/status");
    let status: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    // The online monitor may already have determined a status.
    let online_status = status["OnlineStatus"].as_u64().expect("online status");
    assert!(online_status <= 5, "valid status: {online_status}");
    assert_eq!(status["Modules"], json!([]));
    assert_eq!(status["WorstState"]["Module"], "");

    // Module states flow into the published status and into notifications.
    service
        .status
        .handle_module_status_update(pm_mgr::StateUpdate::new(
            "test-module",
            vec![pm_mgr::State::new(
                "test-module:warning",
                "Test Warning",
                pm_mgr::STATE_TYPE_WARNING,
            )],
        ));
    let record = service
        .databases
        .internal_interface()
        .get("runtime:system/status")
        .unwrap();
    let status: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    assert_eq!(status["WorstState"]["Module"], "test-module");
    assert_eq!(status["WorstState"]["Type"], "warning");
    assert!(service.notifications.get("test-module:warning").is_some());

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

    // The database API requires the configured admin key; the test client is
    // not a trusted process, so the process authenticator denies it.
    let response = http_get(address, "/api/database/v1", "").await;
    assert!(response.starts_with("HTTP/1.1 403"), "{response}");

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
    assert!(response.starts_with("HTTP/1.1 403"), "{response}");

    server.abort();
    service.shutdown().unwrap();
}

fn dns_query(name: &str, qtype: u16) -> Vec<u8> {
    let mut packet = vec![0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0];
    for label in name.trim_end_matches('.').split('.') {
        packet.push(label.len() as u8);
        packet.extend_from_slice(label.as_bytes());
    }
    packet.push(0);
    packet.extend_from_slice(&qtype.to_be_bytes());
    packet.extend_from_slice(&1u16.to_be_bytes());
    packet
}

#[tokio::test]
async fn nameserver_answers_internal_names() {
    let data_dir = temp_dir("dns");
    let service = Arc::new(Service::start(&test_options(&data_dir)).await.unwrap());
    let addrs = service.nameserver_addrs().await;
    assert!(!addrs.is_empty(), "nameserver is not listening");

    let client = tokio::net::UdpSocket::bind("127.0.0.1:0").await.unwrap();
    client
        .send_to(&dns_query("localhost.", 1), addrs[0])
        .await
        .unwrap();
    let mut buf = [0u8; 512];
    let (n, _) = tokio::time::timeout(Duration::from_secs(5), client.recv_from(&mut buf))
        .await
        .expect("no reply")
        .unwrap();
    let reply = &buf[..n];
    assert_eq!(&reply[..2], &[0x12, 0x34]);
    assert_ne!(reply[2] & 0x80, 0, "not a response");
    assert_eq!(reply[3] & 0x0f, 0, "rcode");
    assert_eq!(u16::from_be_bytes([reply[6], reply[7]]), 1, "answer count");
    assert_eq!(&reply[n - 4..], &[127, 0, 0, 1]);

    // The resolver endpoints are registered.
    let server = tokio::spawn(Arc::clone(&service).run());
    let response = http_get(service.local_addr(), "/api/v1/dns/resolvers", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");

    server.abort();
    let _ = server.await;
    service.dns.shutdown().await;
}

#[tokio::test]
async fn busy_listen_port_notifies_and_keeps_api_up() {
    let data_dir = temp_dir("dns-busy");
    let blocker = std::net::UdpSocket::bind("127.0.0.1:0").unwrap();
    let mut options = test_options(&data_dir);
    options.nameserver_address = blocker.local_addr().unwrap().to_string();

    let service = Arc::new(Service::start(&options).await.unwrap());
    assert!(service.nameserver_addrs().await.is_empty());
    let notification = service
        .notifications
        .all()
        .into_iter()
        .map(|handle| handle.snapshot())
        .find(|n| n.event_id.starts_with("nameserver:"))
        .expect("no listener failure notification");
    assert_eq!(notification.type_, pm_notifications::TYPE_ERROR);

    let server = tokio::spawn(Arc::clone(&service).run());
    let response = http_get(service.local_addr(), "/api/v1/ping", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");

    server.abort();
    let _ = server.await;
    service.dns.shutdown().await;
}

#[tokio::test]
async fn restart_endpoint_requests_a_restart() {
    let data_dir = temp_dir("restart");
    let options = test_options(&data_dir);
    let service = Arc::new(Service::start(&options).await.unwrap());
    let address = service.local_addr();
    let mut server = tokio::spawn(Arc::clone(&service).run());

    let response = http_request("POST", address, "/api/v1/core/restart", "").await;
    assert!(response.starts_with("HTTP/1.1 200"), "{response}");
    assert!(response.ends_with("restart initiated\n"), "{response}");

    // The updater callback uses the same token; both paths stop the service.
    let result = tokio::time::timeout(std::time::Duration::from_secs(2), &mut server)
        .await
        .expect("service stops on restart request")
        .expect("service task");
    assert!(result.is_ok(), "service stopped cleanly: {result:?}");
    assert!(service.restart_token.is_cancelled());
    let _ = std::fs::remove_dir_all(&data_dir);
}
