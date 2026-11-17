#!/usr/bin/env bash
# Manage the Linux (Fedora Cloud) test VM, in which tests run as root.
#
#   vm.sh create IMAGE   create the VM from a Fedora Cloud qcow2 image
#   vm.sh start          start the VM
#   vm.sh stop           shut the VM down
#   vm.sh status         show whether the VM runs and SSH works
#   vm.sh ssh [CMD]      open a shell or run a command in the VM
#   vm.sh reset          discard all changes since create (snapshot "clean")
source "$(dirname "${BASH_SOURCE[0]}")/../common.sh"

DIR="$LINUX_DIR"
DISK="$DIR/disk.qcow2"
PIDFILE="$DIR/qemu.pid"
MONITOR="$DIR/monitor.sock"

start_qemu() {
	vm_running "$PIDFILE" && die "VM already running"
	qemu-system-x86_64 \
		-name portmaster-vmtest-linux \
		-machine q35,accel=kvm -cpu host \
		-smp "$LINUX_CPUS" -m "$LINUX_MEM" \
		-drive "file=$DISK,if=virtio,format=qcow2,discard=unmap" \
		-netdev "user,id=n0,hostfwd=tcp:127.0.0.1:$LINUX_SSH_PORT-:22" \
		-device virtio-net-pci,netdev=n0 \
		-display none -serial "file:$DIR/serial.log" \
		-monitor "unix:$MONITOR,server,nowait" \
		-pidfile "$PIDFILE" -daemonize \
		"$@"
}

monitor_cmd() {
	printf '%s\n' "$1" | socat - "UNIX-CONNECT:$MONITOR" >/dev/null 2>&1 || true
}

cmd_create() {
	local image="${1:-}"
	[[ -f "$image" ]] || die "usage: vm.sh create FEDORA_CLOUD_QCOW2"
	[[ -f "$DISK" ]] && die "$DISK exists; remove $DIR to recreate"
	ensure_ssh_key
	mkdir -p "$DIR"

	# cloud-init seed with the test user, the SSH key and the packages that
	# Portmaster needs at runtime.
	local seed="$DIR/seed"
	rm -rf "$seed" && mkdir -p "$seed"
	cat >"$seed/meta-data" <<-EOF
		instance-id: portmaster-vmtest
		local-hostname: pm-vmtest
	EOF
	cat >"$seed/user-data" <<-EOF
		#cloud-config
		users:
		  - name: $LINUX_USER
		    sudo: ALL=(ALL) NOPASSWD:ALL
		    shell: /bin/bash
		    ssh_authorized_keys:
		      - $(cat "$VMTEST_SSH_KEY.pub")
		growpart: {mode: auto, devices: ['/']}
		packages: [iptables-nft, nftables, conntrack-tools, bind-utils, rsync, jq]
		runcmd:
		  # The cloud kernel ships without most netfilter modules (nfqueue).
		  - dnf install -y "kernel-modules-\$(uname -r)" "kernel-modules-extra-\$(uname -r)" || dnf install -y kernel-modules kernel-modules-extra
		  - touch /var/lib/vmtest-ready
	EOF
	xorriso -as mkisofs -quiet -J -r -V cidata -o "$DIR/seed.iso" "$seed"
	rm -rf "$seed"

	qemu-img create -q -f qcow2 -F qcow2 -b "$(realpath "$image")" "$DISK" 30G

	log "booting and provisioning the VM"
	start_qemu -drive "file=$DIR/seed.iso,media=cdrom,readonly=on"
	wait_for_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 600 || die "SSH did not come up; see $DIR/serial.log"
	log "waiting for cloud-init"
	vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 'until [ -f /var/lib/vmtest-ready ]; do sleep 5; done; sudo cloud-init status --wait >/dev/null || true'

	# Reboot so that the kernel modules match, then keep a clean snapshot.
	cmd_stop
	start_qemu
	wait_for_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 300 || die "SSH did not come up after reboot"
	vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 'sudo modprobe nfnetlink_queue && sudo modprobe xt_NFQUEUE && uname -r'
	cmd_stop
	qemu-img snapshot -c clean "$DISK"
	log "Linux VM is ready (snapshot \"clean\" taken)"
}

cmd_start() {
	start_qemu
	wait_for_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 300 || die "SSH did not come up"
	log "Linux VM is ready"
}

cmd_stop() {
	vm_running "$PIDFILE" || { log "VM not running"; return; }
	vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 'sudo systemctl poweroff' 2>/dev/null || monitor_cmd system_powerdown
	for _ in $(seq 1 60); do
		vm_running "$PIDFILE" || { log "VM stopped"; return; }
		sleep 1
	done
	log "VM did not shut down, killing it"
	kill "$(cat "$PIDFILE")"
	sleep 1
}

cmd_reset() {
	vm_running "$PIDFILE" && cmd_stop
	qemu-img snapshot -a clean "$DISK"
	log "VM reset to snapshot \"clean\""
}

cmd_status() {
	if vm_running "$PIDFILE"; then
		echo "running (pid $(cat "$PIDFILE"))"
		vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER" 'uname -sr' 2>/dev/null || echo "ssh: not reachable"
	else
		echo "stopped"
	fi
}

case "${1:-}" in
create) shift; cmd_create "$@" ;;
start) cmd_start ;;
stop) cmd_stop ;;
reset) cmd_reset ;;
status) cmd_status ;;
ssh) shift; vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER" "$@" ;;
*) sed -n '2,10p' "$0"; exit 2 ;;
esac
