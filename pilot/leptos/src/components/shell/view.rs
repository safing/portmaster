//! Visual layer for the shell component.
//!
//! The shell is a purely presentational frame: a centered card with a header
//! area that wraps whatever content the caller passes as `children`. Styling
//! is kept inline so the component is self-contained and does not depend on
//! the HTML shell's stylesheet.

use leptos::prelude::*;

/// Centered dark-theme card that frames the pilot's UI.
///
/// Renders a semantic `<main>` landmark with a `<header>` area, a max width of
/// roughly 48rem, and fluid padding that shrinks on narrow viewports. All
/// colors mirror the palette used by `web/index.html` for a coherent look.
#[component]
pub fn Shell(children: Children) -> impl IntoView {
    view! {
        <main
            style="
                box-sizing: border-box;
                width: 100%;
                max-width: 48rem;
                margin: clamp(1rem, 4vw, 3rem) auto;
                padding: clamp(1rem, 3vw, 2rem);
                background: #131a22;
                border: 1px solid #2a3644;
                border-radius: 12px;
                box-shadow: 0 14px 40px rgba(0, 0, 0, 0.35);
                color: #e6edf3;
                font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
                line-height: 1.5;
            "
        >
            <header
                style="
                    display: flex;
                    align-items: baseline;
                    justify-content: space-between;
                    flex-wrap: wrap;
                    gap: 0.5rem 1rem;
                    margin-bottom: 1.25rem;
                    padding-bottom: 0.75rem;
                    border-bottom: 1px solid #2a3644;
                "
            >
                <h1 style="margin: 0; font-size: 1.15rem; letter-spacing: 0.02em;">
                    "Portmaster / Leptos pilot"
                </h1>
                <span
                    style="
                        font-size: 0.75rem;
                        color: #96a4b3;
                        border: 1px solid #2a3644;
                        border-radius: 999px;
                        padding: 0.15rem 0.6rem;
                    "
                >
                    "wasm32 · --target web"
                </span>
            </header>

            <div style="display: grid; gap: 1.25rem;">
                {children()}
            </div>
        </main>
    }
}
