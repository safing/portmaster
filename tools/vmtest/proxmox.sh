# Proxmox backend of the VM test suite. Sourced by common.sh, do not run it.
#
# With VMTEST_BACKEND=proxmox the test VMs run on a Proxmox VE host instead
# of local QEMU. The host is reached with `ssh $PVE_HOST` (set up an alias in
# ~/.ssh/config with key login as root). The VMs sit on the host's LAN bridge
# and get their address by DHCP; the suite finds it from the VM's MAC address
# on the host and connects through the host (ProxyJump), so the VMs do not
# need to be routable from this machine.

PVE_HOST="${VMTEST_PVE_HOST:-proxmox}"
PVE_BRIDGE="${VMTEST_PVE_BRIDGE:-vmbr0}"
LINUX_VMID="${VMTEST_LINUX_VMID:-900}"
WINDOWS_VMID="${VMTEST_WINDOWS_VMID:-901}"

# One shared connection to the host for all calls of a run.
PVE_SSH_OPTS=(-o BatchMode=yes -o ControlMaster=auto
	-o "ControlPath=$HOME/.ssh/cm-vmtest-%C" -o ControlPersist=120)

# pve COMMAND... runs a shell command on the Proxmox host.
pve() {
	ssh "${PVE_SSH_OPTS[@]}" "$PVE_HOST" "$@"
}

# pve_vmid PORT maps the SSH port that names a VM in the scripts to its VMID.
pve_vmid() {
	case "$1" in
	"$LINUX_SSH_PORT") echo "$LINUX_VMID" ;;
	"$WINDOWS_SSH_PORT") echo "$WINDOWS_VMID" ;;
	*) die "no Proxmox VM for port $1" ;;
	esac
}

# pve_vm_ip VMID prints the VM's current IPv4 address. It refreshes the
# host's neighbour entries for the VM's MAC and, if none answers, pings the
# bridge's subnet once so that the VM shows up via ARP (this works even when
# the guest firewall drops ICMP).
pve_vm_ip() {
	local cache="$VMTEST_DIR/proxmox-$1.ip" ip
	ip="$(pve bash -s -- "$1" "$PVE_BRIDGE" <<-'EOF'
		vmid="$1" br="$2"
		mac="$(qm config "$vmid" | sed -n 's/^net0: [a-z0-9]*=\([0-9A-Fa-f:]*\),.*/\1/p' | tr A-F a-f)"
		[ -n "$mac" ] || exit 1
		lookup() { ip -4 neigh show dev "$br" | grep -i " $mac " | grep -E 'REACHABLE|DELAY|PERMANENT' | awk '{print $1; exit}'; }
		for a in $(ip -4 neigh show dev "$br" | grep -i " $mac " | awk '{print $1}'); do ping -c1 -W1 "$a" >/dev/null 2>&1; done
		found="$(lookup)"
		if [ -z "$found" ]; then
			net="$(ip -4 -o addr show dev "$br" | awk '{print $4; exit}')"
			prefix="${net%.*}"
			for i in $(seq 1 254); do ping -c1 -W1 "$prefix.$i" >/dev/null 2>&1 & done
			wait
			found="$(lookup)"
		fi
		[ -n "$found" ] && echo "$found"
	EOF
	)" || return 1
	[[ -n "$ip" ]] || return 1
	echo "$ip" >"$cache"
	echo "$ip"
}

# pve_ssh_target PORT prints the IP of the VM named by PORT, cached between
# calls; set PVE_IP_REFRESH=1 to look it up again.
pve_ssh_target() {
	local vmid cache ip
	vmid="$(pve_vmid "$1")"
	cache="$VMTEST_DIR/proxmox-$vmid.ip"
	if [[ -s "$cache" ]] && [[ "${PVE_IP_REFRESH:-0}" == 0 ]]; then
		ip="$(cat "$cache")"
	else
		ip="$(pve_vm_ip "$vmid")" || return 1
	fi
	echo "$ip"
}

# pve_proxy_command is the ProxyCommand that tunnels VM SSH through the host.
pve_proxy_command() {
	# Escape the host options' % for the outer ssh, which expands the command.
	local opts="${PVE_SSH_OPTS[*]}"
	printf 'ssh %s %s -W %%h:%%p' "${opts//%/%%}" "$PVE_HOST"
}

# pve_vm_main VMID KIND COMMAND... implements vm.sh for the Proxmox backend.
pve_vm_main() {
	local vmid="$1" kind="$2" port user
	shift 2
	if [[ "$kind" == linux ]]; then port="$LINUX_SSH_PORT" user="$LINUX_USER"; else port="$WINDOWS_SSH_PORT" user="$WINDOWS_USER"; fi
	case "${1:-}" in
	create)
		die "on Proxmox, import the VM instead of creating it; see tools/vmtest/README.md"
		;;
	start)
		pve qm status "$vmid" | grep -q running && die "VM $vmid already running"
		pve qm start "$vmid"
		rm -f "$VMTEST_DIR/proxmox-$vmid.ip"
		wait_for_ssh "$port" "$user" 600 || die "SSH did not come up"
		log "$kind VM $vmid is ready ($(cat "$VMTEST_DIR/proxmox-$vmid.ip"))"
		;;
	stop)
		pve qm status "$vmid" | grep -q running || { log "VM not running"; return; }
		if [[ "$kind" == linux ]]; then
			vm_ssh "$port" "$user" 'sudo systemctl poweroff' 2>/dev/null || true
		else
			vm_ssh "$port" "$user" 'Stop-Computer -Force' 2>/dev/null || true
		fi
		# qm shutdown sends ACPI if the guest did not go down by itself, and
		# stops it hard after the timeout.
		pve qm shutdown "$vmid" --timeout 120 --forceStop 1 >/dev/null 2>&1 || true
		log "VM stopped"
		;;
	reset)
		pve qm listsnapshot "$vmid" | grep -q ' clean ' || die "VM $vmid has no snapshot \"clean\""
		pve qm status "$vmid" | grep -q running && "$0" stop
		pve qm rollback "$vmid" clean
		log "VM reset to snapshot \"clean\""
		;;
	status)
		local st
		st="$(pve qm status "$vmid" | awk '{print $2}')"
		echo "$st (Proxmox VM $vmid on $PVE_HOST)"
		if [[ "$st" == running ]]; then
			if [[ "$kind" == linux ]]; then
				vm_ssh "$port" "$user" 'uname -sr' 2>/dev/null || echo "ssh: not reachable"
			else
				vm_ssh "$port" "$user" '$PSVersionTable.OS' 2>/dev/null || echo "ssh: not reachable"
			fi
		fi
		;;
	ssh)
		shift
		vm_ssh "$port" "$user" "$@"
		;;
	*)
		sed -n '2,10p' "$0"
		echo "Backend: Proxmox VM $vmid on $PVE_HOST (VMTEST_BACKEND=proxmox)"
		exit 2
		;;
	esac
}

# pve_serial_capture VMID FILE copies the VM's serial console to FILE in the
# background (as local QEMU does with -serial file:). Prints the PID.
pve_serial_capture() {
	ssh -n "${PVE_SSH_OPTS[@]}" "$PVE_HOST" \
		"socat -u UNIX-CONNECT:/var/run/qemu-server/$1.serial0 STDOUT" >>"$2" 2>/dev/null </dev/null &
	echo $!
}
