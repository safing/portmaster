//! Pure, DOM-free logic for the shell component.
//!
//! The visual layer can build its inline `style` attribute from
//! [`shell_style()`] and its heading text from [`title_for()`] without this
//! module depending on Leptos, `web-sys`, or any signals. That keeps the
//! interesting bits unit-testable on a native target.

/// Maximum width of the shell card, in `rem`.
///
/// Kept as a `u32` so it can be interpolated straight into the inline CSS
/// string produced by [`shell_style()`].
pub const CARD_MAX_WIDTH_REM: u32 = 48;

/// Inline CSS for the shell container.
///
/// Centres the card and caps its width using [`CARD_MAX_WIDTH_REM`], so the
/// single source of truth for the width is this module rather than a magic
/// number buried in a view macro.
pub fn shell_style() -> String {
    format!(
        "font-family: system-ui, sans-serif; max-width: {CARD_MAX_WIDTH_REM}rem; margin: 2rem auto;"
    )
}

/// Translated document title for the given language.
pub fn title_for(lang: crate::model::Lang) -> String {
    crate::model::t("title", lang).to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::Lang;

    #[test]
    fn card_max_width_is_48_rem() {
        assert_eq!(CARD_MAX_WIDTH_REM, 48);
    }

    #[test]
    fn shell_style_uses_the_card_width_constant() {
        let style = shell_style();
        assert!(
            style.contains(&format!("max-width: {CARD_MAX_WIDTH_REM}rem")),
            "style should embed the constant: {style}"
        );
        assert!(style.contains("margin: 2rem auto"), "style should centre the card: {style}");
    }

    #[test]
    fn title_for_returns_the_translated_title() {
        assert_eq!(title_for(Lang::En), crate::model::t("title", Lang::En));
        assert_eq!(title_for(Lang::Ru), crate::model::t("title", Lang::Ru));
        assert_ne!(title_for(Lang::En), title_for(Lang::Ru));
    }
}
