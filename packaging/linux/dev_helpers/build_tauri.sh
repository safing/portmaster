#!/usr/bin/env bash
set -euo pipefail

# This script builds the Tauri application for Portmaster on Linux.
# It optionally builds the required Angular tauri-builtin project first.
# The script assumes that all necessary dependencies (Node.js, Rust, cargo-tauri) are installed
# and that 'npm install' has been run in desktop/angular (the project-local Angular CLI is used).
# Output file: dist/portmaster

# Resolve script directory and project root
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../../../" && pwd)"
ORIGINAL_DIR="$(pwd)"

# Create output directory
OUTPUT_DIR="${SCRIPT_DIR}/dist"
mkdir -p "${OUTPUT_DIR}"

# Helper: check for command availability
have() { command -v "$1" >/dev/null 2>&1; }

# Optional: build Angular tauri-builtin
read -r -p "Build Angular tauri-builtin project? (Y/N, default: Y) " REPLY
REPLY=${REPLY:-Y}
if [[ ! ${REPLY} =~ ^[Nn]$ ]]; then
  ANGULAR_DIR="${PROJECT_ROOT}/desktop/angular"
  # Use the project-local Angular CLI so the version matches package.json
  # and an unrelated 'ng' on PATH (e.g. the ng-common editor) is never used.
  NG="${ANGULAR_DIR}/node_modules/.bin/ng"
  if [[ ! -x "${NG}" ]]; then
    echo "Error: Angular CLI not found at ${NG}." >&2
    echo "Run 'npm install' in ${ANGULAR_DIR} first." >&2
    exit 1
  fi
  # Navigate to Angular project
  pushd "${ANGULAR_DIR}" >/dev/null
  # Build tauri-builtin with production config
  "${NG}" build --configuration production --base-href / tauri-builtin || {
    popd >/dev/null
    cd "${ORIGINAL_DIR}"
    exit 1
  }
  popd >/dev/null
fi

# Navigate to Tauri src-tauri directory
pushd "${PROJECT_ROOT}/desktop/tauri/src-tauri" >/dev/null

# Ensure cargo and tauri plugin are available
if ! have cargo; then
  echo "Error: cargo not found. Install Rust toolchain (rustup)." >&2
  exit 1
fi
if ! cargo tauri --help >/dev/null 2>&1; then
  echo "Error: cargo-tauri not installed." >&2
  echo "Install via: cargo install tauri-cli" >&2
  popd >/dev/null
  exit 1
fi

# Build Tauri project (no bundle)
cargo tauri build --no-bundle

# Copy built binary to dist
TAURI_OUTPUT_DIR="$(pwd)/target/release"
if [[ -f "${TAURI_OUTPUT_DIR}/portmaster" ]]; then
  cp -f "${TAURI_OUTPUT_DIR}/portmaster" "${OUTPUT_DIR}/"
  echo "Build completed successfully: ${OUTPUT_DIR}/portmaster"
else
  echo "Error: Built binary not found at ${TAURI_OUTPUT_DIR}/portmaster" >&2
  popd >/dev/null
  exit 1
fi

# Return to original directory
popd >/dev/null
cd "${ORIGINAL_DIR}"
