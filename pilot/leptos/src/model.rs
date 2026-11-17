//! Pure, DOM-free helpers for the Leptos pilot UI.
//!
//! Keeping the dictionary and node formatting here means the interesting
//! logic can be unit-tested on a native target without pulling in `web-sys`
//! or spinning up a browser.

/// UI language. Small enough to be `Copy`, which makes it pleasant to store
/// in a Leptos signal.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Lang {
    En,
    Ru,
}

impl Lang {
    /// Switch between the two supported languages.
    pub fn toggle(self) -> Self {
        match self {
            Lang::En => Lang::Ru,
            Lang::Ru => Lang::En,
        }
    }

    /// Short code, handy for button labels / debugging.
    pub fn as_str(self) -> &'static str {
        match self {
            Lang::En => "en",
            Lang::Ru => "ru",
        }
    }
}

/// Tiny EN/RU dictionary for the labels used by the UI.
///
/// The key is `&'static str` so that an unknown key can be returned as a
/// graceful fallback without allocating. All call sites pass string literals.
pub fn t(key: &'static str, lang: Lang) -> &'static str {
    match (key, lang) {
        ("title", Lang::En) => "Portmaster · Pilot Console",
        ("title", Lang::Ru) => "Portmaster · Пилотная консоль",

        ("counter", Lang::En) => "Counter",
        ("counter", Lang::Ru) => "Счётчик",

        ("connect", Lang::En) => "Connect",
        ("connect", Lang::Ru) => "Подключиться",

        ("disconnect", Lang::En) => "Disconnect",
        ("disconnect", Lang::Ru) => "Отключиться",

        ("status", Lang::En) => "Xray status",
        ("status", Lang::Ru) => "Статус Xray",

        ("connected", Lang::En) => "Connected",
        ("connected", Lang::Ru) => "Подключено",

        ("disconnected", Lang::En) => "Disconnected",
        ("disconnected", Lang::Ru) => "Отключено",

        ("nodes", Lang::En) => "Demo nodes",
        ("nodes", Lang::Ru) => "Демо-узлы",

        ("subscription", Lang::En) => "Subscription URL",
        ("subscription", Lang::Ru) => "URL подписки",

        ("language", Lang::En) => "Language",
        ("language", Lang::Ru) => "Язык",

        ("dark", Lang::En) => "Dark",
        ("dark", Lang::Ru) => "Тёмная",

        ("light", Lang::En) => "Light",
        ("light", Lang::Ru) => "Светлая",

        // Unknown keys are returned verbatim instead of panicking.
        _ => key,
    }
}

/// A demo proxy node. `name + country` is enough to hint at the upcoming
/// subscription / map feature without involving any real backend.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Node {
    pub name: &'static str,
    pub country: &'static str,
}

impl Node {
    /// Human-readable representation used by the rendered list.
    pub fn label(&self) -> String {
        format_node(self.name, self.country)
    }
}

/// Pure formatting helper, deliberately separate from `Node` so it can be
/// tested on its own.
pub fn format_node(name: &str, country: &str) -> String {
    format!("{name} — {country}")
}

/// Two static demo nodes. A `&'static [Node]` keeps the UI rendering code
/// simple and avoids cloning.
pub const DEMO_NODES: &[Node] = &[
    Node {
        name: "Frankfurt-01",
        country: "DE",
    },
    Node {
        name: "Singapore-02",
        country: "SG",
    },
];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dictionary_covers_required_labels() {
        let required = [
            "title",
            "dark",
            "light",
            "counter",
            "connect",
            "status",
            "connected",
            "disconnected",
        ];

        for key in required {
            let en = t(key, Lang::En);
            let ru = t(key, Lang::Ru);
            assert_ne!(en, key, "missing EN translation for {key}");
            assert_ne!(ru, key, "missing RU translation for {key}");
            assert_ne!(en, ru, "EN and RU are identical for {key}");
        }
    }

    #[test]
    fn dictionary_returns_known_translations() {
        assert_eq!(t("connected", Lang::En), "Connected");
        assert_eq!(t("connected", Lang::Ru), "Подключено");
        assert_eq!(t("disconnected", Lang::Ru), "Отключено");
        assert_eq!(t("status", Lang::En), "Xray status");
    }

    #[test]
    fn unknown_key_falls_back_to_itself() {
        assert_eq!(t("not_in_dictionary", Lang::En), "not_in_dictionary");
        assert_eq!(t("not_in_dictionary", Lang::Ru), "not_in_dictionary");
    }

    #[test]
    fn language_toggle_is_symmetric() {
        assert_eq!(Lang::En.toggle(), Lang::Ru);
        assert_eq!(Lang::Ru.toggle(), Lang::En);
        assert_eq!(Lang::En.as_str(), "en");
        assert_eq!(Lang::Ru.as_str(), "ru");
    }

    #[test]
    fn node_formatting_is_stable() {
        assert_eq!(format_node("Frankfurt-01", "DE"), "Frankfurt-01 — DE");

        assert_eq!(DEMO_NODES.len(), 2);
        assert_eq!(DEMO_NODES[0].label(), "Frankfurt-01 — DE");
        assert_eq!(DEMO_NODES[1].label(), "Singapore-02 — SG");
    }
}
