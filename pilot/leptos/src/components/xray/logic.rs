//! Pure logic for the xray component.
//!
//! DOM-free and Leptos-free on purpose: the visual layer owns all signals and
//! `view!` macros, while these helpers are plain functions that can be unit
//! tested without a browser or a WASM runtime.

use crate::model::{t, Lang};

/// Label for the connect/disconnect toggle button.
///
/// While connected the button offers to disconnect; otherwise it offers to
/// connect. The text is translated into the requested UI language.
pub fn button_label(connected: bool, lang: Lang) -> String {
    let key = if connected { "disconnect" } else { "connect" };
    t(key, lang).to_string()
}

/// Human-readable status of the Xray connection, translated into `lang`.
pub fn status_label(connected: bool, lang: Lang) -> String {
    let key = if connected { "connected" } else { "disconnected" };
    t(key, lang).to_string()
}

/// CSS color used to render the connection status.
///
/// Green when connected, red otherwise. Returned as a `&'static str` so it can
/// be dropped straight into a Leptos `style:color` binding.
pub fn status_color(connected: bool) -> &'static str {
    if connected {
        "seagreen"
    } else {
        "crimson"
    }
}

/// Normalize a pasted subscription URL for storage / comparison.
///
/// Trims surrounding whitespace, removes trailing `/` characters and returns
/// an empty string when the result is empty (e.g. `""`, `"   "`, `"/"`).
pub fn normalize_subscription_url(raw: &str) -> String {
    let normalized = raw.trim().trim_end_matches('/');
    if normalized.is_empty() {
        String::new()
    } else {
        normalized.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn button_label_reflects_connection_state_in_english() {
        assert_eq!(button_label(false, Lang::En), "Connect");
        assert_eq!(button_label(true, Lang::En), "Disconnect");
    }

    #[test]
    fn button_label_is_translated_to_russian() {
        assert_eq!(button_label(false, Lang::Ru), "Подключиться");
        assert_eq!(button_label(true, Lang::Ru), "Отключиться");
    }

    #[test]
    fn status_label_reflects_connection_state_in_english() {
        assert_eq!(status_label(false, Lang::En), "Disconnected");
        assert_eq!(status_label(true, Lang::En), "Connected");
    }

    #[test]
    fn status_label_is_translated_to_russian() {
        assert_eq!(status_label(false, Lang::Ru), "Отключено");
        assert_eq!(status_label(true, Lang::Ru), "Подключено");
    }

    #[test]
    fn status_color_encodes_connection_state() {
        assert_eq!(status_color(true), "seagreen");
        assert_eq!(status_color(false), "crimson");
    }

    #[test]
    fn normalize_subscription_url_trims_whitespace() {
        assert_eq!(
            normalize_subscription_url("  https://example.com/sub  "),
            "https://example.com/sub"
        );
        assert_eq!(
            normalize_subscription_url("\thttps://example.com/sub\n"),
            "https://example.com/sub"
        );
    }

    #[test]
    fn normalize_subscription_url_strips_trailing_slashes() {
        assert_eq!(
            normalize_subscription_url("https://example.com/sub/"),
            "https://example.com/sub"
        );
        assert_eq!(
            normalize_subscription_url("https://example.com/sub///"),
            "https://example.com/sub"
        );
    }

    #[test]
    fn normalize_subscription_url_rejects_empty_input() {
        assert_eq!(normalize_subscription_url(""), "");
        assert_eq!(normalize_subscription_url("   "), "");
        assert_eq!(normalize_subscription_url("/"), "");
        assert_eq!(normalize_subscription_url(" /// "), "");
    }

    #[test]
    fn normalize_subscription_url_keeps_internal_slashes() {
        assert_eq!(
            normalize_subscription_url("https://example.com/a/b"),
            "https://example.com/a/b"
        );
    }
}
