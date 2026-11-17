#!/usr/bin/env bash
# Runs all compiled tests of a bundle (see build-tests.sh) on Linux.
# Run it from the bundle root, as root to cover privileged code paths:
#
#   sudo ./run-tests.sh [EXTRA TEST FLAGS...]
#
# Prints one line per package and a summary; exits non-zero on failures.
# Full output of failed packages is in results/<package>.log.
set -uo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
results="$root/results"
rm -rf "$results" && mkdir -p "$results"

pass=0 fail=0
failed=()
while IFS= read -r bin; do
	dir="$(dirname "$bin")"
	rel="${dir#"$root"/}"
	logf="$results/${rel//\//_}.log"
	start=$(date +%s)
	(cd "$dir" && timeout 15m ./pkg.test -test.short -test.count=1 -test.timeout=10m "$@") >"$logf" 2>&1
	rc=$?
	dur=$(($(date +%s) - start))
	if ((rc == 0)); then
		((pass++))
		printf 'ok    %-60s %4ss\n' "$rel" "$dur"
		rm -f "$logf"
	else
		((fail++))
		failed+=("$rel")
		printf 'FAIL  %-60s %4ss (exit %d)\n' "$rel" "$dur" "$rc"
	fi
done < <(find "$root" -name pkg.test -type f | sort)

echo
echo "passed: $pass  failed: $fail"
for f in "${failed[@]}"; do echo "  failed: $f"; done
((fail == 0))
