#!/usr/bin/env bash
# Soak test of portmaster-core on Linux. Runs inside the test VM as root (see
# test-linux.sh soak):
#
#   sudo bash soak.sh ./portmaster-core [MINUTES]
#
# Runs the service with interception under constant traffic and samples its
# resource usage every minute, to find leaks that short tests do not show.
# Prints one line per sample and a summary of the growth between the first
# samples after warm-up and the last ones. Exits non-zero if the service dies
# or a resource keeps growing well beyond the warm-up level.
set -uo pipefail

CORE="$(realpath "${1:?usage: soak.sh PORTMASTER_CORE [MINUTES]}")"
MINUTES="${2:-60}"
WORK=/tmp/pm-soak
DATA="$WORK/data"
LOG="$WORK/core.log"
CSV="$WORK/samples.csv"
API=http://127.0.0.1:817/api/v1

rm -rf "$WORK" && mkdir -p "$DATA"
# Allow incoming SSH, the test is controlled over SSH.
printf '{"filter":{"serviceEndpoints":["+ * TCP/22"]}}\n' >"$DATA/config.json"

"$CORE" --data-dir "$DATA" --log-stdout --log warning \
	--allowed-clients "$(realpath "$(command -v curl)")" >"$LOG" 2>&1 &
core_pid=$!
for _ in $(seq 1 150); do
	curl -sf --max-time 5 "$API/ready" >/dev/null 2>&1 && break
	kill -0 "$core_pid" 2>/dev/null || { echo "SOAK FAILED: portmaster-core exited during start"; tail -20 "$LOG"; exit 1; }
	sleep 2
done
echo "portmaster-core running (pid $core_pid), soaking for $MINUTES minutes"

# Traffic: HTTPS to a few sites and DNS lookups of always new names, so that
# connections, DNS caches and the connection history keep changing.
traffic() {
	local i=0
	local sites=(example.com example.org wikipedia.org debian.org kernel.org)
	while true; do
		i=$((i + 1))
		for s in "${sites[@]}"; do
			curl -s -o /dev/null --max-time 10 "https://$s/?soak=$i" &
		done
		for n in $(seq 1 10); do
			getent hosts "soak-$i-$n.example.com" >/dev/null 2>&1 &
		done
		wait
		sleep 1
	done
}
traffic &
traffic_pid=$!
trap 'kill "$traffic_pid" 2>/dev/null; kill "$core_pid" 2>/dev/null' EXIT

echo "minute,rss_kb,heap_kb,goroutines,fds,db_kb" >"$CSV"
sample() {
	local rss heap goroutines fds db
	rss=$(ps -o rss= -p "$core_pid" | tr -d ' ')
	# The stack endpoint returns a grouped goroutine profile with the total in
	# its first line: "goroutine profile: total N".
	goroutines=$(curl -sf --max-time 10 "$API/debug/stack" | sed -n 's/^goroutine profile: total \([0-9]*\).*/\1/p' | head -1)
	# Prometheus metrics are served at /metrics, with a namespace prefix and
	# possibly labels: portmaster_go_memstats_heap_inuse_bytes{...} VALUE
	heap=$(curl -sf --max-time 10 http://127.0.0.1:817/metrics | awk '$1 ~ /^([a-z]+_)?go_memstats_heap_inuse_bytes(\{.*\})?$/ {printf "%d", $NF/1024; exit}')
	fds=$(ls "/proc/$core_pid/fd" | wc -l)
	db=$(du -sk "$DATA" | cut -f1)
	echo "$1,$rss,${heap:-0},$goroutines,$fds,$db" >>"$CSV"
	printf 'minute %3d: rss %6d KB  heap %6d KB  goroutines %5d  fds %4d  data %6d KB\n' "$1" "$rss" "${heap:-0}" "$goroutines" "$fds" "$db"
}

for m in $(seq 0 "$MINUTES"); do
	if ! kill -0 "$core_pid" 2>/dev/null; then
		echo "SOAK FAILED: portmaster-core died after $m minutes"
		tail -30 "$LOG"
		exit 1
	fi
	sample "$m"
	[ "$m" -lt "$MINUTES" ] && sleep 60
done

# Compare the averages after warm-up with the averages at the end. The
# connection history keeps ended connections for 10 minutes, so memory only
# levels off after about 20 minutes under constant traffic. Open files vary
# with the connections open at each sample, so averages over 10 samples are
# compared.
python3 - "$CSV" <<'PY'
import csv, sys
rows = list(csv.DictReader(open(sys.argv[1])))
if len(rows) < 15:
    print("SOAK INCONCLUSIVE: too few samples"); sys.exit(0)
def avg(rs, k): return sum(int(r[k] or 0) for r in rs) / len(rs)
if len(rows) >= 45:
    early, late = rows[20:30], rows[-10:]
else:
    early, late = rows[5:10], rows[-5:]
failed = False
for k, limit in (("rss_kb", 1.5), ("heap_kb", 1.5), ("goroutines", 1.3), ("fds", 1.5)):
    a, b = avg(early, k), avg(late, k)
    ratio = b / a if a else 0
    flag = "GROWING" if ratio > limit else "ok"
    failed |= ratio > limit
    print(f"{k:11s} after warm-up {a:9.0f}  at the end {b:9.0f}  ratio {ratio:4.2f}  {flag}")
print("SOAK TEST FAILED" if failed else "SOAK TEST PASSED")
sys.exit(1 if failed else 0)
PY
