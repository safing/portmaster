//! The connection manager (Go `netquery/manager.go`): converts connections
//! from the firewall feed to rows, saves them and pushes runtime updates.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use pm_network::{format_ip, Connection, ConnectionType, SharedConnection};
use pm_record::{Key, Meta, Record};
use pm_runtime::{PushFunc, Registry, SimpleValueGetter};
use serde_json::Value;
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;

use crate::{Conn, Database};

/// Runtime prefix the UI subscribes to (`netquery/data/<id>`).
pub const PUSH_PREFIX: &str = "netquery/data/";

/// Why a connection could not be converted.
#[derive(Debug, thiserror::Error)]
pub enum ConvertError {
    /// Go dereferences the process unconditionally; the firewall always sets it.
    #[error("connection has no process")]
    NoProcess,
    #[error("failed to marshal extra data: {0}")]
    ExtraData(String),
}

/// Go `makeNqIDFromParts`.
pub fn make_nq_id(process_key: &str, conn_id: &str) -> String {
    format!("{process_key}-{conn_id}")
}

fn to_value<T: serde::Serialize>(value: &T) -> Result<Value, ConvertError> {
    serde_json::to_value(value).map_err(|e| ConvertError::ExtraData(e.to_string()))
}

/// Go `convertConnection`. The caller holds the connection lock.
///
/// Times are UTC. `extra_data` is built like Go's `map[string]interface{}`:
/// keys sorted, HTML characters escaped.
pub fn convert_connection(conn: &Connection) -> Result<Conn, ConvertError> {
    let process = conn.process.as_ref().ok_or(ConvertError::NoProcess)?;
    let ctx = &conn.process_context;

    let mut c = Conn {
        id: make_nq_id(process.key(), &conn.id),
        external: conn.external,
        ip_version: conn.ip_version,
        ip_protocol: conn.ip_protocol,
        local_ip: format_ip(conn.local_ip),
        local_port: conn.local_port,
        active_verdict: conn.verdict as i8,
        started: conn.started,
        tunneled: conn.tunneled,
        encrypted: conn.encrypted,
        internal: conn.internal,
        direction: if conn.inbound { "inbound" } else { "outbound" }.to_string(),
        conn_type: match conn.conn_type {
            ConnectionType::Dns => "dns",
            ConnectionType::Ip => "ip",
            ConnectionType::Undefined => "",
        }
        .to_string(),
        profile_id: format!("{}/{}", ctx.source, ctx.profile),
        path: ctx.binary_path.clone(),
        profile_revision: i64::try_from(conn.profile_revision_counter).unwrap_or(i64::MAX),
        profile_name: ctx.profile_name.clone(),
        // Go takes the established flag as is, so `allowed` is never NULL here.
        allowed: Some(conn.connection_established),
        ..Conn::default()
    };
    if conn.ended > 0 {
        c.ended = Some(conn.ended);
        c.active = false;
    } else {
        c.active = true;
    }

    let mut extra: BTreeMap<&str, Value> = BTreeMap::new();
    extra.insert("pid", Value::from(ctx.pid));
    extra.insert("processCreatedAt", Value::from(ctx.created_at));
    // The SPN tunnel context does not exist in this port: no `tunnel`, no exit node.
    if let Some(split) = &conn.split_tun_context {
        extra.insert("split_tun", to_value(split)?);
    }
    if let Some(dns) = &conn.dns_context {
        extra.insert("dns", to_value(dns)?);
    }

    if let Some(entity) = &conn.entity {
        // Go writes nil slices as null; empty and nil are the same here except
        // for the lists after a match, which are non-nil.
        let list = |items: &[String], keep_empty: bool| {
            if items.is_empty() && !keep_empty {
                Value::Null
            } else {
                Value::from(items.to_vec())
            }
        };
        extra.insert("cname", list(&entity.cname, false));
        extra.insert(
            "blockedByLists",
            list(&entity.blocked_by_lists, entity.lists_matched),
        );
        extra.insert(
            "blockedEntities",
            list(&entity.blocked_entities, entity.lists_matched),
        );
        let mut reason = to_value(&conn.reason)?;
        // Only the free-form context: the struct fields keep Go's order.
        if let Some(context) = reason.get_mut("Context") {
            sort_keys(context);
        }
        extra.insert("reason", reason);

        c.remote_ip = format_ip(entity.ip);
        c.remote_port = entity.port;
        c.domain = entity.domain.clone();
        c.country = entity.country.clone();
        c.asn = entity.asn;
        c.as_owner = entity.as_org.clone();
        c.scope = entity.ip_scope as i8;
        if let Some(coords) = &entity.coordinates {
            c.latitude = coords.latitude;
            c.longitude = coords.longitude;
        }
    }

    c.extra_data =
        pm_dsd::gojson::to_vec(&extra).map_err(|e| ConvertError::ExtraData(e.to_string()))?;
    Ok(c)
}

/// Sorts object keys recursively: Go marshals maps with sorted keys.
fn sort_keys(value: &mut Value) {
    match value {
        Value::Object(map) => {
            let mut entries: Vec<(String, Value)> = std::mem::take(map).into_iter().collect();
            entries.sort_by(|a, b| a.0.cmp(&b.0));
            for (key, mut v) in entries {
                sort_keys(&mut v);
                map.insert(key, v);
            }
        }
        Value::Array(items) => items.iter_mut().for_each(sort_keys),
        _ => {}
    }
}

/// Persists feed connections and pushes updates to the runtime database.
pub struct Manager {
    store: Arc<Database>,
    push: PushFunc,
    registry: Registry,
    push_prefix: String,
}

impl Manager {
    /// Registers `push_prefix` (normally [`PUSH_PREFIX`]) on `registry`.
    /// Reads there return nothing, like Go.
    pub fn new(
        store: Arc<Database>,
        push_prefix: &str,
        registry: &Registry,
    ) -> pm_runtime::Result<Manager> {
        let provider = Arc::new(SimpleValueGetter::new(|_key| Ok(Vec::new())));
        let push = registry.register(push_prefix, provider)?;
        Ok(Manager {
            store,
            push,
            registry: registry.clone(),
            push_prefix: push_prefix.to_string(),
        })
    }

    /// Handles one update like one iteration of Go `HandleFeed`: skips
    /// incomplete connections, saves the row (history only if the connection
    /// has it enabled) and pushes the update. Errors are logged.
    pub fn handle(&self, conn: &SharedConnection) {
        // Hold the connection lock only for the conversion.
        let (model, history, meta) = {
            let conn = lock(conn);
            if !conn.data_complete {
                return;
            }
            match convert_connection(&conn) {
                Ok(model) => (model, conn.history_enabled, conn.meta),
                Err(error) => {
                    tracing::error!(%error, id = %conn.id, "netquery: failed to convert connection");
                    return;
                }
            }
        };
        if let Err(error) = self.store.save(&model, history) {
            tracing::error!(%error, id = %model.id, "netquery: failed to save connection");
            return;
        }
        if let Err(error) = self.push_conn_update(meta, &model) {
            tracing::error!(%error, id = %model.id, "netquery: failed to push connection update");
        }
    }

    fn push_conn_update(&self, meta: Meta, model: &Conn) -> Result<(), pm_record::Error> {
        let key = Key::new(&self.registry.database_name(), &format!("{}{}", self.push_prefix, model.id));
        let record = Record::from_typed(key, meta, model)?;
        (self.push)(&[record]);
        Ok(())
    }

    /// Reads updates until `cancel` fires or the feed is closed (Go
    /// `HandleFeed`). Updates are handled one after another; the SQLite calls
    /// run on the blocking pool.
    pub async fn handle_feed(
        self: &Arc<Self>,
        mut feed: mpsc::Receiver<SharedConnection>,
        cancel: CancellationToken,
    ) {
        loop {
            tokio::select! {
                () = cancel.cancelled() => return,
                next = feed.recv() => match next {
                    Some(conn) => {
                        let manager = Arc::clone(self);
                        if let Err(error) =
                            tokio::task::spawn_blocking(move || manager.handle(&conn)).await
                        {
                            tracing::error!(%error, "netquery: connection handler panicked");
                        }
                    }
                    None => return,
                },
            }
        }
    }
}

fn lock(conn: &Mutex<Connection>) -> std::sync::MutexGuard<'_, Connection> {
    conn.lock().unwrap_or_else(std::sync::PoisonError::into_inner)
}
