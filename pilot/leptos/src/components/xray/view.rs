//! Visual layer for the xray component.
//!
//! Owns the connection / subscription UI state and delegates every pure
//! label + colour decision to [`super::logic`].

use leptos::prelude::*;

use crate::model::{t, Lang};

#[component]
pub fn XrayPanel() -> impl IntoView {
    // Local reactive state for this panel.
    let (connected, set_connected) = create_signal(false);
    let (subscription, set_subscription) = create_signal(String::new());

    // The panel is self-contained, so it uses the default UI language.
    let lang = Lang::En;

    view! {
        <section>
            <h2>{t("status", lang)}</h2>

            <p style:color=move || super::logic::status_color(connected.get())>
                {move || t(
                    if connected.get() { "connected" } else { "disconnected" },
                    lang,
                )}
            </p>

            <button on:click=move |_| set_connected.set(!connected.get())>
                {move || super::logic::button_label(connected.get(), lang)}
            </button>

            <label style="display: block; margin-top: 1rem;">
                {t("subscription", lang)}
                <input
                    type="text"
                    placeholder="https://example.com/subscription"
                    prop:value=move || subscription.get()
                    on:input=move |ev| set_subscription.set(event_target_value(&ev))
                />
            </label>

            <small>"Paste a subscription URL above, then toggle the connection."</small>
        </section>
    }
}
