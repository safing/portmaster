#!/usr/bin/env bash
#
# Build the standalone Leptos WASM pilot for the browser.
#
# What it does:
#   1. Ensures the wasm32-unknown-unknown Rust target is installed.
#   2. Builds the `pm_leptos_pilot` crate as a release wasm cdylib.
#   3. Runs wasm-bindgen (--target web) into web/pkg/.
#   4. Stages the generated JS + WASM into web/ so index.html can load them.
#
# Requirements (install once, separately):
#   - Rust toolchain:            rustup target add wasm32-unknown-unknown
#   - wasm-bindgen-cli 0.2.130:  cargo install wasm-bindgen-cli --version 0.2.130
#     (the CLI version must match the `wasm-bindgen` crate version in Cargo.lock)
#
# This script never installs tooling itself; it only checks for it.
set -euo pipefail

# Always work from the directory that holds this script (pilot/leptos/).
cd "$(dirname "${BASH_SOURCE[0]}")"

TARGET="wasm32-unknown-unknown"
CRATE="pm_leptos_pilot"
OUT_DIR="web/pkg"
WEB_DIR="web"

# 1. Check for the wasm target -------------------------------------------------
if ! rustup target list --installed | grep -qx "$TARGET"; then
  echo "error: Rust target '$TARGET' is not installed." >&2
  echo "       Install it with: rustup target add $TARGET" >&2
  exit 1
fi

# 2. Check for wasm-bindgen-cli -----------------------------------------------
if ! command -v wasm-bindgen >/dev/null 2>&1; then
  echo "error: wasm-bindgen CLI not found on PATH." >&2
  echo "       Install it with: cargo install wasm-bindgen-cli --version 0.2.130" >&2
  exit 1
fi

# 3. Build the cdylib ----------------------------------------------------------
echo "==> Building $CRATE for $TARGET (release) ..."
cargo build --release --target "$TARGET"

# Locate the produced .wasm. Cargo places artifacts in the workspace target dir
# when this crate is a workspace member, otherwise in a local ./target dir.
WASM="target/$TARGET/release/$CRATE.wasm"
if [[ ! -f "$WASM" ]]; then
  WS_TARGET="$(cargo metadata --no-deps --format-version 1 \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["target_directory"])')"
  WASM="$WS_TARGET/$TARGET/release/$CRATE.wasm"
fi
if [[ ! -f "$WASM" ]]; then
  echo "error: could not find built wasm for '$CRATE' (looked in ./target and workspace target)." >&2
  exit 1
fi

# 4. Generate browser bindings -------------------------------------------------
echo "==> Running wasm-bindgen (--target web) ..."
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR" "$WEB_DIR"
wasm-bindgen --target web --out-dir "$OUT_DIR" "$WASM"

# 5. Stage artifacts at web/ root ---------------------------------------------
# index.html references ./pm_leptos_pilot.js, whose init() locates the sibling
# pm_leptos_pilot_bg.wasm via import.meta.url. Keep both in the same directory.
cp "$OUT_DIR/$CRATE.js" "$WEB_DIR/$CRATE.js"
cp "$OUT_DIR"/*.wasm "$WEB_DIR/"

echo "==> Done. Generated files in $WEB_DIR/:"
ls -1 "$WEB_DIR/$CRATE.js" "$WEB_DIR"/*.wasm
echo
echo "Serve locally with: python3 -m http.server 8080 -d web"
echo "Then open:          http://localhost:8080/"
