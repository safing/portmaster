//! Windows `svchost.exe` tag handler.
//!
//! Port of `service/process/tags/svchost_windows.go`. Go's `CreateProfile`
//! callback has no counterpart in the Rust [`TagHandler`] trait, so only tag
//! creation and the process-name suffix are ported here.

use pm_winplatform::osdetail::{self, OsError};

use crate::{Process, Tag, TagDescription, TagHandler};

const SVCHOST_NAME: &str = "Service Host";
const SVCHOST_TAG_KEY: &str = "svchost";

/// Tags the services hosted by a `svchost.exe` process.
pub struct SvchostHandler;

impl TagHandler for SvchostHandler {
    fn name(&self) -> &str {
        SVCHOST_NAME
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![TagDescription {
            id: SVCHOST_TAG_KEY.to_string(),
            name: "SvcHost Service Name".to_string(),
            description: "Name of a service running in svchost.exe as reported by Windows."
                .to_string(),
        }]
    }

    fn add_tags(&self, process: &mut Process) {
        if process.exec_name != "svchost.exe" {
            return;
        }

        match osdetail::get_service_names(process.pid) {
            Ok(service_names) => {
                if service_names.is_empty() {
                    return;
                }
                process
                    .name
                    .push_str(&format!(" ({})", service_names.join(", ")));
                for service_name in service_names {
                    // Drop suffixes like the "_1bf5729" in "CDPUserSvc_1bf5729".
                    let short_name = service_name
                        .split('_')
                        .next()
                        .unwrap_or(&service_name)
                        .to_string();
                    process.tags.push(Tag {
                        key: SVCHOST_TAG_KEY.to_string(),
                        value: short_name,
                    });
                }
            }
            Err(OsError::ServiceNotFound) => {
                tracing::trace!(
                    pid = process.pid,
                    "process/tags: failed to get service name for svchost.exe"
                );
            }
            Err(error) => {
                tracing::warn!(
                    pid = process.pid,
                    %error,
                    "process/tags: failed to get service name for svchost.exe"
                );
            }
        }
    }
}
