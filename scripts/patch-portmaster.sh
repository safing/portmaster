#!/usr/bin/env bash
#
# Portmaster Patcher / Installer for Linux
# Replaces or upgrades the Portmaster core with the Rust core (pm-core)
# and modern unlocked web UI with Xray proxy integration.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/VLOD-ZDOV/portmaster-rust/main/scripts/patch-portmaster.sh | sudo bash
#   sudo ./scripts/patch-portmaster.sh [--core rs|go] [--tag latest|<version>] [--restore]
#
set -euo pipefail

REPO="VLOD-ZDOV/portmaster-rust"
TARGET_CORE="rs"
TAG="latest"
ACTION="patch"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --core)
      TARGET_CORE="$2"
      shift 2
      ;;
    --tag)
      TAG="$2"
      shift 2
      ;;
    --restore)
      ACTION="restore"
      shift
      ;;
    --help|-h)
      echo "Portmaster Patcher (Linux)"
      echo "Usage: $0 [options]"
      echo "  --core rs|go       Choose core implementation (default: rs)"
      echo "  --tag <tag>        GitHub release tag or 'latest' (default: latest)"
      echo "  --restore          Restore previous core from backup"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [[ ${EUID} -ne 0 ]]; then
  echo "[!] Error: Root privileges required. Run with sudo." >&2
  exit 1
fi

ARCH=$(uname -m)
if [[ "${ARCH}" != "x86_64" && "${ARCH}" != "amd64" ]]; then
  echo "[!] Unsupported architecture: ${ARCH}. Only x86_64 / amd64 is supported." >&2
  exit 1
fi

# Detect install directory
INSTALL_DIR="/usr/lib/portmaster"
if [[ ! -d "${INSTALL_DIR}" && -d "/opt/safing/portmaster" ]]; then
  INSTALL_DIR="/opt/safing/portmaster"
fi
DATA_DIR="/var/lib/portmaster"
BACKUP_DIR="${DATA_DIR}/core-backups"

mkdir -p "${INSTALL_DIR}" "${DATA_DIR}" "${BACKUP_DIR}"

if [[ "${ACTION}" == "restore" ]]; then
  echo "[*] Restoring from backup..."
  LATEST_BACKUP=$(ls -1td "${BACKUP_DIR}"/*/ 2>/dev/null | head -n1 || true)
  if [[ -z "${LATEST_BACKUP}" || ! -d "${LATEST_BACKUP}" ]]; then
    echo "[!] No backup found in ${BACKUP_DIR}" >&2
    exit 1
  fi
  echo "[*] Restoring from ${LATEST_BACKUP}..."
  if systemctl is-active --quiet portmaster 2>/dev/null; then
    systemctl stop portmaster || true
  fi
  if [[ -f "${LATEST_BACKUP}/portmaster-core" ]]; then
    cp -af "${LATEST_BACKUP}/portmaster-core" "${INSTALL_DIR}/portmaster-core"
  fi
  rm -f /etc/systemd/system/portmaster.service.d/50-rust-core.conf || true
  systemctl daemon-reload || true
  systemctl start portmaster || true
  echo "[+] Restore completed successfully."
  exit 0
fi

echo "=========================================================="
echo " Portmaster Upgrade & Patcher (Core: ${TARGET_CORE}, Release: ${TAG})"
echo "=========================================================="

# Check curl / wget
DOWNLOAD_CMD=""
if command -v curl >/dev/null 2>&1; then
  DOWNLOAD_CMD="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  DOWNLOAD_CMD="wget -qO-"
else
  echo "[!] Neither curl nor wget was found." >&2
  exit 1
fi

# Resolve download URL
echo "[*] Querying GitHub releases from ${REPO}..."
if [[ "${TAG}" == "latest" ]]; then
  RELEASE_API_URL="https://api.github.com/repos/${REPO}/releases/latest"
else
  RELEASE_API_URL="https://api.github.com/repos/${REPO}/releases/tags/${TAG}"
fi

RELEASE_JSON=$(eval "${DOWNLOAD_CMD} \"${RELEASE_API_URL}\"" || true)
if [[ -z "${RELEASE_JSON}" ]]; then
  # Fallback to direct release URL
  if [[ "${TAG}" == "latest" ]]; then
    BASE_URL="https://github.com/${REPO}/releases/latest/download"
  else
    BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
  fi
else
  BASE_URL="https://github.com/${REPO}/releases/download/$(echo "${RELEASE_JSON}" | grep -m1 '"tag_name":' | cut -d '"' -f4)"
fi

BIN_NAME="portmaster-core-${TARGET_CORE}"
DOWNLOAD_BIN_URL="${BASE_URL}/${BIN_NAME}"
DOWNLOAD_UI_URL="${BASE_URL}/portmaster.zip"

TMP_DIR=$(mktemp -d /tmp/pm-patch.XXXXXX)
trap 'rm -rf "${TMP_DIR}"' EXIT

echo "[*] Downloading ${BIN_NAME}..."
if ! curl -fSL "${DOWNLOAD_BIN_URL}" -o "${TMP_DIR}/${BIN_NAME}"; then
  echo "[!] Failed to download binary from: ${DOWNLOAD_BIN_URL}" >&2
  exit 1
fi
chmod +x "${TMP_DIR}/${BIN_NAME}"

echo "[*] Downloading modern UI bundle (portmaster.zip)..."
curl -fSL "${DOWNLOAD_UI_URL}" -o "${TMP_DIR}/portmaster.zip" || {
  echo "[*] Notice: portmaster.zip download failed or not found, continuing without UI patch."
}

# Stop Portmaster service
SERVICE_WAS_ACTIVE=0
if systemctl is-active --quiet portmaster 2>/dev/null; then
  echo "[*] Stopping portmaster service..."
  systemctl stop portmaster || true
  SERVICE_WAS_ACTIVE=1
fi

# Backup existing binaries
STAMP=$(date +%Y%m%d-%H%M%S)
CURRENT_BACKUP="${BACKUP_DIR}/${STAMP}"
mkdir -p "${CURRENT_BACKUP}"

for target_file in portmaster-core portmaster-core-rs portmaster-core-go; do
  if [[ -f "${INSTALL_DIR}/${target_file}" ]]; then
    cp -af "${INSTALL_DIR}/${target_file}" "${CURRENT_BACKUP}/"
  fi
done
echo "[*] Existing binaries backed up to: ${CURRENT_BACKUP}"

# Install new binary
TARGET_BIN_PATH="${INSTALL_DIR}/${BIN_NAME}"
cp -af "${TMP_DIR}/${BIN_NAME}" "${TARGET_BIN_PATH}"
chmod 755 "${TARGET_BIN_PATH}"

# Ensure default execution points to chosen core
if [[ "${TARGET_CORE}" == "rs" ]]; then
  cp -af "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/portmaster-core"
  # Also setup systemd drop-in override if service exists
  DROPIN_DIR="/etc/systemd/system/portmaster.service.d"
  if [[ -f "/usr/lib/systemd/system/portmaster.service" || -f "/lib/systemd/system/portmaster.service" || -f "/etc/systemd/system/portmaster.service" ]]; then
    mkdir -p "${DROPIN_DIR}"
    cat > "${DROPIN_DIR}/50-rust-core.conf" <<EOF
[Service]
ExecStart=
ExecStart=${TARGET_BIN_PATH}
EOF
    systemctl daemon-reload || true
  fi
else
  cp -af "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/portmaster-core"
  rm -f /etc/systemd/system/portmaster.service.d/50-rust-core.conf || true
  systemctl daemon-reload || true
fi

# Unpack UI bundle if present
if [[ -f "${TMP_DIR}/portmaster.zip" ]] && command -v unzip >/dev/null 2>&1; then
  echo "[*] Updating Web UI..."
  UI_DEST="${DATA_DIR}/ui/modules/portmaster"
  mkdir -p "${UI_DEST}"
  unzip -q -o "${TMP_DIR}/portmaster.zip" -d "${UI_DEST}" || true

  # Also update updates path if present
  UPDATES_UI="${INSTALL_DIR}/updates/linux_amd64/ui/modules/portmaster"
  if [[ -d "${UPDATES_UI}" ]]; then
    unzip -q -o "${TMP_DIR}/portmaster.zip" -d "${UPDATES_UI}" || true
  fi
fi

# Restart Portmaster
if [[ ${SERVICE_WAS_ACTIVE} -eq 1 ]] || systemctl is-enabled --quiet portmaster 2>/dev/null; then
  echo "[*] Starting portmaster service..."
  systemctl start portmaster || true
fi

echo ""
echo "=========================================================="
echo " [✓] Portmaster patched successfully!"
echo "     Active binary: ${TARGET_BIN_PATH}"
echo "     Backup saved:  ${CURRENT_BACKUP}"
echo "=========================================================="
