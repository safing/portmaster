//! API endpoints, ported from Go `service/broadcasts/api.go`.

use std::sync::Arc;

use pm_api::{Api, ApiError, Endpoint, Parameter, PERMIT_ADMIN};

use crate::{Broadcasts, Error, Result};

/// Registers `broadcasts/matching-data`, `broadcasts/reset-state` and
/// `broadcasts/simulate`.
pub(crate) fn register_api(module: &Arc<Broadcasts>, api: &Api) -> Result<()> {
    let matching_module = Arc::clone(module);
    api.register_endpoint(
        Endpoint::new(
            "broadcasts/matching-data",
            "Get Broadcast Notifications Matching Data",
            "Returns the data used by the broadcast notifications to match the instance.",
        )
        .with_read(PERMIT_ADMIN)
        .with_struct(move |_| Ok(matching_module.collect_data())),
    )
    .map_err(map_api_error)?;

    let reset_module = Arc::clone(module);
    api.register_endpoint(
        Endpoint::new(
            "broadcasts/reset-state",
            "Reset Notification States",
            "Deletes the cached state of broadcast and other notifications, causing them to appear again.",
        )
        .with_write(PERMIT_ADMIN)
        .with_write_method("POST")
        .with_action(move |_| {
            reset_module
                .reset_state()
                .map_err(|error| ApiError::Message(error.to_string()))
        }),
    )
    .map_err(map_api_error)?;

    let simulate_module = Arc::clone(module);
    api.register_endpoint(
        Endpoint::new(
            "broadcasts/simulate",
            "Simulate Broadcast Notifications",
            "Test broadcast notifications by sending a valid source file in the body.",
        )
        .with_write(PERMIT_ADMIN)
        .with_write_method("POST")
        .with_parameter(Parameter {
            method: "POST".to_string(),
            field: "state".to_string(),
            value: "true".to_string(),
            description: "Check against state when deciding to display a broadcast notification. Acknowledgements are always saved.".to_string(),
        })
        .with_action(move |request| {
            let use_state = request.query_param("state") == Some("true");
            simulate_module
                .simulate(&request.body, use_state)
                .map_err(|error| ApiError::Message(error.to_string()))
        }),
    )
    .map_err(map_api_error)?;

    Ok(())
}

fn map_api_error(error: ApiError) -> Error {
    Error::Message(error.to_string())
}
