//! Pure logic for the theme component.
//!
//! Deliberately DOM-free and macro-free: just small helpers for flipping the
//! dark-mode flag and deriving the strings/classes the visual layer needs.
//! Keeping it pure means it can be unit-tested on a native target without
//! pulling in Leptos or a browser.

use crate::model::{t, Lang};

/// Flip a dark-mode flag.
///
/// ```ignore
/// assert!(!toggle(true));
/// assert!(toggle(false));
/// ```
pub fn toggle(dark: bool) -> bool {
    !dark
}

/// Human-readable label describing the **current** theme state: `true`
/// yields "Dark", `false` yields "Light".
///
/// Callers that want an *action* label (e.g. "switch to Light") can simply
/// pass the inverted flag.
///
/// # Translation & fallback
///
/// The preferred path is the shared dictionary via the `dark`/`light` keys
/// (see [`t`]). Those keys are **not** present in `crate::model` right now,
/// and this module intentionally does not own that file, so we fall back to
/// hardcoded "Dark"/"Light" (documented behavior). Because [`t`] returns
/// unknown keys verbatim, a missing translation is detected by the result
/// equalling the key itself. If the keys are added to the dictionary later,
/// this function picks up the translated strings automatically with no
/// change here.
pub fn label(dark: bool, lang: Lang) -> String {
    let key = if dark { "dark" } else { "light" };
    let fallback = if dark { "Dark" } else { "Light" };
    let translated = t(key, lang);

    if translated == key {
        fallback.to_string()
    } else {
        translated.to_string()
    }
}

/// Color for the swatch shown in the theme toggle UI.
///
/// - dark  -> `#131a22`
/// - light -> `#e6edf3`
pub fn swatch_color(dark: bool) -> &'static str {
    if dark {
        "#131a22"
    } else {
        "#e6edf3"
    }
}

/// CSS class the visual layer applies to the body to select the theme.
///
/// - dark  -> `theme-dark`
/// - light -> `theme-light`
pub fn body_class(dark: bool) -> &'static str {
    if dark {
        "theme-dark"
    } else {
        "theme-light"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn toggle_flips_both_directions() {
        assert!(!toggle(true));
        assert!(toggle(false));
    }

    #[test]
    fn toggle_is_an_involution() {
        assert!(toggle(toggle(true)));
        assert!(!toggle(toggle(false)));
    }

    #[test]
    fn swatch_colors_are_stable() {
        assert_eq!(swatch_color(true), "#131a22");
        assert_eq!(swatch_color(false), "#e6edf3");
    }

    #[test]
    fn body_classes_are_stable() {
        assert_eq!(body_class(true), "theme-dark");
        assert_eq!(body_class(false), "theme-light");
    }

    #[test]
    fn label_uses_dictionary_translations() {
        assert_eq!(label(true, Lang::En), "Dark");
        assert_eq!(label(true, Lang::Ru), "Тёмная");
        assert_eq!(label(false, Lang::En), "Light");
        assert_eq!(label(false, Lang::Ru), "Светлая");
    }

    #[test]
    fn label_distinguishes_dark_from_light() {
        for lang in [Lang::En, Lang::Ru] {
            assert_ne!(label(true, lang), label(false, lang));
            assert!(!label(true, lang).is_empty());
            assert!(!label(false, lang).is_empty());
        }
    }
}
