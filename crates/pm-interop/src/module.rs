//! Top-level interoperability module, port of `service/interop/module.go` and
//! `verdict_handler.go`.

use std::sync::Arc;

use pm_api::Api;
use pm_network::Connection;

use crate::client::IvpnClientFactory;
use crate::engine::InteropIvpn;
use crate::env::InteropEnv;
use crate::ping::register_ping_api;
use crate::status::VerdictDecision;

/// Aggregates all interoperability modules, currently only IVPN.
pub struct Interop {
    ivpn: Arc<InteropIvpn>,
}

impl Interop {
    /// Builds the module with injectable environment and client factory.
    pub fn new(env: Arc<dyn InteropEnv>, factory: Arc<dyn IvpnClientFactory>) -> Interop {
        Interop {
            ivpn: Arc::new(InteropIvpn::new(env, factory)),
        }
    }

    /// Starts all interoperability modules and waits for their first attempt.
    pub async fn start(self: &Arc<Self>) {
        self.ivpn.start().await;
    }

    /// Handles a ping by invoking every module's ping handler.
    pub fn ping_handler(self: &Arc<Self>) {
        self.ivpn.ping_handler();
    }

    /// Returns the first non-undecided verdict from all modules, like Go
    /// `verdict_handler`.
    pub fn verdict_handler(&self, conn: &Connection) -> VerdictDecision {
        let decision = self.ivpn.verdict_handler(conn);
        if decision.is_undecided() {
            VerdictDecision::undecided()
        } else {
            decision
        }
    }

    /// Registers the interoperability API endpoints.
    pub fn register_api(self: &Arc<Self>, api: &Api) -> pm_api::Result<()> {
        let this = Arc::clone(self);
        register_ping_api(api, move || this.ping_handler())
    }

    /// Returns the IVPN engine, mainly for tests and diagnostics.
    pub fn ivpn(&self) -> &Arc<InteropIvpn> {
        &self.ivpn
    }
}
