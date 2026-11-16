//! API endpoint of the custom filter list, port of Go `customlists/module.go`.

use std::sync::Arc;

use pm_api::{Api, ApiError, Endpoint, Result, PERMIT_USER};

use crate::customlists::{CustomListService, UpdateOutcome, ERR_NOT_CONFIGURED};

/// Path of the endpoint that reloads the list.
pub const UPDATE_ENDPOINT_PATH: &str = "customlists/update";

/// Registers the `customlists/update` endpoint.
///
/// The action returns Go's "Custom filter list loaded successfully." message,
/// but not for an unconfigured list: Go returns the `ErrNotConfigured` error
/// text to the UI, so the Rust version reports it as an error too.
pub fn register_api(api: &Api, service: Arc<CustomListService>) -> Result<()> {
    api.register_endpoint(
        Endpoint::new(
            UPDATE_ENDPOINT_PATH,
            "Update custom filter list",
            "Reload the filter list from the configured file.",
        )
        .with_write(PERMIT_USER)
        .with_action(move |_| {
            match service
                .check_and_update_configured()
                .map_err(|error| ApiError::Message(error.to_string()))?
            {
                UpdateOutcome::NotConfigured => {
                    return Err(ApiError::Message(ERR_NOT_CONFIGURED.to_string()))
                }
                UpdateOutcome::Unchanged | UpdateOutcome::Loaded(_) => {}
            }
            Ok("Custom filter list loaded successfully.".to_string())
        }),
    )
}
