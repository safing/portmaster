#!/usr/bin/env bash
# Run the test suite in the Linux VM as root:
#
#   test-linux.sh [unit|smoke|all]     (default: all)
#   test-linux.sh soak [MINUTES]       (not part of all; default 60 minutes)
#
# The VM is reset to its clean snapshot first, so every run starts from the
# same state. Results are written to $VMTEST_WORK/results/linux-*.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

what="${1:-all}"
bundle="$VMTEST_WORK/bundle-linux"
results="$VMTEST_WORK/results/linux-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$results"
SSH=(vm_ssh "$LINUX_SSH_PORT" "$LINUX_USER")

"$VMTEST_REPO/tools/vmtest/linux/vm.sh" reset
"$VMTEST_REPO/tools/vmtest/linux/vm.sh" start
trap '"$VMTEST_REPO/tools/vmtest/linux/vm.sh" stop' EXIT

rc=0
if [[ "$what" == unit || "$what" == all ]]; then
	"$VMTEST_REPO/tools/vmtest/build-tests.sh" linux "$bundle"
	log "copying test bundle"
	rsync -a --delete -e "$(vm_rsh "$LINUX_SSH_PORT")" \
		"$bundle/" "$(vm_dest "$LINUX_SSH_PORT" "$LINUX_USER" bundle/)"
	log "running unit tests as root"
	"${SSH[@]}" 'cd bundle && sudo ./run-tests.sh' | tee "$results/unit.txt" || rc=1
	"${SSH[@]}" 'sudo tar -C bundle -cf - results' | tar -C "$results" -xf - 2>/dev/null || true
fi

if [[ "$what" == smoke || "$what" == all ]]; then
	log "building portmaster-core"
	(cd "$VMTEST_REPO" && GOOS=linux CGO_ENABLED=0 "$(go_bin)" build -o "$results/portmaster-core" ./cmds/portmaster-core)
	vm_scp "$LINUX_SSH_PORT" "$LINUX_USER" "$results/portmaster-core" "$VMTEST_REPO/tools/vmtest/linux/smoke.sh" "/home/$LINUX_USER/"
	# Run detached from the SSH session, as Portmaster may hold SSH while it
	# intercepts. The test reports on the serial console, which QEMU writes
	# to serial.log on the host, so the result arrives even without SSH.
	serial="$LINUX_DIR/serial.log"
	capture_pid=""
	if [[ "$VMTEST_BACKEND" == proxmox ]]; then
		serial="$results/serial.log"
		: >"$serial"
		capture_pid="$(pve_serial_capture "$LINUX_VMID" "$serial")"
		sleep 2
	fi
	log "running smoke test as root (live output: $serial)"
	offset=$(stat -c %s "$serial")
	"${SSH[@]}" 'sudo systemd-run --unit=pm-smoke --collect bash -c "bash /home/'"$LINUX_USER"'/smoke.sh /home/'"$LINUX_USER"'/portmaster-core 2>&1 | tee /dev/ttyS0"'
	waited=0
	until tail -c +"$((offset + 1))" "$serial" | grep -qa 'SMOKE TEST \(PASSED\|FAILED\)'; do
		sleep 10
		((waited += 10))
		((waited < 1800)) || { log "smoke test did not finish within 30 min"; rc=1; break; }
	done
	[[ -n "$capture_pid" ]] && kill "$capture_pid" 2>/dev/null
	tail -c +"$((offset + 1))" "$serial" | tr -d '\r' | sed -n '/^== Before start/,/^SMOKE TEST/p' | tee "$results/smoke.txt"
	grep -q 'SMOKE TEST PASSED' "$results/smoke.txt" || rc=1
	"${SSH[@]}" 'sudo cat /tmp/pm-smoke/core.log' >"$results/core.log" 2>/dev/null || true
fi

if [[ "$what" == soak ]]; then
	minutes="${2:-60}"
	log "building portmaster-core"
	(cd "$VMTEST_REPO" && GOOS=linux CGO_ENABLED=0 "$(go_bin)" build -o "$results/portmaster-core" ./cmds/portmaster-core)
	vm_scp "$LINUX_SSH_PORT" "$LINUX_USER" "$results/portmaster-core" "$VMTEST_REPO/tools/vmtest/linux/soak.sh" "/home/$LINUX_USER/"
	# Like the smoke test: run detached and report on the serial console.
	serial="$LINUX_DIR/serial.log"
	capture_pid=""
	if [[ "$VMTEST_BACKEND" == proxmox ]]; then
		serial="$results/serial.log"
		: >"$serial"
		capture_pid="$(pve_serial_capture "$LINUX_VMID" "$serial")"
		sleep 2
	fi
	log "running soak test for $minutes minutes as root (live output: $serial)"
	offset=$(stat -c %s "$serial")
	"${SSH[@]}" 'sudo systemd-run --unit=pm-soak --collect bash -c "bash /home/'"$LINUX_USER"'/soak.sh /home/'"$LINUX_USER"'/portmaster-core '"$minutes"' 2>&1 | tee /dev/ttyS0"'
	waited=0
	limit=$(((minutes + 15) * 60))
	until tail -c +"$((offset + 1))" "$serial" | grep -qa 'SOAK \(TEST PASSED\|TEST FAILED\|FAILED\|INCONCLUSIVE\)'; do
		sleep 30
		((waited += 30))
		((waited < limit)) || { log "soak test did not finish in time"; rc=1; break; }
	done
	[[ -n "$capture_pid" ]] && kill "$capture_pid" 2>/dev/null
	tail -c +"$((offset + 1))" "$serial" | tr -d '\r' | sed -n '/^portmaster-core running/,/^SOAK/p' | tee "$results/soak.txt"
	grep -q 'SOAK TEST PASSED' "$results/soak.txt" || rc=1
	"${SSH[@]}" 'sudo cat /tmp/pm-soak/samples.csv' >"$results/samples.csv" 2>/dev/null || true
	"${SSH[@]}" 'sudo cat /tmp/pm-soak/core.log' >"$results/core.log" 2>/dev/null || true
fi

log "results in $results"
exit $rc
