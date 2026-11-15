//! Tests against real MaxMind databases, if they are provided.
//!
//! The databases are not part of the repository (MaxMind license), so the test
//! is skipped unless `PM_GEOIP_V4` (and optionally `PM_GEOIP_V6`) point to a
//! database file.

use pm_geoip::GeoIp;

#[test]
fn reads_real_databases_when_available() {
    let Ok(v4) = std::env::var("PM_GEOIP_V4") else {
        eprintln!("skipping: PM_GEOIP_V4 is not set");
        return;
    };

    let geoip = GeoIp::new();
    assert!(!geoip.is_initialized(false));

    geoip.replace_database(&v4, false).unwrap();
    assert!(geoip.is_initialized(false));
    assert!(
        !geoip.is_initialized(true),
        "IPv6 database is still missing"
    );

    // 1.1.1.1 is anycast: the location is remapped to the anycast pseudo
    // country, exactly like Go's AddCountryInfo.
    let anycast = geoip.get_location("1.1.1.1".parse().unwrap()).unwrap();
    assert!(anycast.is_anycast, "{anycast:?}");
    assert_eq!(anycast.country.code, "__");
    assert_eq!(anycast.country.name, "Anycast");
    assert_eq!(anycast.coordinates, anycast.country.center);

    // A regular address gets country information and coordinates.
    let location = geoip
        .get_location("93.184.216.34".parse().unwrap())
        .unwrap();
    assert!(!location.country.code.is_empty(), "{location:?}");
    assert!(!location.country.name.is_empty(), "country info was added");
    assert_eq!(location.country.code.len(), 2, "{location:?}");
    assert_eq!(location.country.continent.code.len(), 2, "{location:?}");
    assert!(
        location.coordinates.latitude != 0.0 || location.coordinates.longitude != 0.0,
        "{location:?}"
    );

    // Addresses without a record return an empty location, not an error.
    let unknown = geoip.get_location("192.0.2.1".parse().unwrap()).unwrap();
    assert!(unknown.country.code.is_empty(), "{unknown:?}");

    if let Ok(v6) = std::env::var("PM_GEOIP_V6") {
        geoip.replace_database(&v6, true).unwrap();
        assert!(geoip.is_initialized(true));
        let location = geoip
            .get_location("2001:4860:4860::8888".parse().unwrap())
            .unwrap();
        assert!(!location.country.code.is_empty(), "{location:?}");
        assert!(!location.country.name.is_empty(), "{location:?}");
    }
}

#[test]
fn resources_are_selected_by_name() {
    let geoip = GeoIp::new();
    match geoip.replace_resource("/nonexistent/geoip.mmdb", "other.mmdb") {
        Err(pm_geoip::Error::UnknownResource(name)) => assert_eq!(name, "other.mmdb"),
        other => panic!("unexpected result: {other:?}"),
    }

    // A known resource is opened (and fails because the file is missing).
    assert!(geoip
        .replace_resource("/nonexistent/geoipv4.mmdb", pm_geoip::V4_RESOURCE)
        .is_err());
    assert!(!geoip.is_initialized(false));
}
