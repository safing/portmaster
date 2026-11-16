//! Rust port of the Portmaster `base/api/client` and `base/api/testclient`.
//!
//! The client speaks the database WebSocket protocol of
//! [`pm_api::DatabaseApi`](../pm_api/struct.DatabaseApi.html) at
//! `/api/database/v1`: frames are `opID|type|key|data`, record values carry the
//! DSD JSON byte prefix. A single connection multiplexes operations by id;
//! subscriptions can be resuscitated after a reconnect and authenticate with
//! the same bearer tokens and `Portmaster-API-Token` session cookies as the
//! server.
//!
//! # Compatibility notes
//!
//! * Go callbacks (`func(*Message)`) are replaced by an async
//!   [`Operation::recv`] channel; `Online`/`Offline` channels are one
//!   [`Client::online`] watch channel.
//! * `sub`/`qsub` enable resuscitation automatically so subscriptions survive
//!   reconnects; Go left this to an explicit `EnableResuscitation` call.
//! * The Go `base/api/testclient` `/test/` static debug page is a server route,
//!   not client logic; the integration tests serve that role here.
//!
//! # Example
//!
//! ```no_run
//! use std::sync::Arc;
//!
//! use pm_api_client::{msg, Client};
//! use serde_json::json;
//!
//! # async fn demo() -> Result<(), Box<dyn std::error::Error>> {
//! let client = Arc::new(Client::new("127.0.0.1:817").with_token("admin-key"));
//! let connection = client.stay_connected();
//!
//! let mut created = client.create("test:item", json!({"Value": 1}));
//! let reply = created.recv().await.expect("reply");
//! assert_eq!(reply.msg_type, msg::SUCCESS);
//!
//! let mut sub = client.sub("query test:");
//! while let Some(message) = sub.recv().await {
//!     if message.msg_type == msg::NEW {
//!         println!("new record: {}", message.key);
//!     }
//! }
//!
//! client.shutdown();
//! connection.await;
//! # Ok(())
//! # }
//! ```

pub mod client;
pub mod error;
pub mod message;

pub use client::{Client, Operation, BACKOFF, DB_API_PATH};
pub use error::{Error, Result};
pub use message::{msg, Message, SEPARATOR};
