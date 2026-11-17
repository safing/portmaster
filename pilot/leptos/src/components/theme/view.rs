//! Visual layer for the theme component. Replaced by the visual agent.

use leptos::prelude::*;

#[component]
pub fn ThemeSwitcher() -> impl IntoView {
    let (dark, set_dark) = create_signal(true);

    let label = move || super::logic::label(dark.get(), crate::model::Lang::En);
    let swatch_color = move || super::logic::swatch_color(dark.get());

    view! {
        <div style="display: inline-flex; align-items: center; gap: 0.5rem;">
            <button on:click=move |_| set_dark.set(super::logic::toggle(dark.get()))>
                {label}
            </button>
            <span
                aria-hidden="true"
                style="display: inline-block; width: 1rem; height: 1rem; border: 1px solid #888;"
                style:background-color=swatch_color
            ></span>
        </div>
    }
}
