//! Visual layer for the lang component.
//!
//! Owns the language signal and renders a single button that toggles between
//! the supported locales. Translation strings come from [`crate::model`];
//! the state transition itself is delegated to [`super::logic`].

use leptos::prelude::*;

/// Button that flips the UI language between English and Russian.
#[component]
pub fn LangSwitcher() -> impl IntoView {
    let (lang, set_lang) = create_signal(crate::model::Lang::En);

    view! {
        <button
            on:click=move |_| set_lang.update(|current| *current = super::logic::toggle(*current))
            aria-label=move || crate::model::t("language", lang.get())
        >
            {move || format!("{} ({})", crate::model::t("language", lang.get()), lang.get().as_str())}
        </button>
    }
}
