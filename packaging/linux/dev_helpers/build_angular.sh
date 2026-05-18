#!/usr/bin/env bash
set -euo pipefail

# This script builds the Angular project for the Portmaster application and packages it into a zip file.
# The script assumes that all necessary dependencies are installed and available.
# Output file: dist/portmaster.zip

DEVELOPMENT=false
INTERACTIVE=false

usage() {
  cat <<'EOF'
Usage: build_angular.sh [options]

Options:
  -d, --development   Build Angular and libs in development mode
  -i, --interactive   Ask before running install and libs build steps
  -h, --help          Show this help message
EOF
}

have() {
  command -v "$1" >/dev/null 2>&1
}

ask_yes_no_default_yes() {
  local prompt=$1
  local reply
  read -r -p "$prompt (Y/N, default: Y) " reply
  reply=${reply:-Y}
  [[ ! $reply =~ ^[Nn]$ ]]
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--development)
      DEVELOPMENT=true
      shift
      ;;
    -i|--interactive)
      INTERACTIVE=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../../../" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/dist"
ORIGINAL_DIR="$(pwd)"

cleanup() {
  cd "${ORIGINAL_DIR}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "${OUTPUT_DIR}"

if ! have npm; then
  echo "Error: npm not found in PATH." >&2
  exit 1
fi
if ! have zip; then
  echo "Error: zip not found in PATH." >&2
  echo "Install it using your package manager (for example: sudo apt install zip)." >&2
  exit 1
fi

cd "${PROJECT_ROOT}/desktop/angular"

if ! $INTERACTIVE || ask_yes_no_default_yes "Run 'npm install'?"; then
  npm install
fi

if ! $INTERACTIVE || ask_yes_no_default_yes "Build shared libraries?"; then
  if $DEVELOPMENT; then
    echo "Building shared libraries in development mode"
    npm run build-libs:dev
  else
    echo "Building shared libraries in production mode"
    npm run build-libs
  fi
fi

if $DEVELOPMENT; then
  echo "Building Angular project in development mode"
  ./node_modules/.bin/ng build --configuration development --base-href /ui/modules/portmaster/ portmaster
else
  echo "Building Angular project in production mode"
  NODE_ENV=production ./node_modules/.bin/ng build --configuration production --base-href /ui/modules/portmaster/ portmaster
fi

DESTINATION_ZIP="${OUTPUT_DIR}/portmaster.zip"
echo "Creating zip archive"
rm -f "${DESTINATION_ZIP}"
(
  cd dist
  zip -r "${DESTINATION_ZIP}" .
)

echo "Build completed successfully: ${DESTINATION_ZIP}"
echo
echo "To replace the currently installed UI bundle, use:"
echo "  sudo cp -f /usr/lib/portmaster/portmaster.zip /usr/lib/portmaster/portmaster.zip.bak"
echo "  sudo cp -f \"${DESTINATION_ZIP}\" /usr/lib/portmaster/portmaster.zip"
