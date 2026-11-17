//! Pilot Leptos 0.8 CSR frontend.
//!
//! A tiny but real client-side app: a reactive counter, an EN/RU language
//! toggle, and a fake "Xray status" panel with a node list. All translatable
//! strings and node formatting live in the DOM-free [`model`] module.

// `create_signal` is the classic 0.8 name for `signal()`. It is deprecated in
// favour of `signal()` but still exported from `leptos::prelude`, so we allow
// the deprecation here to keep the rest of the crate warning-free.
#![allow(deprecated)]

use leptos::prelude::*;

mod model;

use model::{t, Lang, DEMO_NODES};

#[component]
pub fn App() -> impl IntoView {
    // --- reactive state -------------------------------------------------
    let (count, set_count) = create_signal(0i32);
    let (lang, set_lang) = create_signal(Lang::En);
    let (connected, set_connected) = create_signal(false);
    let (subscription, set_subscription) = create_signal(String::new());

    // --- derived, reactive labels ---------------------------------------
    let status_text = move || t(if connected.get() { "connected" } else { "disconnected" }, lang.get());
    let status_color = move || {
        if connected.get() {
            "seagreen"
        } else {
            "crimson"
        }
    };
    let connect_label = move || t(if connected.get() { "disconnect" } else { "connect" }, lang.get());

    // The demo nodes are static, so rendering them once is enough. Keeping
    // them in `model` makes it trivial to swap in real subscription data.
    let node_items = DEMO_NODES
        .iter()
        .map(|node| view! { <li>{node.label()}</li> })
        .collect::<Vec<_>>();

    view! {
        <main style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 2rem auto;">
            <header>
                <h1>{move || t("title", lang.get())}</h1>
                <button on:click=move |_| set_lang.update(|l| *l = (*l).toggle())>
                    {move || format!("{} ({})", t("language", lang.get()), lang.get().as_str())}
                </button>
            </header>

            <section>
                <h2>{move || t("counter", lang.get())}</h2>
                <button on:click=move |_| set_count.update(|n| *n -= 1) aria-label="decrement">
                    "-1"
                </button>
                <output>{move || count.get()}</output>
                <button on:click=move |_| set_count.update(|n| *n += 1) aria-label="increment">
                    "+1"
                </button>
            </section>

            <section>
                <h2>{move || t("status", lang.get())}</h2>
                <p style:color=status_color>{status_text}</p>
                <button on:click=move |_| set_connected.update(|c| *c = !*c)>
                    {connect_label}
                </button>

                <label style="display: block; margin-top: 1rem;">
                    {move || t("subscription", lang.get())}
                    <input
                        type="text"
                        placeholder="https://example.com/subscription"
                        prop:value=move || subscription.get()
                        on:input=move |ev| set_subscription.set(event_target_value(&ev))
                    />
                </label>

                <h3>{move || t("nodes", lang.get())}</h3>
                <ul>{node_items}</ul>
            </section>
        </main>
    }
}

#[wasm_bindgen::prelude::wasm_bindgen(start)]
pub fn main() {
    console_error_panic_hook::set_once();
    leptos::mount::mount_to_body(|| view! { <App/> });
}
