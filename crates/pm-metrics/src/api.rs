//! API endpoints and the raw Prometheus exposition handler.

use std::sync::Arc;

use pm_api::{Api, Endpoint, Parameter, Response, DYNAMIC};
use pm_config::{ExpertiseLevel, EXPERTISE_LEVEL_DEVELOPER, EXPERTISE_LEVEL_EXPERT, EXPERTISE_LEVEL_USER};

use crate::registry::Registry;

/// Registers `/metrics`, `metrics/list` and `metrics/values`, matching Go.
pub fn register_api(
    api: &Arc<Api>,
    registry: Arc<Registry>,
    on_scrape: Arc<dyn Fn() + Send + Sync>,
) -> pm_api::Result<()> {
    let raw_registry = Arc::clone(&registry);
    api.register_handler(
        "/metrics",
        Arc::new(move |request| {
            on_scrape();
            let permission = request
                .auth
                .as_ref()
                .map_or(pm_api::PERMIT_ANYONE, |token| token.read);
            let expertise_level = expertise_from_query(request.query_param("level"));
            let mut body = String::new();
            raw_registry.write_prometheus(&mut body, permission, expertise_level);
            Response::body(
                200,
                body.into_bytes(),
                "text/plain; version=0.0.4; charset=utf-8",
            )
        }),
    )?;

    let list_registry = Arc::clone(&registry);
    api.register_endpoint(
        Endpoint::new(
            "metrics/list",
            "Export Registered Metrics",
            "List all registered metrics with their metadata.",
        )
        .with_read(DYNAMIC)
        .with_struct(move |request| {
            let permission = request
                .auth
                .as_ref()
                .map_or(pm_api::PERMIT_ANYONE, |token| token.read);
            Ok(list_registry.export_metrics(permission))
        }),
    )?;

    let values_registry = Arc::clone(&registry);
    api.register_endpoint(
        Endpoint::new(
            "metrics/values",
            "Export Metric Values",
            "List all exportable metric values.",
        )
        .with_read(DYNAMIC)
        .with_parameter(Parameter {
            method: "GET".to_string(),
            field: "internal-only".to_string(),
            value: String::new(),
            description: "Specify to only return metrics with an alternative internal ID."
                .to_string(),
        })
        .with_struct(move |request| {
            let permission = request
                .auth
                .as_ref()
                .map_or(pm_api::PERMIT_ANYONE, |token| token.read);
            let internal_only = request.query.contains_key("internal-only");
            Ok(values_registry.export_values(permission, internal_only))
        }),
    )?;

    Ok(())
}

/// Resolves the expertise level from a `level` query value, like Go.
pub(crate) fn expertise_from_query(value: Option<&str>) -> ExpertiseLevel {
    match value {
        Some("user") => EXPERTISE_LEVEL_USER,
        Some("expert") => EXPERTISE_LEVEL_EXPERT,
        _ => EXPERTISE_LEVEL_DEVELOPER,
    }
}
