//! Windows OS integration (ETW) glue for the core service.
//!
//! On Windows this loads `portmaster-core.dll` from the data directory and
//! resolves its ETW entry points. Go resolves the DLL through the binary
//! updater; here the updater is not yet wired to this module, so the DLL is
//! looked up next to the core data. On other platforms the returned
//! [`OsIntegration`] is a zero-sized no-op.

use std::path::Path;

pub use pm_winplatform::integration::{EtwFunctions, IntegrationError, OsIntegration};

/// Creates the OS integration and tries to load the ETW DLL on Windows.
pub fn initialize(data_dir: &Path) -> OsIntegration {
    #[cfg(windows)]
    let mut integration = OsIntegration::new();
    #[cfg(not(windows))]
    let integration = OsIntegration::new();

    #[cfg(windows)]
    {
        let dll_path = data_dir.join("portmaster-core.dll");
        if dll_path.is_file() {
            if let Err(error) = integration.initialize(&dll_path.to_string_lossy()) {
                tracing::warn!(%error, "integration: failed to load portmaster-core.dll");
            } else {
                tracing::info!("integration: initialize successful");
            }
        } else {
            tracing::debug!("integration: portmaster-core.dll not present in data directory");
        }
    }
    #[cfg(not(windows))]
    let _ = data_dir;

    integration
}
