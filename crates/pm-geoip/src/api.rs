//! Port of the GeoIP API endpoint from Go `service/intel/geoip/module.go`.

use std::collections::BTreeMap;

use pm_api::{Api, ApiError, Endpoint, Result, PERMIT_USER};

use crate::{countries, CountryInfo};

/// Path of the country information endpoint.
pub const COUNTRIES_ENDPOINT_PATH: &str = "intel/geoip/countries";

/// Registers the `intel/geoip/countries` endpoint.
///
/// Go attaches the endpoint to the geoip module; the country table is static,
/// so it is always available here too.
pub fn register_api(api: &Api) -> Result<()> {
    api.register_endpoint(
        Endpoint::new(
            COUNTRIES_ENDPOINT_PATH,
            "Get Country Information",
            "Returns a map of country information centers indexed by ISO-A2 country code",
        )
        .with_read(PERMIT_USER)
        .with_struct(|_| {
            // Go serializes a map, which sorts the keys; keep that order.
            let sorted: BTreeMap<&str, &CountryInfo> = countries()
                .iter()
                .map(|(code, info)| (code.as_str(), info))
                .collect();
            serde_json::to_value(sorted).map_err(ApiError::from)
        }),
    )
}
