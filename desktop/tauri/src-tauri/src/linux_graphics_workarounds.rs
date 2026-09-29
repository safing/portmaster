//! Linux-only graphics workarounds, applied at the start of `main()` before
//! any GTK/WebKit initialisation and before any thread exists. Kept in its own
//! module so it can be adjusted or removed once the upstream issue is fixed.
//!
//! # Problem
//!
//! On Wayland with the NVIDIA driver (proprietary or open kernel module) the UI
//! exits right after the main window is created:
//!
//! ```text
//! Gdk-Message: Error 71 (Protocol error) dispatching to Wayland display.
//! ```
//!
//! WebKitGTK and NVIDIA's Wayland explicit-sync implementation do not work
//! together. Upstream: <https://bugs.webkit.org/show_bug.cgi?id=280210>,
//! Tauri: <https://github.com/tauri-apps/tauri/issues/10702>,
//! Portmaster: <https://github.com/safing/portmaster/issues/1898>.
//!
//! # Workarounds
//!
//! Reference: <https://v2.tauri.app/develop/debug/linux-graphics/>.
//! Three environment variables avoid the crash; each is enabled with `1`:
//!
//! | Variable                            | Effect                                   | GPU accel | Confirmed in Portmaster |
//! |-------------------------------------|------------------------------------------|-----------|-------------------------|
//! | `__NV_DISABLE_EXPLICIT_SYNC`        | NVIDIA driver falls back to implicit sync| kept      | no                      |
//! | `WEBKIT_DISABLE_DMABUF_RENDERER`    | WebKit rasterises on CPU, shared memory  | lost      | #1898, #1806            |
//! | `WEBKIT_DISABLE_COMPOSITING_MODE`   | WebKit disables accelerated compositing  | lost      | #2021                   |
//!
//! `WORKAROUND_ENV_VAR` selects the one applied. The NVIDIA variable is used
//! because it has no performance cost.
//!
//! What each variable can fix, should another one be chosen later:
//! - `__NV_DISABLE_EXPLICIT_SYNC`: only the Wayland crash on NVIDIA. No effect
//!   on X11 or on other GPUs. No other code change needed.
//! - `WEBKIT_DISABLE_DMABUF_RENDERER`: the Wayland crash on NVIDIA, the blank
//!   window on NVIDIA under X11/XWayland, and GPU buffer failures on other
//!   setups (e.g. virtual machines). Requires dropping the Wayland condition
//!   in `is_wayland_session()`.
//! - `WEBKIT_DISABLE_COMPOSITING_MODE`: everything the DMA-BUF variable
//!   covers, plus a silent crash on window resize. Same code changes as above.
//!
//! # Conditions
//!
//! The variable is set only when the session is Wayland, the NVIDIA kernel
//! module is loaded (`/sys/module/nvidia`, excludes nouveau) and the variable
//! is not already present in the environment. An existing value, including
//! `0`, is never overridden, so the workaround can be disabled without a
//! rebuild.

use std::path::Path;

/// The workaround to apply. See the module documentation for the alternatives
/// (`WEBKIT_DISABLE_DMABUF_RENDERER`, `WEBKIT_DISABLE_COMPOSITING_MODE`).
const WORKAROUND_ENV_VAR: &str = "__NV_DISABLE_EXPLICIT_SYNC";
/// "1" enables all three known variables, so this does not need to change
/// when switching `WORKAROUND_ENV_VAR`.
const WORKAROUND_ENV_VALUE: &str = "1";
/// Present when the NVIDIA kernel module (proprietary or open) is loaded.
const NVIDIA_MODULE_SYSFS: &str = "/sys/module/nvidia";

/// Applies the workarounds and returns human readable messages describing what
/// was (or was not) done. The logger is not initialised yet when this runs, so
/// the caller is expected to log the messages later.
pub fn apply() -> Vec<String> {
    let mut messages = Vec::new();

    if !is_wayland_session() {
        messages.push("linux graphics workarounds: not a Wayland session, nothing to do".into());
        return messages;
    }

    if !is_nvidia_module_loaded() {
        messages.push("linux graphics workarounds: NVIDIA kernel module not loaded, nothing to do".into());
        return messages;
    }

    if let Ok(value) = std::env::var(WORKAROUND_ENV_VAR) {
        messages.push(format!(
            "linux graphics workarounds: {}={:?} already set in environment, leaving it untouched",
            WORKAROUND_ENV_VAR, value
        ));
        return messages;
    }

    // Safe: called from the start of main() before any other thread exists.
    std::env::set_var(WORKAROUND_ENV_VAR, WORKAROUND_ENV_VALUE);
    messages.push(format!(
        "linux graphics workarounds: NVIDIA driver on Wayland detected, set {}={} to avoid WebKitGTK 'Error 71' crash (see issue #1898)",
        WORKAROUND_ENV_VAR, WORKAROUND_ENV_VALUE
    ));

    messages
}

/// True when GTK may use its Wayland backend.
///
/// `WAYLAND_DISPLAY` is set by the compositor for every client of a Wayland
/// session. `GDK_BACKEND` can be used to restrict the backend; it accepts a
/// comma separated priority list (e.g. `wayland,x11`) where `*` means "try all
/// remaining backends", so the check is whether `wayland` is still a candidate.
fn is_wayland_session() -> bool {
    let has_wayland_display = std::env::var_os("WAYLAND_DISPLAY")
        .map(|v| !v.is_empty())
        .unwrap_or(false);
    if !has_wayland_display {
        return false;
    }

    match std::env::var("GDK_BACKEND") {
        Ok(backends) => backends
            .split(',')
            .any(|b| b == "*" || b == "wayland"),
        Err(_) => true,
    }
}

/// True when the NVIDIA kernel module is loaded. Both the proprietary
/// `nvidia.ko` and the open kernel module register under this name; nouveau
/// registers as `nouveau` and is therefore excluded.
fn is_nvidia_module_loaded() -> bool {
    Path::new(NVIDIA_MODULE_SYSFS).is_dir()
}
