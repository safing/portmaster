//! Pilot Leptos frontend. Replaced by the UI agent.
use leptos::prelude::*;

#[wasm_bindgen::prelude::wasm_bindgen]
pub fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

#[component]
pub fn App() -> impl IntoView {
    view! { <main><h1>"Leptos pilot"</h1></main> }
}

#[wasm_bindgen::prelude::wasm_bindgen(start)]
pub fn main() {
    console_error_panic_hook::set_once();
    leptos::mount::mount_to_body(|| view! { <App/> });
}
