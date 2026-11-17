# Shared settings for the VM test suite. Source this file, do not run it.
#
# All VM images, disks, keys and credentials live in VMTEST_DIR, outside of
# the repository. Nothing here needs root on the host: the VMs run as the
# current user with QEMU, KVM and user-mode networking.

set -euo pipefail

VMTEST_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VMTEST_DIR="${VMTEST_DIR:-$HOME/vms/portmaster-test}"
VMTEST_SSH_KEY="$VMTEST_DIR/ssh/id_ed25519"

# Linux VM.
LINUX_DIR="$VMTEST_DIR/linux"
LINUX_SSH_PORT="${LINUX_SSH_PORT:-2222}"
LINUX_MEM="${LINUX_MEM:-4096}"
LINUX_CPUS="${LINUX_CPUS:-4}"
LINUX_USER="tester"

# Windows VM.
WINDOWS_DIR="$VMTEST_DIR/windows"
WINDOWS_SSH_PORT="${WINDOWS_SSH_PORT:-2223}"
WINDOWS_VNC_DISPLAY="${WINDOWS_VNC_DISPLAY:-1}" # 127.0.0.1:5901
WINDOWS_MEM="${WINDOWS_MEM:-6144}"
WINDOWS_CPUS="${WINDOWS_CPUS:-4}"
WINDOWS_USER="tester"

OVMF_DIR="${OVMF_DIR:-/usr/share/edk2/ovmf}"

log() { printf '\033[1m[vmtest]\033[0m %s\n' "$*" >&2; }
die() { log "error: $*"; exit 1; }

# ensure_ssh_key creates the SSH key used to log into all test VMs.
ensure_ssh_key() {
	if [[ ! -f "$VMTEST_SSH_KEY" ]]; then
		mkdir -p "$(dirname "$VMTEST_SSH_KEY")"
		ssh-keygen -q -t ed25519 -N "" -C "portmaster-vmtest" -f "$VMTEST_SSH_KEY"
	fi
}

VMTEST_BACKEND="${VMTEST_BACKEND:-local}"
case "$VMTEST_BACKEND" in
local) ;;
proxmox) source "$(dirname "${BASH_SOURCE[0]}")/proxmox.sh" ;;
*) die "unknown VMTEST_BACKEND $VMTEST_BACKEND (local or proxmox)" ;;
esac

# Test bundles and results; separate per backend so that local and Proxmox
# runs can happen at the same time.
VMTEST_WORK="$VMTEST_DIR"
[[ "$VMTEST_BACKEND" == local ]] || VMTEST_WORK="$VMTEST_DIR/$VMTEST_BACKEND"

# vm_endpoint PORT sets VM_HOST, VM_PORT and VM_SSH_OPTS for the VM named by
# PORT: 127.0.0.1 with QEMU's port forward locally, the VM's LAN address
# through the host on Proxmox.
vm_endpoint() {
	VM_SSH_OPTS=(-i "$VMTEST_SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
		-o LogLevel=ERROR -o ConnectTimeout=5 -o BatchMode=yes)
	if [[ "$VMTEST_BACKEND" == proxmox ]]; then
		VM_HOST="$(pve_ssh_target "$1")" || return 255
		VM_PORT=22
		VM_SSH_OPTS+=(-o "ProxyCommand=$(pve_proxy_command)")
	else
		VM_HOST=127.0.0.1
		VM_PORT="$1"
	fi
}

# vm_ssh PORT USER [COMMAND...] runs a command in a test VM. On Proxmox, a
# failed connection looks up the VM's address again, as DHCP may change it.
vm_ssh() {
	local port="$1" user="$2" rc
	shift 2
	vm_endpoint "$port" || return 255
	ssh "${VM_SSH_OPTS[@]}" -p "$VM_PORT" "$user@$VM_HOST" "$@"
	rc=$?
	if ((rc == 255)) && [[ "$VMTEST_BACKEND" == proxmox ]]; then
		PVE_IP_REFRESH=1 vm_endpoint "$port" || return 255
		ssh "${VM_SSH_OPTS[@]}" -p "$VM_PORT" "$user@$VM_HOST" "$@"
		rc=$?
	fi
	return $rc
}

# vm_rsh PORT prints an rsync -e command for the VM named by PORT, and
# vm_dest PORT USER PATH the matching rsync destination.
vm_rsh() {
	vm_endpoint "$1" || return 1
	# rsync splits -e on spaces and honours quotes, not backslashes.
	local a
	for a in ssh "${VM_SSH_OPTS[@]}" -p "$VM_PORT"; do printf "'%s' " "$a"; done
}
vm_dest() {
	vm_endpoint "$1" || return 1
	echo "$2@$VM_HOST:$3"
}

# vm_scp PORT USER SRC... DST copies files into a test VM (DST is remote).
vm_scp() {
	local port="$1" user="$2"
	shift 2
	local args=("$@")
	local dst="${args[-1]}"
	unset 'args[-1]'
	vm_endpoint "$port" || return 1
	scp -q "${VM_SSH_OPTS[@]}" -P "$VM_PORT" "${args[@]}" "$user@$VM_HOST:$dst"
}

# vm_fetch PORT USER REMOTE_SRC LOCAL_DST copies a file out of a test VM.
vm_fetch() {
	vm_endpoint "$1" || return 1
	scp -q "${VM_SSH_OPTS[@]}" -P "$VM_PORT" "$2@$VM_HOST:$3" "$4"
}

# wait_for_ssh PORT USER TIMEOUT_SECONDS waits until SSH login works.
wait_for_ssh() {
	local port="$1" user="$2" timeout="$3" waited=0
	until vm_ssh "$port" "$user" true 2>/dev/null; do
		((waited += 10))
		((waited <= timeout)) || return 1
		sleep 10
	done
}

# vm_running PIDFILE reports whether the VM of PIDFILE is running.
vm_running() {
	[[ -f "$1" ]] && kill -0 "$(cat "$1")" 2>/dev/null
}

# go_bin returns the go binary to use.
go_bin() {
	if command -v go >/dev/null; then
		command -v go
	elif [[ -x "$HOME/.local/go1.26.3/go/bin/go" ]]; then
		echo "$HOME/.local/go1.26.3/go/bin/go"
	else
		die "go not found"
	fi
}
