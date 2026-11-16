//! The interoperability ping API, ported from `service/interop/api.go`.

use pm_api::{Api, Endpoint, PERMIT_ANYONE};

/// Path of the ping endpoint.
pub const API_ENDPOINT_PING: &str = "interop/ping";
/// Human readable name, kept identical to the Go endpoint.
pub const API_NAME_PING: &str = "Interoperability: Ping Portmaster";
/// Description, kept identical to the Go endpoint.
pub const API_DESCRIPTION_PING: &str =
    "Ping the Portmaster Core Service for interoperability checks.";

/// Registers the ping endpoint. Received pings may come from the IVPN client;
/// the callback triggers every interoperability module's ping handler, like Go
/// `handlePing` iterating over `interopModules`.
pub fn register_ping_api<F>(api: &Api, on_ping: F) -> pm_api::Result<()>
where
    F: Fn() + Send + Sync + 'static,
{
    api.register_endpoint(
        Endpoint::new(API_ENDPOINT_PING, API_NAME_PING, API_DESCRIPTION_PING)
            .with_read(PERMIT_ANYONE)
            .with_write(PERMIT_ANYONE)
            .with_action(move |_| {
                on_ping();
                // Go `handlePing` returns an empty message on success.
                Ok(String::new())
            }),
    )
}
