//! Visual layer for the counter component.
//!
//! Self-contained reactive counter: one internal signal drives both buttons
//! and the `<output>` element. No global state, no `wasm_bindgen` glue.

use leptos::prelude::*;

/// A tiny counter whose state lives entirely inside the component.
///
/// The `RwSignal` returned by [`signal`] is `Copy`, so the same `count` handle
/// is shared by the two click handlers and the reactive read in `<output>`.
#[component]
pub fn Counter() -> impl IntoView {
    let (count, set_count) = create_signal(0i32);

    view! {
        <section class="counter">
            <h2>{crate::model::t("counter", crate::model::Lang::En)}</h2>
            <button
                type="button"
                aria-label="decrement"
                on:click=move |_| set_count.update(|n| *n -= 1)
            >
                "-1"
            </button>
            <output>{move || count.get()}</output>
            <button
                type="button"
                aria-label="increment"
                on:click=move |_| set_count.update(|n| *n += 1)
            >
                "+1"
            </button>
        </section>
    }
}
