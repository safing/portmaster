//! Pilot Leptos 0.8 CSR frontend.
//!
//! Components are split into a pure `logic` layer and a visual `view` layer.
//! The DOM-free helpers live in [`model`] and in each component's `logic`
//! module, which makes them unit-testable on the native target.

#![allow(deprecated)]

use leptos::prelude::*;

mod components;
mod model;

use components::{
    counter::view::Counter, lang::view::LangSwitcher, nodes::view::NodeList,
    shell::view::Shell, theme::view::ThemeSwitcher, xray::view::XrayPanel,
};

#[component]
pub fn App() -> impl IntoView {
    let title = move || model::t("title", model::Lang::En);
    view! {
        <Shell>
            <header style="display:flex; align-items:center; justify-content:space-between; gap:1rem; flex-wrap:wrap;">
                <h1 style="margin:0; font-size:1.25rem;">{title}</h1>
                <div style="display:flex; gap:.5rem; flex-wrap:wrap;">
                    <LangSwitcher/>
                    <ThemeSwitcher/>
                </div>
            </header>
            <hr style="border:0; border-top:1px solid #2a3644;"/>
            <Counter/>
            <XrayPanel/>
            <NodeList/>
        </Shell>
    }
}

#[wasm_bindgen::prelude::wasm_bindgen(start)]
pub fn main() {
    console_error_panic_hook::set_once();
    leptos::mount::mount_to_body(|| view! { <App/> });
}
