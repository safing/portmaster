//! Tests for update channels (Go `service/configure/updates.go`).

use pm_updates::{
    binary_update_urls, intel_update_urls, DEFAULT_BETA_BINARY_INDEX_URLS,
    DEFAULT_STABLE_BINARY_INDEX_URLS, DEFAULT_STAGING_BINARY_INDEX_URLS,
    DEFAULT_SUPPORT_BINARY_INDEX_URLS,
};

#[test]
fn channels_match_go() {
    assert_eq!(
        binary_update_urls("stable"),
        DEFAULT_STABLE_BINARY_INDEX_URLS
    );
    assert_eq!(binary_update_urls("beta"), DEFAULT_BETA_BINARY_INDEX_URLS);
    assert_eq!(
        binary_update_urls("staging"),
        DEFAULT_STAGING_BINARY_INDEX_URLS
    );
    assert_eq!(
        binary_update_urls("support"),
        DEFAULT_SUPPORT_BINARY_INDEX_URLS
    );
    // Unknown channels fall back to stable.
    assert_eq!(
        binary_update_urls("unknown"),
        DEFAULT_STABLE_BINARY_INDEX_URLS
    );
    assert_eq!(binary_update_urls(""), DEFAULT_STABLE_BINARY_INDEX_URLS);
}

#[test]
fn intel_urls_match_go() {
    assert_eq!(
        intel_update_urls(),
        vec!["https://updates.safing.io/intel.v3.json".to_string()]
    );
}
