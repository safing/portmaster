//! Port of Go `api.go`: the `dns/*` API endpoints.

use pm_api::{Api, ApiError, Endpoint, Parameter, PERMIT_ANYONE, PERMIT_USER};
use serde_json::{json, Value};

use crate::service::ResolverService;

/// Returns whether the query of `dns/cache/{query}` matches the Go route
/// pattern `[a-z0-9\.-]{0,512}\.[A-Z]{1,32}`.
pub fn is_valid_cache_query(query: &str) -> bool {
    let Some((prefix, suffix)) = query.rsplit_once('.') else {
        return false;
    };
    (1..=32).contains(&suffix.len())
        && suffix.bytes().all(|b| b.is_ascii_uppercase())
        && prefix.len() <= 512
        && prefix
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'.' || b == b'-')
}

/// Exports the global resolvers with their status, like Go `exportDNSResolvers`.
pub fn export_dns_resolvers(service: &ResolverService) -> Result<Value, ApiError> {
    let mut export = Vec::new();
    for resolver in service.global_resolvers() {
        let mut value = serde_json::to_value(&*resolver)?;
        let failing = resolver.conn.as_ref().is_some_and(|c| c.is_failing());
        if let Value::Object(map) = &mut value {
            map.insert("Failing".to_string(), json!(failing));
        }
        export.push(value);
    }
    Ok(Value::Array(export))
}

/// Registers the resolver endpoints.
pub fn register_api(api: &Api, service: &ResolverService) -> Result<(), ApiError> {
    let clear_service = service.clone();
    api.register_endpoint(
        Endpoint::new(
            "dns/clear",
            "Clear cached DNS records",
            "Deletes all saved DNS records from the database.",
        )
        .with_write(PERMIT_USER)
        .with_action(move |_| {
            tracing::info!("resolver: user requested dns cache clearing via action");
            let count = clear_service
                .store()
                .clear()
                .map_err(|e| ApiError::Message(e.to_string()))?;
            tracing::debug!("resolver: cleared {count} entries from dns cache");
            Ok(format!("cleared {count} dns cache entries"))
        }),
    )?;

    let export_service = service.clone();
    api.register_endpoint(
        Endpoint::new(
            "dns/resolvers",
            "List DNS Resolvers",
            "List currently configured DNS resolvers and their status.",
        )
        .with_read(PERMIT_ANYONE)
        .with_struct(move |_| export_dns_resolvers(&export_service)),
    )?;

    let cache_service = service.clone();
    api.register_endpoint(
        Endpoint::new(
            "dns/cache/{query}",
            "Get DNS Record from Cache",
            "Returns cached dns records from the internal cache.",
        )
        .with_read(PERMIT_USER)
        .with_parameter(Parameter {
            method: "GET".to_string(),
            field: "query (in path)".to_string(),
            value: "fqdn and query type".to_string(),
            description: "Specify the query like this: `example.com.A`.".to_string(),
        })
        .with_record(move |request| {
            let query = request.param("query").unwrap_or_default().to_string();
            // The Go route only matches queries of this form.
            if !is_valid_cache_query(&query) {
                return Err(ApiError::Message("not found".to_string()).with_status(404));
            }
            cache_service
                .store()
                .get_raw(&query)
                .map_err(|e| ApiError::Message(e.to_string()))
        }),
    )?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cache_query_pattern() {
        for ok in [
            "example.com.A",
            "a-b.example.com.AAAA",
            ".A",
            "x.HTTPS",
            "1.0.0.127.in-addr.arpa.PTR",
        ] {
            assert!(is_valid_cache_query(ok), "{ok}");
        }
        for bad in [
            "",
            "example.com",
            "example.com.a",
            "Example.com.A",
            "example.com.A1",
            "exam_ple.com.A",
            "example.com.",
        ] {
            assert!(!is_valid_cache_query(bad), "{bad}");
        }
        assert!(!is_valid_cache_query(&format!("{}.A", "a".repeat(513))));
        assert!(is_valid_cache_query(&format!("{}.A", "a".repeat(512))));
        assert!(!is_valid_cache_query(&format!("a.{}", "A".repeat(33))));
    }
}
