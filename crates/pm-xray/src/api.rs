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
                // The Angular UI expects a plain array.
                return serde_json::to_value(items).map_err(pm_api::ApiError::Json);
            }
            match request.effective_method.as_str() {
                "POST" => {
                    let payload: AddSubscriptionRequest = request.body_json()?;
                    let subscription = subscriptions
                        .store
                        .add(&payload)
                        .map_err(crate::Error::api)?;
                    if subscription.enabled {
                        subscriptions.refresh(&subscription.id, None);
                    }
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
            // The Angular UI expects a plain array.
            serde_json::to_value(items).map_err(pm_api::ApiError::Json)
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
            serde_json::to_value(disconnect.manager.status()).map_err(pm_api::ApiError::Json)
        }),
    )?;

    let status = Arc::clone(xray);
    api.register_endpoint(
        Endpoint::new(
            "xray/status",
            "Xray Status",
            "Returns the state of the Xray process and its selected node.",
        )
        .with_read(PERMIT_USER)
        .with_struct(move |_| {
            serde_json::to_value(status.manager.status()).map_err(pm_api::ApiError::Json)
        }),
    )?;

    Ok(())
}
