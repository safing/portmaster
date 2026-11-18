#!/usr/bin/env bash
# Run the test suite in the Windows VM as administrator:
#
#   test-windows.sh [unit|smoke|all]   (default: unit)
#   test-windows.sh soak [MINUTES]     (not part of all; default 60 minutes)
#
# Results are written to $VMTEST_WORK/results/windows-*.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

what="${1:-unit}"
bundle="$VMTEST_WORK/bundle-windows"
results="$VMTEST_WORK/results/windows-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$results"
SSH=(vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER")
VM="$VMTEST_REPO/tools/vmtest/windows/vm.sh"

started_vm=0
if ! "${SSH[@]}" 'exit 0' 2>/dev/null; then
	"$VM" start
	started_vm=1
fi
trap '((started_vm)) && "$VM" stop' EXIT

rc=0
if [[ "$what" == unit || "$what" == all ]]; then
	"$VMTEST_REPO/tools/vmtest/build-tests.sh" windows "$bundle"
	log "copying test bundle"
	tar -C "$bundle" -czf "$VMTEST_WORK/bundle-windows.tgz" .
	"${SSH[@]}" 'Remove-Item -Recurse -Force C:\vmtest\bundle -ErrorAction SilentlyContinue; New-Item -ItemType Directory -Force C:\vmtest\bundle | Out-Null'
	vm_scp "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "$VMTEST_WORK/bundle-windows.tgz" 'C:/vmtest/bundle.tgz'
	"${SSH[@]}" 'tar -xzf C:\vmtest\bundle.tgz -C C:\vmtest\bundle; Remove-Item C:\vmtest\bundle.tgz'
	log "running unit tests as administrator"
	"${SSH[@]}" 'powershell -NoProfile -ExecutionPolicy Bypass -File C:\vmtest\bundle\run-tests.ps1; exit $LASTEXITCODE' | tee "$results/unit.txt" || rc=1
	"${SSH[@]}" 'tar -cf C:\vmtest\results.tar -C C:\vmtest\bundle results' >/dev/null 2>&1 &&
		vm_fetch "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'C:/vmtest/results.tar' "$results/results.tar" &&
		tar -C "$results" -xf "$results/results.tar" && rm -f "$results/results.tar"
fi

if [[ "$what" == smoke || "$what" == all ]]; then
	log "building portmaster-core.exe"
	(cd "$VMTEST_REPO" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$(go_bin)" build -o "$results/portmaster-core.exe" ./cmds/portmaster-core)
	vm_scp "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "$results/portmaster-core.exe" "$VMTEST_REPO/tools/vmtest/windows/smoke.ps1" "$VMTEST_REPO/tools/vmtest/windows/smoke-task.ps1" 'C:/vmtest/'
	# WINDOWS_CORE_DLL: test this core DLL instead of the downloaded one.
	vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'Remove-Item C:\vmtest\portmaster-core.dll -ErrorAction SilentlyContinue' || true
	if [[ -n "${WINDOWS_CORE_DLL:-}" ]]; then
		log "using core DLL $WINDOWS_CORE_DLL"
		vm_scp "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "$WINDOWS_CORE_DLL" 'C:/vmtest/portmaster-core.dll'
	fi
	log "running smoke test as administrator"
	# Run as a scheduled task, detached from the SSH session: while Portmaster
	# starts intercepting, the session may be dropped. Then wait for the
	# result with new sessions.
	"${SSH[@]}" '
		Remove-Item C:\vmtest\smoke.txt, C:\vmtest\smoke.done -ErrorAction SilentlyContinue
		$action = New-ScheduledTaskAction -Execute powershell.exe -Argument "-NoProfile -ExecutionPolicy Bypass -File C:\vmtest\smoke-task.ps1"
		Register-ScheduledTask -TaskName pm-smoke -Action $action -User SYSTEM -RunLevel Highest -Force | Out-Null
		Start-ScheduledTask -TaskName pm-smoke'
	waited=0
	until vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'if (-not (Test-Path C:\vmtest\smoke.done)) { exit 1 }' 2>/dev/null; do
		sleep 15
		((waited += 15))
		((waited < 1800)) || { log "smoke test did not finish within 30 min"; rc=1; break; }
	done
	"${SSH[@]}" 'Get-Content C:\vmtest\smoke.txt' | tee "$results/smoke.txt"
	[[ "$("${SSH[@]}" '(Get-Content C:\vmtest\smoke.done).Trim()' 2>/dev/null | tr -d '\r')" == 0 ]] || rc=1
	"${SSH[@]}" 'Unregister-ScheduledTask -TaskName pm-smoke -Confirm:$false' >/dev/null 2>&1 || true
	vm_fetch "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'C:/vmtest/pm-smoke/core.log' "$results/core.log" 2>/dev/null || true
fi

if [[ "$what" == soak ]]; then
	minutes="${2:-60}"
	log "building portmaster-core.exe"
	(cd "$VMTEST_REPO" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$(go_bin)" build -o "$results/portmaster-core.exe" ./cmds/portmaster-core)
	vm_scp "$WINDOWS_SSH_PORT" "$WINDOWS_USER" "$results/portmaster-core.exe" "$VMTEST_REPO/tools/vmtest/windows/soak.ps1" 'C:/vmtest/'
	log "running soak test for $minutes minutes as administrator"
	# Like the smoke test: run as a scheduled task, detached from SSH.
	"${SSH[@]}" "
		Remove-Item C:\\vmtest\\soak.txt, C:\\vmtest\\soak.done -ErrorAction SilentlyContinue
		Set-Content -Path C:\\vmtest\\soak-task.ps1 -Value '& C:\\vmtest\\soak.ps1 -Core C:\\vmtest\\portmaster-core.exe -Minutes $minutes *> C:\\vmtest\\soak.txt; Set-Content -Path C:\\vmtest\\soak.done -Value \$LASTEXITCODE'
		\$action = New-ScheduledTaskAction -Execute powershell.exe -Argument '-NoProfile -ExecutionPolicy Bypass -File C:\\vmtest\\soak-task.ps1'
		Register-ScheduledTask -TaskName pm-soak -Action \$action -User SYSTEM -RunLevel Highest -Force | Out-Null
		Start-ScheduledTask -TaskName pm-soak"
	waited=0
	limit=$(((minutes + 20) * 60))
	until vm_ssh "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'if (-not (Test-Path C:\vmtest\soak.done)) { exit 1 }' 2>/dev/null; do
		sleep 30
		((waited += 30))
		((waited < limit)) || { log "soak test did not finish in time"; rc=1; break; }
	done
	"${SSH[@]}" 'Get-Content C:\vmtest\soak.txt' | tee "$results/soak.txt"
	[[ "$("${SSH[@]}" '(Get-Content C:\vmtest\soak.done).Trim()' 2>/dev/null | tr -d '\r')" == 0 ]] || rc=1
	"${SSH[@]}" 'Unregister-ScheduledTask -TaskName pm-soak -Confirm:$false' >/dev/null 2>&1 || true
	vm_fetch "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'C:/vmtest/pm-soak/samples.csv' "$results/samples.csv" 2>/dev/null || true
	vm_fetch "$WINDOWS_SSH_PORT" "$WINDOWS_USER" 'C:/vmtest/pm-soak/core.log' "$results/core.log" 2>/dev/null || true
fi

log "results in $results"
exit $rc
