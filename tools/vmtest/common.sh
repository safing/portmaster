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

# vm_ssh PORT USER [COMMAND...] runs a command in a test VM.
vm_ssh() {
	local port="$1" user="$2"
	shift 2
	ssh -i "$VMTEST_SSH_KEY" -p "$port" \
		-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
		-o LogLevel=ERROR -o ConnectTimeout=5 -o BatchMode=yes \
		"$user@127.0.0.1" "$@"
}

# vm_scp PORT USER SRC... DST copies files into a test VM (DST is remote).
vm_scp() {
	local port="$1" user="$2"
	shift 2
	local args=("$@")
	local dst="${args[-1]}"
	unset 'args[-1]'
	scp -q -i "$VMTEST_SSH_KEY" -P "$port" \
		-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
		-o LogLevel=ERROR -o BatchMode=yes \
		"${args[@]}" "$user@127.0.0.1:$dst"
}

# vm_fetch PORT USER REMOTE_SRC LOCAL_DST copies a file out of a test VM.
vm_fetch() {
	scp -q -i "$VMTEST_SSH_KEY" -P "$1" \
		-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
		-o LogLevel=ERROR -o BatchMode=yes \
		"$2@127.0.0.1:$3" "$4"
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
