//! Visual layer for the nodes component.
//!
//! Renders the static [`crate::model::DEMO_NODES`] as a styled list. All
//! formatting decisions live in [`super::logic`], so this module only deals
//! with markup.

use leptos::prelude::*;

/// Styled, non-reactive list of the demo proxy nodes.
#[component]
pub fn NodeList() -> impl IntoView {
    let title = crate::model::t("nodes", crate::model::Lang::En);

    let rows = crate::model::DEMO_NODES
        .iter()
        .enumerate()
        .map(|(index, node)| {
            let class = super::logic::row_class(index);
            let badge = super::logic::country_badge(&node);

            view! {
                <li class=class>
                    <span class="node-label">{node.label()}</span>
                    <span class="node-badge">{badge}</span>
                </li>
            }
        })
        .collect::<Vec<_>>();

    view! {
        <section class="nodes">
            <h2 class="nodes-title">{title}</h2>
            <ul class="nodes-list" style="list-style: none; margin: 0; padding: 0;">
                {rows}
            </ul>
        </section>
    }
}
