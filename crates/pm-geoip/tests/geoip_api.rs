//! Tests for the `intel/geoip/countries` endpoint (Go
//! `service/intel/geoip/module.go`).

use pm_api::{Api, Request};
use pm_geoip::{register_api, COUNTRIES_ENDPOINT_PATH};
use serde_json::{json, Value};

fn dev_api() -> std::sync::Arc<Api> {
    let api = Api::new("127.0.0.1:817");
    api.set_dev_mode(true);
    register_api(&api).expect("register endpoint");
    api
}

#[test]
fn endpoint_metadata_matches_go() {
    let api = dev_api();
    let endpoint = api.endpoint(COUNTRIES_ENDPOINT_PATH).expect("endpoint");
    assert_eq!(endpoint.name, "Get Country Information");
    assert_eq!(
        endpoint.description,
        "Returns a map of country information centers indexed by ISO-A2 country code"
    );
    assert_eq!(endpoint.read, pm_api::PERMIT_USER);
}

#[test]
fn returns_the_go_country_data() {
    let api = dev_api();
    let response = api.handle(Request::new(
        "GET",
        format!("/api/v1/{COUNTRIES_ENDPOINT_PATH}"),
    ));
    assert_eq!(response.status, 200);

    let body: Value = serde_json::from_slice(&response.body).expect("JSON body");
    let countries = body.as_object().expect("object");

    // The Go table has 248 entries (247 countries plus anycast).
    assert!(countries.len() >= 247, "only {} countries", countries.len());

    // Reference value from Go `GetCountryInfo("US")`.
    assert_eq!(
        countries["US"],
        json!({
            "Code": "US",
            "Name": "United States of America",
            "Center": {"AccuracyRadius": 200, "Latitude": 37, "Longitude": -95},
            "Continent": {"Code": "NA", "Region": "NA-N", "Name": "North America"},
        })
    );

    // Go's `init` applies the default accuracy to the anycast entry too.
    assert_eq!(
        countries["__"],
        json!({
            "Code": "__",
            "Name": "Anycast",
            "Center": {"AccuracyRadius": 200, "Latitude": 0, "Longitude": 0},
            "Continent": {"Code": "", "Region": "", "Name": ""},
        })
    );

    // Go serializes a map with sorted keys; check that the order matches.
    let keys: Vec<&String> = countries.keys().collect();
    let mut sorted = keys.clone();
    sorted.sort();
    assert_eq!(keys, sorted, "country codes are sorted");
}
