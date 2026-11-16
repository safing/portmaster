//! Network tag handler.
//!
//! Port of `service/process/tags/net.go`. The Go handler's `AddTags` is a
//! no-op: the `ip` tag is attached directly to virtual network-host processes
//! (see `pm_network::SystemProcessResolver::network_host`). The handler is
//! ported so that the tag description is exposed through the registry.

use crate::{Process, TagDescription, TagHandler};

/// Exposes the `ip` tag used by virtual network processes.
pub struct NetworkHandler;

impl TagHandler for NetworkHandler {
    fn name(&self) -> &str {
        "Network"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![TagDescription {
            id: "ip".to_string(),
            name: "IP Address".to_string(),
            description: "The remote IP address of external requests to Portmaster, if enabled."
                .to_string(),
        }]
    }

    fn add_tags(&self, _process: &mut Process) {
        // The "ip" tag is added directly when creating the virtual process.
    }
}
