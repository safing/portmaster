//! API endpoints of the Xray integration.
//!
//! Reads require `PERMIT_USER`, writes require `PERMIT_ADMIN` (matching the
//! pause/resume and config-change endpoints of the core). Like the Go API, one
//! endpoint may serve a read and a write method; the handler dispatches on the
//! effective HTTP method.

use std::sync::Arc;

use pm_api::{Api, Endpoint, PERMIT_ADMIN, PERMIT_USER};
use serde_json::json;

use crate::model::{AddSubscriptionRequest, ConnectRequest, UpdateSubscriptionRequest};
use crate::Xray;

impl Xray {
    /// Registers all Xray endpoints on the API.
    pub fn register_api(self: &Arc<Self>, api: &Api) -> pm_api::Result<()> {
        register_api(self, api)
    }
}

/// Removes userinfo and query parameters from a subscription URL so that
/// credentials never appear in an API reply or a UI tooltip.
fn redact_url(url: &str) -> String {
    let Some((scheme, rest)) = url.split_once("://") else {
        return url.split('?').next().unwrap_or(url).to_string();
    };
    let host = rest.rsplit('@').next().unwrap_or(rest);
    let host = host.split('?').next().unwrap_or(host);
    format!("{scheme}://{host}")
}

/// Registers the Xray endpoints on `api`.
pub fn register_api(xray: &Arc<Xray>, api: &Api) -> pm_api::Result<()> {
    // GET: list. POST: add. PUT: update/refresh. DELETE: remove.
    let subscriptions = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/subscriptions",
            "Xray Subscriptions",
            "Lists, adds, updates and removes Xray subscription URLs.",
        )
        .with_read(PERMIT_USER)
        .with_write(PERMIT_ADMIN)
        .with_struct(move |request| {
            if request.read_method {
                let items = subscriptions.store.list().map_err(crate::Error::api)?;
                // The Angular UI expects a plain array. Never expose
                // credentials embedded in the subscription URL.
                let items = items
                    .into_iter()
                    .map(|mut item| {
                        item.url = redact_url(&item.url);
                        item
                    })
                    .collect::<Vec<_>>();
                return serde_json::to_value(items).map_err(pm_api::ApiError::Json);
            }
            match request.effective_method.as_str() {
                "POST" => {
                    let payload: AddSubscriptionRequest = request.body_json()?;
                    let mut subscription = subscriptions
                        .store
                        .add(&payload)
                        .map_err(crate::Error::api)?;
                    if subscription.enabled {
                        subscriptions.refresh(&subscription.id, None);
                    }
                    subscription.url = redact_url(&subscription.url);
                    serde_json::to_value(subscription).map_err(pm_api::ApiError::Json)
                }
                "PUT" => {
                    let payload: UpdateSubscriptionRequest = request.body_json()?;
                    let id = payload.id.clone();
                    if payload.name.is_some() || payload.url.is_some() || payload.enabled.is_some() {
                        subscriptions
                            .store
                            .update(&payload)
                            .map_err(crate::Error::api)?;
                    }
                    let refreshed = subscriptions.refresh(&id, None);
                    serde_json::to_value(json!({ "refreshed": refreshed }))
                        .map_err(pm_api::ApiError::Json)
                }
                "DELETE" => {
                    let id = request
                        .param("id")
                        .or_else(|| request.query_param("id"))
                        .unwrap_or_default();
                    let id = if id.is_empty() {
                        let payload: UpdateSubscriptionRequest = request.body_json()?;
                        payload.id
                    } else {
                        id.to_string()
                    };
                    subscriptions.store.remove(&id).map_err(crate::Error::api)?;
                    serde_json::to_value(json!({ "removed": id })).map_err(pm_api::ApiError::Json)
                }
                method => Err(pm_api::ApiError::WithStatus {
                    status: 405,
                    message: format!("method {method} is not supported"),
                }),
            }
        }),
    )?;

    // The UI deletes by path segment: DELETE xray/subscriptions/<id>.
    let remove_by_path = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/subscriptions/{id}",
            "Remove Xray Subscription",
            "Removes a subscription by ID.",
        )
        .with_write(PERMIT_ADMIN)
        .with_write_method("DELETE")
        .with_struct(move |request| {
            let id = request.param("id").unwrap_or_default();
            remove_by_path
                .store
                .remove(id)
                .map_err(crate::Error::api)?;
            serde_json::to_value(json!({ "removed": id })).map_err(pm_api::ApiError::Json)
        }),
    )?;

    let nodes = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/nodes",
            "List Xray Nodes",
            "Returns all discovered nodes, including their countries, for the map.",
        )
        .with_read(PERMIT_USER)
        .with_struct(move |_| {
            let items = nodes.store.list_nodes().map_err(crate::Error::api)?;
            // The Angular UI expects a plain array. `raw_config` contains the
            // full share link (UUIDs/passwords) and must never leave the core.
            let items = items
                .iter()
                .filter_map(|node| {
                    let mut value = serde_json::to_value(node).ok()?;
                    if let Some(object) = value.as_object_mut() {
                        object.remove("raw_config");
                    }
                    Some(value)
                })
                .collect::<Vec<_>>();
            serde_json::to_value(items).map_err(pm_api::ApiError::Json)
        }),
    )?;

    // Endpoint for adding manual/direct proxy links without an HTTP subscription URL
    let manual_nodes = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/nodes/manual",
            "Add Manual Xray Nodes",
            "Adds direct VLESS, Trojan, Hysteria2, Shadowsocks or JSON links without an HTTP subscription.",
        )
        .with_write(PERMIT_ADMIN)
        .with_struct(move |request| {
            let payload: serde_json::Value = request.body_json()?;
            let content = payload
                .get("links")
                .or_else(|| payload.get("link"))
                .or_else(|| payload.get("url"))
                .or_else(|| payload.get("content"))
                .and_then(|v| v.as_str())
                .unwrap_or_default();

            if content.trim().is_empty() {
                return Err(pm_api::ApiError::WithStatus {
                    status: 400,
                    message: "links or url is required".into(),
                });
            }

            let name = payload
                .get("name")
                .and_then(|v| v.as_str())
                .unwrap_or("Manual Nodes");

            let sub_req = AddSubscriptionRequest {
                name: name.to_string(),
                url: content.trim().to_string(),
                enabled: true,
            };
            let subscription = manual_nodes.store.add(&sub_req).map_err(crate::Error::api)?;
            manual_nodes.refresh(&subscription.id, None);
            let created_nodes = manual_nodes
                .store
                .list_nodes_for_subscription(&subscription.id)
                .map_err(crate::Error::api)?;

            let safe_nodes = created_nodes
                .iter()
                .filter_map(|n| {
                    let mut val = serde_json::to_value(n).ok()?;
                    if let Some(obj) = val.as_object_mut() {
                        obj.remove("raw_config");
                    }
                    Some(val)
                })
                .collect::<Vec<_>>();

            serde_json::to_value(json!({
                "subscriptionId": subscription.id,
                "nodeCount": safe_nodes.len(),
                "nodes": safe_nodes
            }))
            .map_err(pm_api::ApiError::Json)
        }),
    )?;

    let connect = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/connect",
            "Connect to Xray Node",
            "Starts Xray with an outbound for the selected node.",
        )
        .with_write(PERMIT_ADMIN)
        .with_struct(move |request| {
            if !connect.enabled {
                return Err(crate::Error::Disabled.api());
            }
            let payload: ConnectRequest = request.body_json()?;
            let node = connect.store.get_node(&payload.node_id).map_err(crate::Error::api)?;
            connect
                .manager
                .connect(
                    &node,
                    payload.inbound_port.unwrap_or(connect.inbound_port),
                    connect.inbound,
                )
                .map_err(crate::Error::api)?;
            connect
                .store
                .set_active(&node.subscription_id, &node.id)
                .map_err(crate::Error::api)?;
            let inbound_port = payload.inbound_port.unwrap_or(connect.inbound_port);
            let _ = connect.config.set_config_option("spn/enable", serde_json::Value::Bool(true));
            let _ = connect.config.set_config_option(
                "splittun/proxy",
                serde_json::Value::String(format!("socks5://127.0.0.1:{inbound_port}")),
            );
            let probe_xray = Arc::clone(&connect);
            std::thread::spawn(move || {
                std::thread::sleep(std::time::Duration::from_millis(300));
                probe_xray.manager.probe_exit_ip(Some(&probe_xray.geoip));
            });
            serde_json::to_value(connect.manager.status()).map_err(pm_api::ApiError::Json)
        }),
    )?;

    let disconnect = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/disconnect",
            "Disconnect Xray Node",
            "Stops the Xray process.",
        )
        .with_write(PERMIT_ADMIN)
        .with_struct(move |_| {
            disconnect.manager.disconnect().map_err(crate::Error::api)?;
            disconnect.store.clear_active();
            let _ = disconnect.config.set_config_option("spn/enable", serde_json::Value::Bool(false));
            let _ = disconnect.config.set_config_option("splittun/proxy", serde_json::Value::String(String::new()));
            serde_json::to_value(disconnect.manager.status()).map_err(pm_api::ApiError::Json)
        }),
    )?;

    let status = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/status",
            "Xray Status",
            "Returns the state of the Xray process, its selected node and live traffic counters.",
        )
        .with_read(PERMIT_USER)
        .with_struct(move |_| {
            serde_json::to_value(status.status()).map_err(pm_api::ApiError::Json)
        }),
    )?;

    let stats = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/stats",
            "Xray Traffic Statistics",
            "Returns traffic counters from Xray's gRPC StatsService (GetStats/QueryStats).",
        )
        .with_read(PERMIT_USER)
        .with_struct(move |request| {
            let name = request.query_param("name").unwrap_or("proxy");
            let pattern = request.query_param("pattern").unwrap_or(">>>traffic>>>");
            let available = stats.stats.available();
            let mut counters = Vec::new();
            let mut error = serde_json::Value::Null;
            match stats.stats.query_stats(pattern) {
                Ok(list) => counters = list,
                Err(failure) => error = json!(failure.to_string()),
            }
            let node = match stats.stats.traffic(name) {
                Ok(snapshot) => {
                    json!({ "uplink": snapshot.uplink, "downlink": snapshot.downlink })
                }
                Err(failure) => {
                    if error.is_null() {
                        error = json!(failure);
                    }
                    serde_json::Value::Null
                }
            };
            // Sum the same counter set for a total view; non-positive values
            // (e.g. reset counters) are ignored.
            let mut uplink = 0u64;
            let mut downlink = 0u64;
            for counter in &counters {
                if counter.name.ends_with(">>>uplink") {
                    uplink = uplink.saturating_add(counter.value.max(0) as u64);
                } else if counter.name.ends_with(">>>downlink") {
                    downlink = downlink.saturating_add(counter.value.max(0) as u64);
                }
            }
            serde_json::to_value(json!({
                "available": available,
                "name": name,
                "node": node,
                "total": { "uplink": uplink, "downlink": downlink },
                "counters": counters,
                "error": error,
            }))
            .map_err(pm_api::ApiError::Json)
        }),
    )?;

    Ok(())
}
