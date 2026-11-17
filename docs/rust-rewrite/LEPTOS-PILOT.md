# Leptos pilot — evaluation only

Status, as of 2026-11-17: a feasibility experiment, **not** a candidate for
production. The production UI remains the Angular app in `desktop/angular`,
packaged with Tauri from `desktop/tauri`. The pilot is being assembled in
parallel worktrees, so individual files may still be in flight; re-check the
paths below before relying on a detail.

## 1. What the pilot is

- A small, standalone client-side-rendered (CSR) Leptos app in `pilot/leptos/`.
- Crate `pm-leptos-pilot`, `cdylib` + `rlib`, `leptos = 0.8.23` with the `csr`
  feature, `wasm-bindgen = 0.2.130`. It declares its own `[workspace]`
  (`pilot/leptos/Cargo.toml`) so WASM-only dependencies cannot affect the main
  Rust core workspace, its MSRV, or its lint table.
- It is built for `wasm32-unknown-unknown` and loaded directly by the browser
  via `wasm-bindgen --target web`; there is no bundler, trunk, or cargo-leptos.
  `pilot/leptos/build.sh` builds the crate, runs wasm-bindgen, and stages
  `web/pm_leptos_pilot.js` + `web/pm_leptos_pilot_bg.wasm` next to
  `web/index.html` (raw output stays in `web/pkg/`).
- The demo UI (`pilot/leptos/src/lib.rs`, `src/model.rs`) is a reactive counter,
  a tiny EN/RU label toggle, and a fake "Xray status" panel with two hard-coded
  demo nodes. All state is local; **it makes no API calls and shows no real
  connection, profile, intel, or netquery data**. Its own tests are native unit
  tests of the pure `src/model.rs` helpers — there are no DOM/WASM or browser
  tests.
- Serving: the page needs HTTP (`file://` will not work with ES modules). The
  intended integration is that `pm-core` can optionally expose the built pilot
  assets at `/pilot/`, alongside — never instead of — the production routes
  `/` and `/ui/modules/<module>/*` implemented in `crates/pm-api/src/ui.rs`
  (which serves `portmaster.zip` and `assets.zip` from the binary directory).
  **Honesty note:** that `/pilot` route is not present in the source tree at
  the time of writing; until the serving integration lands, use the static
  server in §4. The route is opt-in and is not part of any default install.

## 2. Why it is NOT a replacement for Angular yet

| Area | Production UI (`desktop/angular`, `desktop/tauri`) | Pilot today |
|---|---|---|
| Component library / design system | `ng-zorro-antd` (Ant Design) + FontAwesome, Tailwind/SCSS theme in `desktop/angular/src/theme/`; tables, modals, selects, date pickers, tooltips, charts (d3) | Inline styles and raw HTML elements only. No component kit, no icon set, no tokens/theme system. (The production UI uses Ant, not Angular Material — "Material" here just means "a real component library".) |
| RxJS services / API layer | 15 `*.service.ts` in `desktop/angular/src/app/services/` plus the generated client `desktop/angular/projects/safing/portmaster-api/` (`config`, `netquery`, `portapi`, `spn`, `websocket`, …) with live WS DB subscriptions | No backend at all. `crates/pm-api-client` exists as a Rust WS client but the pilot does not use it; no HTTP client, no typed models, no auth/dev-mode handshake, no reconnect. |
| Routing | `desktop/angular/src/app/app-routing.module.ts`: 10 routes (`dashboard`, `settings`, `app/overview`, `app/:source/:id`, `monitor`, `support`, `spn`, `xray`, wildcard) | One view, no router, no deep links, no history/back-button behaviour. |
| Forms | `@angular/forms`, validation and error states throughout settings/support flows | One raw `<input>` bound to a signal; no validation, async errors, dirty/pristine state, or persistence. |
| i18n coverage | `desktop/angular/src/i18n/`: `translations.en.ts`, `translations.ru.ts` (hundreds of keys), `helptexts*.yaml`, `translate`/`localized-value`/`i18n-version` pipes, `I18nService.language$` for live switching | Hand-rolled ~10-key dictionary in `src/model.rs`; no interpolation, plurals, number/date formatting, helptext system, or translation workflow. |
| Tauri integration | `desktop/tauri/src-tauri/src/`: commands, tray menu, windows, notifications, service manager, `portapi` client, plugins (`@tauri-apps/*`), `tauri-builtin` Angular project | Plain browser page. No tray, no native notifications, no service control, no window/deep-link handling, no packaging or installer story. |
| Accessibility | An accumulated (still imperfect) a11y baseline across the Angular components | Essentially unaudited: two `aria-label`s on the counter buttons, no focus management, keyboard navigation, screen-reader pass, contrast or reduced-motion checks. |
| Testing | Karma unit tests (`ng test`), Protractor e2e (`ng e2e`), plus the Rust core's fixture-based compatibility suites (`docs/rust-rewrite/SPEC.md`) | A handful of native unit tests for `model.rs` only. No WASM/DOM tests, no e2e, no visual regression. |

Also missing: settings persistence, theme/light-dark switching, zoom, real-time
updates, and the charts/maps used by monitor and Xray. The pilot proves "Leptos
can build and render in a browser"; it does not prove "Leptos can run
Portmaster's UI".

## 3. What a real migration would need

Rough order and order-of-magnitude sizing for a small team — these are not a
committed schedule, and the later phases are open-ended. The current pilot is
a tiny fraction of this work.

| Phase | Work | Rough size |
|---|---|---|
| 0. Scope & parity inventory | Decide whether to migrate at all; route-by-route and widget-by-widget checklist against the Angular UI; define acceptance criteria and rollback. | 1–2 weeks |
| 1. Build & delivery foundation | Replace the hand-written shell/build.sh with a reproducible pipeline (e.g. trunk or cargo-leptos), versioned hashed assets, CI for WASM, opt-in `/pilot`-style serving compatible with the UI upgrade lock in `crates/pm-api/src/ui.rs`. | 2–4 weeks |
| 2. API & data layer | Rust client over HTTP + WS DB API (build on `crates/pm-api-client`), typed models, subscriptions, reconnect/resuscitation, dev-mode/auth, fixture-based parity tests against Go/Rust core. | 1–2 months |
| 3. Design system | Component kit and tokens equivalent to ng-zorro-antd + FontAwesome + theme, including tables/modals/selects and d3-equivalent charts/maps. | 1–2 months |
| 4. Feature parity | Port the actual pages one at a time: dashboard → monitor/app view → settings/forms → support → xray/SPN. Settings persistence and live updates included. | Several months (8–16+ weeks) |
| 5. i18n | Port hundreds of EN/RU keys and helptexts, live switching, pipes/formatting, and an ongoing translation maintenance process. | 2–4 weeks for plumbing; maintenance forever |
| 6. Desktop & packaging | Wire `tauri-builtin`/`desktop/tauri` plugins, tray, notifications, service manager, installers, Windows/Linux parity. | 1–2 months |
| 7. a11y, tests, rollout | Keyboard/screen-reader audit, visual and e2e tests, staged A/B behind a flag with rollback. | 1–2 months, then continuous |

Order-of-magnitude total: **a few months for a usable subset, on the order of
a year for full parity and stabilisation** — not a sprint, and not something a
static demo can justify on its own. Re-estimate after phase 2, when there is a
real API-connected slice to measure.

## 4. Run, build, and A/B compare

Build the pilot (from the repository root, note the separate workspace):

```sh
rustup target add wasm32-unknown-unknown
cargo install wasm-bindgen-cli --version 0.2.130   # must match Cargo.lock
cd pilot/leptos
./build.sh                                          # stages JS + WASM into web/
python3 -m http.server 8080 -d web                  # serve over HTTP, not file://
# open http://localhost:8080/
```

`cargo build` at the repository root does **not** build the pilot. If the
`/pilot` integration is present in your checkout, run the core and open
`http://127.0.0.1:817/pilot/` instead:

```sh
cargo build                         # -> target/debug/portmaster-core-rs
./target/debug/portmaster-core-rs --data-dir /tmp/pm-data --bin-dir /tmp/pm-bin \
  --log-dir /tmp/pm-logs --api-address 127.0.0.1:817 --devmode --log-stdout
```

A/B against the Angular UI, with the **same backend** for both:

1. Start a core (Go or Rust) on `127.0.0.1:817` with dev mode enabled; the
   Angular dev server expects WS at `ws://127.0.0.1:817/api/database/v1`.
2. Angular: `cd desktop/angular && npm start` → `http://localhost:4200/`.
   `desktop/angular/proxy.json` forwards `/api` to `http://localhost:817`.
   The packaged equivalent is `http://127.0.0.1:817/ui/modules/portmaster/`.
3. Pilot: `http://localhost:8080/` (static) or `http://127.0.0.1:817/pilot/`
   when available.
4. Compare at the same viewport/theme with the same data snapshot: console
   errors, first paint, artifact size (`ls -lh pilot/leptos/web/*.wasm
   pilot/leptos/web/*.js`), build time, and interaction feel.

Caveat: today the pilot renders only local demo state, so a data-parity A/B is
not yet possible — add one real API-connected slice first. Also, the HTML
shell (`web/index.html`, `#root`) and the Rust mount target
(`mount_to_body`) come from parallel worktrees; verify they agree after merge.

Core smoke-test procedure: `TESTING.md`. UI commands: `desktop/angular/README.md`
and `desktop/angular/package.json`.

## 5. Decision log

| Date | Decision | Rationale / revisit condition |
|---|---|---|
| 2026-11-17 | **Keep Angular as the production UI.** | It is the only implementation with feature parity, i18n, a11y work, Tauri integration, and tests. |
| 2026-11-17 | **Pilot is for evaluation only.** | It demonstrates WASM + Leptos CSR build/runtime viability, nothing more. No user-facing default is changed. |
| 2026-11-17 | Any `/pilot` route is opt-in and must not displace `/` or `/ui/modules/portmaster/`. | Production must stay reversible; the UI upgrade lock and packaged zips keep working. |
| 2026-11-17 | Do not fork production work from pilot code; do not start phases 3–7 implicitly. | Re-evaluate only after an explicit decision and an API-connected demo exists (see §3, phase 2). |

## References (paths in this repo)

- `pilot/leptos/Cargo.toml`, `pilot/leptos/src/lib.rs`, `pilot/leptos/src/model.rs`
- `pilot/leptos/build.sh`, `pilot/leptos/web/index.html`, `pilot/leptos/README.md`
- `crates/pm-api/src/ui.rs` (production UI serving), `crates/pm-core/src/cli.rs`
- `crates/pm-api-client/` (existing Rust WS client)
- `desktop/angular/src/app/app-routing.module.ts`, `desktop/angular/src/i18n/`, `desktop/angular/package.json`, `desktop/angular/proxy.json`, `desktop/angular/README.md`
- `desktop/tauri/src-tauri/src/`
- `TESTING.md`, `docs/rust-rewrite/SPEC.md`, `docs/rust-rewrite/STATUS.md`
