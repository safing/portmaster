//! CRUD tests for subscriptions and their parsed nodes.

use std::sync::Arc;

use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use pm_database::{Databases, MemoryStorage};
use pm_xray::model::{AddSubscriptionRequest, UpdateSubscriptionRequest};
use pm_xray::{SubscriptionFetcher, SubscriptionStore, XRAY_DB};

struct StaticFetcher(String);
impl SubscriptionFetcher for StaticFetcher {
    fn fetch(&self, _url: &str) -> std::result::Result<String, String> {
        Ok(self.0.clone())
    }
}

fn store() -> SubscriptionStore {
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected(XRAY_DB, "test").unwrap();
    databases
        .inject(XRAY_DB, Box::new(MemoryStorage::new()))
        .unwrap();
    SubscriptionStore::new(&databases).unwrap()
}

#[test]
fn subscription_crud_and_refresh() {
    let store = store();
    assert!(store.list().unwrap().is_empty());

    let created = store
        .add(&AddSubscriptionRequest {
            name: "My VPN".into(),
            url: "https://example.com/sub".into(),
            enabled: true,
        })
        .unwrap();
    assert_eq!(store.list().unwrap().len(), 1);
    assert_eq!(store.get(&created.id).unwrap().name, "My VPN");

    let link = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443#DE-Node";
    let fetcher = StaticFetcher(STANDARD.encode(link.as_bytes()));
    assert_eq!(store.refresh(&created.id, &fetcher, None).unwrap(), 1);
    let nodes = store.list_nodes().unwrap();
    assert_eq!(nodes.len(), 1);
    assert_eq!(nodes[0].country_code, "DE");
    assert_eq!(store.get(&created.id).unwrap().node_count, 1);
    assert!(store.get(&created.id).unwrap().last_updated > 0);

    store
        .set_active(&created.id, &nodes[0].id)
        .unwrap();
    assert_eq!(store.active_node_id().as_deref(), Some(nodes[0].id.as_str()));

    let updated = store
        .update(&UpdateSubscriptionRequest {
            id: created.id.clone(),
            name: Some("Renamed".into()),
            url: None,
            enabled: Some(false),
        })
        .unwrap();
    assert_eq!(updated.name, "Renamed");
    assert!(!updated.enabled);
    assert_eq!(store.get(&created.id).unwrap().name, "Renamed");

    store.remove(&created.id).unwrap();
    assert!(store.list().unwrap().is_empty());
    assert!(store.list_nodes().unwrap().is_empty());
    assert!(store.active_node_id().is_none());
    assert!(store.get(&created.id).is_err());
}

#[test]
fn replaces_nodes_and_removes_stale_ones() {
    let store = store();
    let created = store
        .add(&AddSubscriptionRequest {
            name: "Second".into(),
            url: "https://example.com/sub2".into(),
            enabled: true,
        })
        .unwrap();

    let first = "trojan://secret@trojan.example:443#US-Node";
    let fetcher = StaticFetcher(STANDARD.encode(first.as_bytes()));
    store.refresh(&created.id, &fetcher, None).unwrap();
    assert_eq!(store.list_nodes().unwrap().len(), 1);

    let second = "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ@ss.example:8388#FR-Node";
    let fetcher = StaticFetcher(STANDARD.encode(second.as_bytes()));
    store.refresh(&created.id, &fetcher, None).unwrap();
    let nodes = store.list_nodes().unwrap();
    assert_eq!(nodes.len(), 1);
    assert_eq!(nodes[0].protocol, pm_xray::Protocol::Shadowsocks);
}

#[test]
fn direct_links_refresh_without_fetcher() {
    let store = store();
    let link = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443?type=ws&security=tls#NL-Direct";
    let created = store
        .add(&AddSubscriptionRequest {
            name: "Direct NL".into(),
            url: link.into(),
            enabled: true,
        })
        .unwrap();

    // Passing a failing fetcher should NOT prevent direct link refresh
    struct FailingFetcher;
    impl SubscriptionFetcher for FailingFetcher {
        fn fetch(&self, _url: &str) -> std::result::Result<String, String> {
            Err("should not call network fetcher for direct links".into())
        }
    }

    let count = store.refresh(&created.id, &FailingFetcher, None).unwrap();
    assert_eq!(count, 1);
    let nodes = store.list_nodes().unwrap();
    assert_eq!(nodes.len(), 1);
    assert_eq!(nodes[0].address, "example.com");
    assert_eq!(nodes[0].country_code, "NL");
}

#[test]
fn sing_box_and_xray_json_subscription_payloads() {
    use pm_xray::subscription::decode_payload;

    let singbox_json = r#"{
      "outbounds": [
        {
          "type": "vless",
          "tag": "US-SingBox",
          "server": "sb.example.com",
          "server_port": 443,
          "uuid": "00010203-0405-0607-0809-0a0b0c0d0e0f",
          "tls": { "enabled": true, "server_name": "sb.example.com" }
        },
        {
          "type": "hysteria2",
          "tag": "DE-Hy2",
          "server": "hy2.example.com",
          "server_port": 8443,
          "password": "secret_hy2_password"
        }
      ]
    }"#;

    let decoded = decode_payload(singbox_json).unwrap();
    assert_eq!(decoded.len(), 2);
    assert!(decoded[0].starts_with("vless://"));
    assert!(decoded[1].starts_with("hysteria2://"));

    let xray_json = r#"{
      "outbounds": [
        {
          "protocol": "trojan",
          "tag": "JP-Xray",
          "settings": {
            "servers": [
              {
                "address": "xray.example.com",
                "port": 443,
                "password": "xray_password"
              }
            ]
          }
        }
      ]
    }"#;

    let decoded_xray = decode_payload(xray_json).unwrap();
    assert_eq!(decoded_xray.len(), 1);
    assert!(decoded_xray[0].starts_with("trojan://"));
}

