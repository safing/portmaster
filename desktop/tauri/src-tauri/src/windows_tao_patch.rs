//! Windows-only: guard for the patched `tao` dependency, plus a debug-only
//! stress test that reproduces the crash the patch fixes. Kept in its own
//! module so it can be removed as a whole once upstream ships a fix.
//!
//! # Problem
//!
//! The UI process (`portmaster.exe`) died randomly on Windows with
//! `Illegal instruction (0xc000001d)`, often preceded by
//! `STATUS_HEAP_CORRUPTION (0xc0000374)`. Both codes are one bug seen at two
//! moments. Portmaster issues:
//! <https://github.com/safing/portmaster/issues/2185> (v2.1.19),
//! <https://github.com/safing/portmaster/issues/2255> (v2.2.3).
//!
//! Cause: `tauri_runtime_wry::Context` derives `Clone` and contains tao's
//! `EventLoopWindowTarget`. On Windows that struct holds the event-loop runner
//! behind `Rc<EventLoopRunner>`, a plain non-atomic reference count. Tauri
//! clones `Context` on every `AppHandle`, `Window` or `Webview` clone, from
//! any thread (IPC commands, emits, tray updates, window lookups). Two threads
//! cloning at the same instant lose an update, the runner is freed while still
//! in use, and the next clone aborts. The collision window is nanoseconds, so
//! normal use needs hours or days to hit it.
//!
//! Upstream: issue <https://github.com/tauri-apps/tauri/issues/15408>,
//! Tauri-side fix <https://github.com/tauri-apps/tauri/pull/15411> (open),
//! tao-side fix <https://github.com/tauri-apps/tao/pull/1334> (same change as
//! ours, closed unmerged). As of 2026-10-02 the Tauri pull request is still
//! open, so no released Tauri version contains a fix (latest checked: tauri
//! 2.12.0 and the 3.0.0 alpha).
//!
//! Not the cause: `std::env::set_var` in `window.rs`. Rust documents it as
//! sound on Windows in multi-threaded programs.
//!
//! # Fix
//!
//! Portmaster builds against a patched tao from
//! <https://github.com/safing/portmaster-tauri-tao-patch>, branch
//! `portmaster/windows-atomic-refcount-0.32.8`, pinned by commit in
//! `Cargo.toml` under `[patch.crates-io]`. The patch changes the runner's
//! reference count from `Rc` to `Arc` (two lines in
//! `src/platform_impl/windows/`) and adds the marker constant
//! `tao::PORTMASTER_PATCH_LEVEL`. Only the count becomes atomic; the runner
//! keeps its single-threaded interior. The fork's README has the full diff.
//!
//! # Guard
//!
//! If a dependency update makes Cargo drop the `[patch]` entry (for example a
//! Tauri bump that requires a newer tao), Cargo prints one warning and builds
//! the unpatched crate. The `const` assertion below turns that into a compile
//! error: the marker constant does not exist in upstream tao. It is reached
//! through `tauri_runtime_wry::tao`, so it is guaranteed to be the tao that
//! Tauri compiles against, not a second copy. `tauri-runtime-wry` is a direct
//! dependency for this reason only.
//!
//! # Verifying the fix, and whether the patch is still needed
//!
//! Debug builds contain a stress test, enabled with the command-line flag
//! `--verify-tao-patch`. It clones and drops the `AppHandle` from four threads
//! in tight loops, which makes the race happen within seconds instead of days,
//! and logs a line every million clones. It never stops by itself: end the
//! test by stopping the UI. Release builds do not contain it; they ignore the
//! flag because the CLI parser in `cli.rs` skips unknown flags. The flag is
//! checked here, not in `cli.rs`, so that everything belonging to the patch
//! stays in this module.
//!
//! ```text
//! cd desktop\tauri\src-tauri
//! cargo build --profile dev        # debug build; a --release build has no stress test
//! .\target\debug\portmaster.exe --background --log=warn --verify-tao-patch
//! ```
//!
//! Unpatched tao: crashes within seconds (panic in `Rc::inc_strong` or
//! Illegal instruction). Patched tao: "[stress] ... still alive" lines keep
//! coming. To check whether upstream has fixed the bug after a Tauri upgrade,
//! temporarily remove the `[patch.crates-io]` entry and this module's
//! assertion, run `cargo build --profile dev` (Cargo re-resolves tao from
//! crates.io by itself), and run the same test for ten minutes.
//!
//! # Removing the patch
//!
//! When a released Tauri contains the fix: delete the `[patch.crates-io]`
//! entry and the `tauri-runtime-wry` dependency in `Cargo.toml`, delete this
//! module and its two references in `main.rs`, run `cargo check` so Cargo
//! rewrites the lock entry back to crates.io.

use tauri::AppHandle;

/// Marker level that the patched tao fork must export. Bump together with the
/// fork when the patch content changes.
const EXPECTED_TAO_PATCH_LEVEL: u32 = 1;

// Compile-time guard, see the module documentation. Do not remove while the
// `[patch.crates-io]` entry for tao is in use.
const _: () = assert!(
    tauri_runtime_wry::tao::PORTMASTER_PATCH_LEVEL == EXPECTED_TAO_PATCH_LEVEL,
    "tao patch level does not match, see src/windows_tao_patch.rs"
);

/// Starts the handle-clone stress test if `--verify-tao-patch` was given on the
/// command line. Debug builds only; see the module documentation.
#[cfg(debug_assertions)]
pub fn maybe_start_handle_clone_stress_test(app: &AppHandle) {
    use std::time::Instant;

    const VERIFY_FLAG: &str = "--verify-tao-patch";
    const THREADS: usize = 4;
    const LOG_EVERY: u64 = 1_000_000;

    if !std::env::args_os().skip(1).any(|arg| arg == VERIFY_FLAG) {
        return; // normal case: flag not given, the stress test stays off
    }

    log::warn!(
        "[stress] handle clone stress test enabled with {THREADS} threads ({VERIFY_FLAG} given). \
         It runs until the process exits: a crash means the tao patch is missing or broken, \
         'still alive' lines mean it works. Stop the UI manually to end the test."
    );
    let started = Instant::now();

    for thread_index in 0..THREADS {
        let handle = app.clone();
        std::thread::spawn(move || {
            let mut clones: u64 = 0;
            loop {
                let cloned = handle.clone(); // Context clone -> runner refcount increment
                drop(cloned); // refcount decrement
                clones += 1;
                if clones % LOG_EVERY == 0 {
                    log::warn!(
                        "[stress] thread {thread_index}: {clones} clones, still alive after {:?}",
                        started.elapsed()
                    );
                }
            }
        });
    }
}

/// Release builds do not contain the stress test; the flag is ignored.
#[cfg(not(debug_assertions))]
pub fn maybe_start_handle_clone_stress_test(_app: &AppHandle) {}
