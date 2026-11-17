//! Pure logic for the language toggle component.
//!
//! No Leptos macros or DOM types live here — just small functions over
//! [`Lang`] so the behaviour can be unit-tested on a native target.

use crate::model::{t, Lang};

/// Return the other supported language.
///
/// This mirrors [`Lang::toggle`] but is exposed as a free function so the
/// view layer does not need to import the enum's methods directly.
pub fn toggle(current: Lang) -> Lang {
    current.toggle()
}

/// Visible caption for the language toggle button, e.g. `"Language (en)"`.
///
/// Combines the localized word for "language" with the short language code
/// so the user can see both the control's purpose and the active language.
pub fn button_label(lang: Lang) -> String {
    format!("{} ({})", t("language", lang), lang.as_str())
}

/// Accessible name for the language toggle button, e.g. `"Language: en"`.
///
/// Kept distinct from [`button_label`] so assistive technology gets a stable,
/// descriptive name that also reports the currently active language.
pub fn aria_label(lang: Lang) -> String {
    format!("{}: {}", t("language", lang), lang.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn toggle_is_symmetric() {
        assert_eq!(toggle(Lang::En), Lang::Ru);
        assert_eq!(toggle(Lang::Ru), Lang::En);
    }

    #[test]
    fn toggle_twice_is_identity() {
        for lang in [Lang::En, Lang::Ru] {
            assert_eq!(toggle(toggle(lang)), lang);
        }
    }

    #[test]
    fn button_labels_are_localized() {
        assert_eq!(button_label(Lang::En), "Language (en)");
        assert_eq!(button_label(Lang::Ru), "Язык (ru)");
    }

    #[test]
    fn button_labels_include_translation_and_code() {
        for lang in [Lang::En, Lang::Ru] {
            let label = button_label(lang);
            assert!(label.contains(t("language", lang)));
            assert!(label.contains(lang.as_str()));
        }
    }

    #[test]
    fn aria_labels_are_localized() {
        assert_eq!(aria_label(Lang::En), "Language: en");
        assert_eq!(aria_label(Lang::Ru), "Язык: ru");
    }

    #[test]
    fn aria_labels_report_current_language() {
        for lang in [Lang::En, Lang::Ru] {
            let label = aria_label(lang);
            assert!(label.contains(t("language", lang)));
            assert!(label.ends_with(lang.as_str()));
        }
    }
}
