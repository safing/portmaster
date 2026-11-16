//! Update channels and default index URLs, port of Go
//! `service/configure/updates.go`.

/// Name of the binary update index.
pub const DEFAULT_BINARY_INDEX_NAME: &str = "Portmaster Binaries";
/// Name of the intel update index.
pub const DEFAULT_INTEL_INDEX_NAME: &str = "Portmaster Intel";

/// Stable release channel index URLs.
pub const DEFAULT_STABLE_BINARY_INDEX_URLS: &[&str] = &["https://updates.safing.io/stable.v3.json"];
/// Beta release channel index URLs.
pub const DEFAULT_BETA_BINARY_INDEX_URLS: &[&str] = &["https://updates.safing.io/beta.v3.json"];
/// Staging release channel index URLs.
pub const DEFAULT_STAGING_BINARY_INDEX_URLS: &[&str] =
    &["https://updates.safing.io/staging.v3.json"];
/// Support release channel index URLs.
pub const DEFAULT_SUPPORT_BINARY_INDEX_URLS: &[&str] =
    &["https://updates.safing.io/support.v3.json"];

/// Default intel index URLs.
pub const DEFAULT_INTEL_INDEX_URLS: &[&str] = &["https://updates.safing.io/intel.v3.json"];

/// Returns the binary update URLs for the release channel.
///
/// An unknown channel silently falls back to stable, like Go
/// `GetBinaryUpdateURLs`.
pub fn binary_update_urls(release_channel: &str) -> &'static [&'static str] {
    match release_channel {
        "stable" => DEFAULT_STABLE_BINARY_INDEX_URLS,
        "beta" => DEFAULT_BETA_BINARY_INDEX_URLS,
        "staging" => DEFAULT_STAGING_BINARY_INDEX_URLS,
        "support" => DEFAULT_SUPPORT_BINARY_INDEX_URLS,
        _ => DEFAULT_STABLE_BINARY_INDEX_URLS,
    }
}

/// Returns the intel update URLs as owned strings.
pub fn intel_update_urls() -> Vec<String> {
    DEFAULT_INTEL_INDEX_URLS
        .iter()
        .map(|url| (*url).to_string())
        .collect()
}

/// Returns the binary update URLs as owned strings for a channel.
pub fn binary_update_url_list(release_channel: &str) -> Vec<String> {
    binary_update_urls(release_channel)
        .iter()
        .map(|url| (*url).to_string())
        .collect()
}
