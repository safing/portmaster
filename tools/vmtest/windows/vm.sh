#!/usr/bin/env bash
# Manage the Windows 11 test VM.
#
#   vm.sh create ISO   install Windows unattended from ISO (30-60 min)
#   vm.sh start        start the installed VM
#   vm.sh stop         shut the VM down
#   vm.sh status       show whether the VM runs and SSH works
#   vm.sh ssh [CMD]    open a shell or run a PowerShell command in the VM
#
# The VM uses UEFI with Secure Boot and a software TPM, as Windows 11
# requires. The screen is on VNC at 127.0.0.1:590$WINDOWS_VNC_DISPLAY.
source "$(dirname "${BASH_SOURCE[0]}")/../common.sh"

if [[ "$VMTEST_BACKEND" == proxmox ]]; then
	pve_vm_main "$WINDOWS_VMID" windows "$@"
	exit
fi

DIR="$WINDOWS_DIR"
DISK="$DIR/disk.qcow2"
VARS="$DIR/OVMF_VARS.fd"
PIDFILE="$DIR/qemu.pid"
MONITOR="$DIR/monitor.sock"
TPM_DIR="$DIR/tpm"

start_tpm() {
	mkdir -p "$TPM_DIR"
	if [[ -S "$TPM_DIR/swtpm-sock" ]] && vm_running "$TPM_DIR/swtpm.pid"; then
		return
	fi
	swtpm socket --tpm2 --daemon \
		--tpmstate "dir=$TPM_DIR" \
		--ctrl "type=unixio,path=$TPM_DIR/swtpm-sock" \
		--pid "file=$TPM_DIR/swtpm.pid" \
		--log "file=$TPM_DIR/swtpm.log,level=1"
}

# start_qemu [EXTRA_ARGS...] starts the VM in the background.
start_qemu() {
	vm_running "$PIDFILE" && die "VM already running"
	start_tpm
	qemu-system-x86_64 \
		-name portmaster-vmtest-windows \
		-machine q35,smm=on,accel=kvm \
		-cpu host -smp "$WINDOWS_CPUS" -m "$WINDOWS_MEM" \
		-global driver=cfi.pflash01,property=secure,value=on \
		-drive "if=pflash,format=raw,unit=0,readonly=on,file=$OVMF_DIR/OVMF_CODE.secboot.fd" \
		-drive "if=pflash,format=raw,unit=1,file=$VARS" \
		-chardev "socket,id=chrtpm,path=$TPM_DIR/swtpm-sock" \
		-tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-crb,tpmdev=tpm0 \
		-device ahci,id=ahci \
		-drive "file=$DISK,if=none,id=disk0,format=qcow2,discard=unmap" \
		-device ide-hd,drive=disk0,bus=ahci.0,bootindex=0 \
		-netdev "user,id=n0,hostfwd=tcp:127.0.0.1:$WINDOWS_SSH_PORT-:22" \
		-device e1000e,netdev=n0 \
		-device qemu-xhci -device usb-tablet \
		-vnc "127.0.0.1:$WINDOWS_VNC_DISPLAY" \
		-monitor "unix:$MONITOR,server,nowait" \
		-pidfile "$PIDFILE" -daemonize \
		"$@"
}

monitor_cmd() {
	printf '%s\n' "$1" | socat - "UNIX-CONNECT:$MONITOR" >/dev/null 2>&1 || true
}

cmd_create() {
	local iso="${1:-}"
	[[ -f "$iso" ]] || die "usage: vm.sh create WINDOWS_ISO"
	[[ -f "$DISK" ]] && die "$DISK exists; remove $DIR to reinstall"
	ensure_ssh_key
	mkdir -p "$DIR"

	# Credentials for the local test account, never stored in the repository.
	local password
	password="$(python3 -c 'import secrets; print(secrets.token_urlsafe(15).replace("-", "x").replace("_", "y"))')Aa1"
	umask 077
	printf 'user=%s\npassword=%s\n' "$WINDOWS_USER" "$password" >"$DIR/credentials"
	umask 022

	# Config CD with the answer file, the setup script and the public key.
	local cfg="$DIR/config"
	rm -rf "$cfg" && mkdir -p "$cfg"
	sed "s|__PASSWORD__|$password|g" "$(dirname "$0")/autounattend.xml.in" >"$cfg/autounattend.xml"
	cp "$(dirname "$0")/vmtest-setup.ps1" "$cfg/vmtest-setup.ps1"
	cp "$VMTEST_SSH_KEY.pub" "$cfg/vmtest_id_ed25519.pub"
	xorriso -as mkisofs -quiet -J -r -V VMTEST_CFG -o "$DIR/config.iso" "$cfg"
	rm -rf "$cfg"

	qemu-img create -q -f qcow2 "$DISK" 80G
	cp "$OVMF_DIR/OVMF_VARS.secboot.fd" "$VARS"

	log "installing Windows; watch on VNC 127.0.0.1:590$WINDOWS_VNC_DISPLAY"
	start_qemu \
		-drive "file=$iso,media=cdrom,if=none,id=cd0,readonly=on" \
		-device ide-cd,drive=cd0,bus=ahci.1,bootindex=1 \
		-drive "file=$DIR/config.iso,media=cdrom,if=none,id=cd1,readonly=on" \
		-device ide-cd,drive=cd1,bus=ahci.2

	# Answer the "Press any key to boot from CD or DVD" prompt.
	for _ in $(seq 1 40); do
		monitor_cmd "sendkey ret"
		sleep 0.5
	done

	log "waiting for setup and first logon (up to 90 min)"
	if ! wait_for_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 5400; then
		die "SSH did not come up; check VNC and C:\\vmtest-setup.log"
	fi
	log "Windows VM is ready"
}

cmd_start() {
	start_qemu
	log "waiting for SSH"
	wait_for_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 600 || die "SSH did not come up"
	log "Windows VM is ready"
}

cmd_stop() {
	vm_running "$PIDFILE" || { log "VM not running"; return; }
	vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "Stop-Computer -Force" 2>/dev/null || monitor_cmd system_powerdown
	for _ in $(seq 1 60); do
		vm_running "$PIDFILE" || { log "VM stopped"; return; }
		sleep 2
	done
	log "VM did not shut down, killing it"
	kill "$(cat "$PIDFILE")"
}

cmd_status() {
	if vm_running "$PIDFILE"; then
		echo "running (pid $(cat "$PIDFILE"))"
		vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" '$PSVersionTable.OS' 2>/dev/null || echo "ssh: not reachable"
	else
		echo "stopped"
	fi
}

case "${1:-}" in
create) shift; cmd_create "$@" ;;
start) cmd_start ;;
stop) cmd_stop ;;
status) cmd_status ;;
ssh) shift; vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "$@" ;;
*) sed -n '2,10p' "$0"; exit 2 ;;
esac
