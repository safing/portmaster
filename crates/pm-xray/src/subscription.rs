//! Subscription storage, HTTP fetching and payload decoding.

use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use std::io::Read;
use std::sync::Arc;

use base64::engine::general_purpose::{STANDARD, STANDARD_NO_PAD, URL_SAFE, URL_SAFE_NO_PAD};
use base64::Engine;
use pm_database::{DatabaseInfo, Databases, Interface, Key, Meta, Options, Query, Record};
use pm_geoip::GeoIp;

use crate::model::{AddSubscriptionRequest, Subscription, UpdateSubscriptionRequest, XrayNode};
use crate::parser::{node_id, parse_profile, percent_decode};
use crate::{Error, Result};

/// Name of the database holding subscriptions, parsed nodes and the active node.
pub const XRAY_DB: &str = "xray";
const SUBSCRIPTION_PREFIX: &str = "subscription:";
const NODE_PREFIX: &str = "node:";
const ACTIVE_KEY: &str = "active";

const USER_AGENT: &str = "Portmaster Xray";

fn id_for(url: &str) -> String {
    let mut hasher = DefaultHasher::new();
    url.hash(&mut hasher);
    format!("{:016x}", hasher.finish())
}

/// Database-local key of a subscription record (without the `db:` prefix).
fn subscription_key(id: &str) -> String {
    format!("{SUBSCRIPTION_PREFIX}{id}")
}

/// Database-local key of a node record.
fn node_key(subscription_id: &str, node_id: &str) -> String {
    format!("{NODE_PREFIX}{subscription_id}:{node_id}")
}

fn full_key(local: &str) -> String {
    format!("{XRAY_DB}:{local}")
}

/// Abstraction over HTTP fetching, so refresh can be tested offline.
pub trait SubscriptionFetcher: Send + Sync {
    fn fetch(&self, url: &str) -> std::result::Result<String, String>;
}

/// Blocking HTTPS client based on `ureq`, the client already used by the
/// update module. API handlers are synchronous, so a blocking client avoids
/// nesting a Tokio runtime inside the core server.
pub struct HttpFetcher {
    agent: ureq::Agent,
}

impl Default for HttpFetcher {
    fn default() -> Self {
        Self::new()
    }
}

impl HttpFetcher {
    pub fn new() -> Self {
        HttpFetcher {
            agent: ureq::Agent::new_with_defaults(),
        }
    }
}

/// Extracts per-subscription request settings from the URL.
///
/// * `https://user:pass@host/sub` sends HTTP Basic authorization.
/// * `?header-X-Name=value` sends a custom `X-Name: value` header. The query
///   parameter is removed from the request URL.
///
/// This keeps credentials and headers in the single URL the UI already
/// stores, without changing the subscription database schema.
fn subscription_request(url: &str) -> (String, Vec<(String, String)>) {
    let mut headers = Vec::new();
    let (mut url, basic) = match url.split_once("://") {
        Some((scheme, rest)) => match rest.split_once('@') {
            Some((userinfo, endpoint))
                if !userinfo.contains(['/', '?', '#'])
                    && !endpoint.is_empty()
                    && userinfo.contains(':') =>
            {
                let credential = STANDARD.encode(percent_decode(userinfo).as_bytes());
                (
                    format!("{scheme}://{endpoint}"),
                    Some(("Authorization".to_string(), format!("Basic {credential}"))),
                )
            }
            _ => (url.to_string(), None),
        },
        None => (url.to_string(), None),
    };
    if let Some(header) = basic {
        headers.push(header);
    }
    if let Some((base, query)) = url.split_once('?') {
        let mut kept = Vec::new();
        for item in query.split('&') {
            if let Some((name, value)) = item.split_once('=') {
                if let Some(header) = name.strip_prefix("header-") {
                    if !header.is_empty() {
                        headers.push((percent_decode(header), percent_decode(value)));
                        continue;
                    }
                }
            }
            kept.push(item);
        }
        url = if kept.is_empty() {
            base.to_string()
        } else {
            format!("{base}?{}", kept.join("&"))
        };
    }
    (url, headers)
}

impl SubscriptionFetcher for HttpFetcher {
    fn fetch(&self, url: &str) -> std::result::Result<String, String> {
        let (url, headers) = subscription_request(url);
        let mut request = self.agent.get(&url).header("User-Agent", USER_AGENT);
        for (name, value) in headers {
            request = request.header(name, value);
        }
        let mut response = request
            .call()
            .map_err(|error| format!("request failed: {error}"))?;
        let mut data = Vec::new();
        response
            .body_mut()
            .as_reader()
            .read_to_end(&mut data)
            .map_err(|error| format!("failed to read body: {error}"))?;
        if data.len() > 16 * 1024 * 1024 {
            return Err("subscription is too large (over 16 MiB)".to_string());
        }
        Ok(String::from_utf8_lossy(&data).into_owned())
    }
}

/// Decodes a subscription body into individual share links. Accepts a plain
/// newline-separated list, Base64 (standard or URL-safe, with or without
/// padding) and Base64 with surrounding whitespace.
pub fn decode_payload(payload: &str) -> std::result::Result<Vec<String>, String> {
    let trimmed = payload.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    // A subscription may ship a raw wg-quick file instead of a link list.
    // Wrap it in the wireguard:// form the parser understands.
    if trimmed.contains("[Interface]") && trimmed.contains("[Peer]") {
        return Ok(vec![format!(
            "wireguard://{}",
            URL_SAFE_NO_PAD.encode(trimmed.as_bytes())
        )]);
    }

    let direct: Vec<String> = trimmed
        .lines()
        .map(str::trim)
        .filter(|line| line.contains("://"))
        .map(str::to_string)
        .collect();
    if !direct.is_empty() {
        return Ok(direct);
    }

    let compact: String = trimmed.chars().filter(|ch| !ch.is_whitespace()).collect();
    let mut padded = compact.clone();
    while !padded.len().is_multiple_of(4) {
        padded.push('=');
    }
    let mut decoded = None;
    for input in [&compact, &padded] {
        for engine in [URL_SAFE_NO_PAD, URL_SAFE, STANDARD_NO_PAD, STANDARD] {
            if let Ok(bytes) = engine.decode(input.as_bytes()) {
                decoded = Some(bytes);
                break;
            }
        }
        if decoded.is_some() {
            break;
        }
    }
    let bytes = decoded.ok_or_else(|| "payload is neither a link list nor Base64".to_string())?;
    let text = String::from_utf8_lossy(&bytes);
    let links: Vec<String> = text
        .lines()
        .map(str::trim)
        .filter(|line| line.contains("://"))
        .map(str::to_string)
        .collect();
    if links.is_empty() {
        return Err("payload contains no profiles".to_string());
    }
    Ok(links)
}

/// CRUD and node storage on top of the `xray` database.
pub struct SubscriptionStore {
    db: Interface,
}

impl SubscriptionStore {
    /// Creates the store, registering the `xray` database if needed.
    pub fn new(databases: &Arc<Databases>) -> Result<Self> {
        if !databases.is_registered(XRAY_DB) {
            databases.register(DatabaseInfo {
                name: XRAY_DB.to_string(),
                description: "Xray subscriptions and discovered nodes".to_string(),
                storage_type: "sqlite".to_string(),
                shadow_delete: false,
            })?;
        }
        Ok(SubscriptionStore {
            db: databases.interface(Options {
                local: true,
                internal: true,
                ..Options::default()
            }),
        })
    }

    pub fn list(&self) -> Result<Vec<Subscription>> {
        let query = Query::new(&format!("{XRAY_DB}:{SUBSCRIPTION_PREFIX}"));
        let mut subscriptions: Vec<Subscription> = self
            .db
            .query(&query)?
            .into_iter()
            .filter_map(|record| record.decode::<Subscription>().ok())
            .collect();
        subscriptions.sort_by(|a, b| a.name.cmp(&b.name).then(a.id.cmp(&b.id)));
        Ok(subscriptions)
    }

    pub fn get(&self, id: &str) -> Result<Subscription> {
        let record = self.db.get(&full_key(&subscription_key(id)))?;
        Ok(record.decode::<Subscription>()?)
    }

    pub fn add(&self, request: &AddSubscriptionRequest) -> Result<Subscription> {
        if request.url.trim().is_empty() {
            return Err(Error::InvalidRequest("url is required".into()));
        }
        let id = id_for(&request.url);
        // The UI sends only the URL; use a readable derived name.
        let name = if request.name.trim().is_empty() {
            format!("Subscription {id}")
        } else {
            request.name.trim().to_string()
        };
        let subscription = Subscription {
            id: id.clone(),
            name,
            url: request.url.trim().to_string(),
            enabled: request.enabled,
            last_updated: 0,
            node_count: 0,
        };
        self.save(&subscription)?;
        Ok(subscription)
    }

    pub fn update(&self, request: &UpdateSubscriptionRequest) -> Result<Subscription> {
        if request.id.is_empty() {
            return Err(Error::InvalidRequest("id is required".into()));
        }
        let mut subscription = self.get(&request.id)?;
        if let Some(name) = &request.name {
            if name.trim().is_empty() {
                return Err(Error::InvalidRequest("name must not be empty".into()));
            }
            subscription.name = name.trim().to_string();
        }
        if let Some(url) = &request.url {
            if url.trim().is_empty() {
                return Err(Error::InvalidRequest("url must not be empty".into()));
            }
            subscription.url = url.trim().to_string();
        }
        if let Some(enabled) = request.enabled {
            subscription.enabled = enabled;
        }
        self.save(&subscription)?;
        Ok(subscription)
    }

    pub fn remove(&self, id: &str) -> Result<()> {
        self.get(id)?;
        self.remove_nodes(id)?;
        if self
            .active_raw()
            .and_then(|value| value.split_once(':').map(|(subscription, _)| subscription.to_string()))
            .as_deref()
            == Some(id)
        {
            self.clear_active();
        }
        self.db.delete(&full_key(&subscription_key(id)))?;
        Ok(())
    }

    /// Refreshes one or all enabled subscriptions and returns the number of
    /// discovered nodes. Parsing failures of individual links are skipped.
    pub fn refresh(
        &self,
        id: &str,
        fetcher: &dyn SubscriptionFetcher,
        geoip: Option<&GeoIp>,
    ) -> Result<usize> {
        let subscriptions = if id.is_empty() {
            self.list()?
                .into_iter()
                .filter(|subscription| subscription.enabled)
                .collect::<Vec<_>>()
        } else {
            vec![self.get(id)?]
        };
        let mut total = 0;
        for mut subscription in subscriptions {
            let body = match fetcher.fetch(&subscription.url) {
                Ok(body) => body,
                Err(error) => {
                    tracing::warn!(%error, id = %subscription.id, "xray: failed to refresh subscription");
                    continue;
                }
            };
            let links = match decode_payload(&body) {
                Ok(links) => links,
                Err(error) => {
                    tracing::warn!(%error, id = %subscription.id, "xray: failed to decode subscription payload");
                    continue;
                }
            };
            let mut nodes = Vec::new();
            for link in links {
                match parse_profile(&link, &subscription.id, geoip) {
                    Ok(node) => nodes.push(node),
                    Err(error) => tracing::debug!(%error, "xray: skipping profile"),
                }
            }
            self.replace_nodes(&subscription.id, &nodes)?;
            subscription.last_updated = crate::now();
            subscription.node_count = nodes.len();
            total += nodes.len();
            self.save(&subscription)?;
        }
        Ok(total)
    }

    pub fn list_nodes(&self) -> Result<Vec<XrayNode>> {
        let query = Query::new(&format!("{XRAY_DB}:{NODE_PREFIX}"));
        let mut nodes: Vec<XrayNode> = self
            .db
            .query(&query)?
            .into_iter()
            .filter_map(|record| record.decode::<XrayNode>().ok())
            .collect();
        nodes.sort_by(|a, b| a.name.cmp(&b.name).then(a.id.cmp(&b.id)));
        Ok(nodes)
    }

    /// Deletes every node belonging to a subscription.
    pub fn remove_nodes(&self, subscription_id: &str) -> Result<()> {
        let prefix = format!("{XRAY_DB}:{NODE_PREFIX}{subscription_id}:");
        for record in self.db.query(&Query::new(&prefix))? {
            self.db.delete(&full_key(&record.key.key))?;
        }
        Ok(())
    }

    pub fn get_node(&self, id: &str) -> Result<XrayNode> {
        self.list_nodes()?
            .into_iter()
            .find(|node| node.id == id)
            .ok_or_else(|| Error::NodeNotFound(id.to_string()))
    }

    /// Replaces all nodes of a subscription, deleting nodes that disappeared.
    pub fn replace_nodes(&self, subscription_id: &str, nodes: &[XrayNode]) -> Result<()> {
        let prefix = format!("{XRAY_DB}:{NODE_PREFIX}{subscription_id}:");
        let old: Vec<String> = self
            .db
            .query(&Query::new(&prefix))?
            .into_iter()
            .map(|record| record.key.key)
            .collect();
        for node in nodes {
            let key = Key::new(XRAY_DB, &node_key(&node.subscription_id, &node.id));
            let record = Record::from_typed(key, Meta::default(), node)?;
            self.db.put_new(record)?;
        }
        let wanted: Vec<String> = nodes
            .iter()
            .map(|node| node_key(subscription_id, &node.id))
            .collect();
        for key in old {
            if !wanted.contains(&key) {
                self.db.delete(&format!("{XRAY_DB}:{key}"))?;
            }
        }
        Ok(())
    }

    pub fn set_active(&self, subscription_id: &str, node_id: &str) -> Result<()> {
        let record = Record::from_typed(
            Key::new(XRAY_DB, ACTIVE_KEY),
            Meta::default(),
            &format!("{subscription_id}:{node_id}"),
        )?;
        Ok(self.db.put_new(record)?)
    }

    fn active_raw(&self) -> Option<String> {
        self.db
            .get(&full_key(ACTIVE_KEY))
            .ok()
            .and_then(|record| record.decode::<String>().ok())
    }

    pub fn active_node_id(&self) -> Option<String> {
        self.active_raw()
            .and_then(|value| value.split_once(':').map(|(_, node)| node.to_string()))
    }

    pub fn clear_active(&self) {
        let _ = self.db.delete(&full_key(ACTIVE_KEY));
    }

    fn save(&self, subscription: &Subscription) -> Result<()> {
        let key = Key::new(XRAY_DB, &subscription_key(&subscription.id));
        let record = Record::from_typed(key, Meta::default(), subscription)?;
        Ok(self.db.put(record)?)
    }
}

/// Placeholder node ID helper re-exported for callers that need to correlate a
/// raw link with a stored node.
pub fn id_for_raw(subscription_id: &str, raw: &str) -> String {
    node_id(subscription_id, raw)
}

#[cfg(test)]
mod tests {
    use super::*;

    struct StaticFetcher(String);
    impl SubscriptionFetcher for StaticFetcher {
        fn fetch(&self, _url: &str) -> std::result::Result<String, String> {
            Ok(self.0.clone())
        }
    }

    fn store() -> (Arc<Databases>, SubscriptionStore) {
        let databases = Arc::new(Databases::without_disk());
        databases.register_injected(XRAY_DB, "test").unwrap();
        databases
            .inject(
                XRAY_DB,
                Box::new(pm_database::MemoryStorage::new()),
            )
            .unwrap();
        let store = SubscriptionStore::new(&databases).unwrap();
        (databases, store)
    }

    #[test]
    fn decodes_raw_wireguard_config_from_a_subscription() {
        let raw = "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0+P0A=\nEndpoint = wg.example.com:51820\nAllowedIPs = 0.0.0.0/0\n";
        let links = decode_payload(raw).unwrap();
        assert_eq!(links.len(), 1);
        let node = crate::parser::parse_profile(&links[0], "sub", None).unwrap();
        assert_eq!(node.protocol, crate::Protocol::Wireguard);
        assert_eq!(node.address, "wg.example.com");
        assert_eq!(node.port, 51820);
    }

    #[test]
    fn extracts_basic_auth_and_custom_headers_from_subscription_url() {
        let (url, headers) = subscription_request(
            "https://user:pass%40example@sub.example.com/sub?header-X-Token=abc%20123&type=base64",
        );
        assert_eq!(url, "https://sub.example.com/sub?type=base64");
        assert_eq!(
            headers,
            vec![
                (
                    "Authorization".to_string(),
                    format!("Basic {}", STANDARD.encode("user:pass@example")),
                ),
                ("X-Token".to_string(), "abc 123".to_string()),
            ]
        );
        let (plain, headers) = subscription_request("https://sub.example.com/sub");
        assert_eq!(plain, "https://sub.example.com/sub");
        assert!(headers.is_empty());
    }

    #[test]
    fn decodes_plain_and_base64() {
        let plain = "vless://id@example.com:443#One\nvmess://abc#Two";
        assert_eq!(decode_payload(plain).unwrap(), vec!["vless://id@example.com:443#One", "vmess://abc#Two"]);
        let encoded = STANDARD.encode(plain.as_bytes());
        assert_eq!(decode_payload(&encoded).unwrap(), decode_payload(plain).unwrap());
    }

    #[test]
    fn refreshes_and_lists_nodes() {
        let (_databases, store) = store();
        let subscription = store
            .add(&AddSubscriptionRequest {
                name: "Test".into(),
                url: "https://example.com/sub".into(),
                enabled: true,
            })
            .unwrap();
        let link = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443#DE-Node";
        let encoded = STANDARD.encode(link.as_bytes());
        let fetcher = StaticFetcher(encoded);
        assert_eq!(store.refresh(&subscription.id, &fetcher, None).unwrap(), 1);
        let nodes = store.list_nodes().unwrap();
        assert_eq!(nodes.len(), 1);
        assert_eq!(nodes[0].country_code, "DE");
        assert_eq!(store.get(&subscription.id).unwrap().node_count, 1);
        store.remove(&subscription.id).unwrap();
        assert!(store.list_nodes().unwrap().is_empty());
    }
}
