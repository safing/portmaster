#!/usr/bin/env bash
# Integration smoke test of portmaster-core on Linux. Runs inside the test VM
# as root (see test-linux.sh):
#
#   sudo bash smoke.sh ./portmaster-core
#
# Starts the real service with packet interception and checks that it
# intercepts and filters traffic, answers its API, shuts down cleanly and that
# a crashed instance can be recovered. Exits non-zero if any check fails.
set -uo pipefail

CORE="$(realpath "${1:?usage: smoke.sh PORTMASTER_CORE}")"
WORK=/tmp/pm-smoke
DATA="$WORK/data"
LOG="$WORK/core.log"
API=http://127.0.0.1:817/api/v1
ALLOWED=example.com
BLOCKED=example.org
# A name whose CNAME points outside the blocked zone (safing/portmaster#2306).
CNAME_BLOCKED=tr.archive.ubuntu.com
CNAME_ZONE=ubuntu.com
# A tracker on the default filter lists.
TRACKER=doubleclick.net

failures=0
pass() { printf '  \033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$*"; ((failures++)); }
check() { # check DESCRIPTION COMMAND...
	local desc="$1"
	shift
	if "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; fi
}
step() { printf '\n== %s\n' "$*"; }

rules_present() { iptables-save 2>/dev/null | grep -q PORTMASTER || ip6tables-save 2>/dev/null | grep -q PORTMASTER; }
https_ok() { curl -sS -o /dev/null --max-time 15 "https://$1/"; }
http_ok() { curl -sS -o /dev/null --max-time 15 "http://$1/"; }
resolves() { getent ahostsv4 "$1" | grep -qv '^0\.0\.0\.0'; }
api_ready() { curl -sf --max-time 5 "$API/ready"; }
core_pid=""

start_core() {
	"$CORE" --data-dir "$DATA" --log-stdout --log info \
		--allowed-clients "$(realpath "$(command -v curl)")" >>"$LOG" 2>&1 &
	core_pid=$!
	local waited=0
	until api_ready >/dev/null 2>&1; do
		sleep 2
		((waited += 2))
		((waited % 30 == 0)) && echo "  waiting for portmaster-core (${waited}s): $(tail -1 "$LOG" | cut -c1-120)"
		if ! kill -0 "$core_pid" 2>/dev/null; then
			echo "portmaster-core exited during start; last log lines:"
			tail -30 "$LOG"
			return 1
		fi
		if ((waited >= 300)); then
			echo "portmaster-core not ready after ${waited}s; last log lines:"
			tail -30 "$LOG"
			return 1
		fi
	done
	echo "  portmaster-core ready after ${waited}s (pid $core_pid)"
}

netquery_count() { # netquery_count DOMAIN
	curl -sf --max-time 10 -X POST "$API/netquery/query" -H 'Content-Type: application/json' \
		-d "{\"query\":{\"domain\":{\"\$like\":\"%$1%\"}},\"select\":[\"id\",\"verdict\"],\"pageSize\":50}" |
		jq '.results | length'
}

rm -rf "$WORK" && mkdir -p "$DATA"

step "Before start"
check "no Portmaster rules installed" bash -c '! { iptables-save | grep -q PORTMASTER; }'
check "https://$ALLOWED reachable" https_ok "$ALLOWED"
check "http://$CNAME_BLOCKED reachable" http_ok "$CNAME_BLOCKED"

# Block one domain and allow incoming SSH (the test is controlled over SSH)
# in the global settings.
write_config() {
	printf '{"filter":{"endpoints":[%s],"serviceEndpoints":["+ * TCP/22"]}}\n' "$1" >"$DATA/config.json"
}
write_config "\"- $BLOCKED\",\"- .$CNAME_ZONE\""

step "Start"
if ! start_core; then
	fail "portmaster-core starts and reports ready"
	exit 1
fi
pass "portmaster-core starts and reports ready"
check "interception rules installed" rules_present
check "nfqueue bound" bash -c 'grep -q . /proc/net/netfilter/nfnetlink_queue'
check "systemd-resolved cache flushed" grep -q 'flushed the systemd-resolved cache' "$LOG"

step "Filtering"
check "$ALLOWED resolves" resolves "$ALLOWED"
check "https://$ALLOWED reachable" https_ok "$ALLOWED"
# Fedora resolves names through systemd-resolved, so the per-app rule applies
# to the connection, not to the DNS query.
echo "  info: $BLOCKED resolves through systemd-resolved: $(resolves "$BLOCKED" && echo yes || echo no)"
check "https://$BLOCKED is blocked" bash -c "! curl -sS -o /dev/null --max-time 10 https://$BLOCKED/"
check "http://$CNAME_BLOCKED is blocked by the rule for .$CNAME_ZONE (CNAME outside the zone)" bash -c "! curl -sS -o /dev/null --max-time 10 http://$CNAME_BLOCKED/"
check "Portmaster nameserver answers" bash -c "dig +short +time=5 +tries=1 $ALLOWED @127.0.0.1 | grep -q ."

step "Filter lists"
# Downloading, decoding and storing the lists covers the intel updater, the
# list decoder and the SQLite cache database.
waited=0
until grep -q 'rebuilt bloom filters' "$LOG" || ((waited >= 240)); do
	sleep 5
	((waited += 5))
done
check "filter lists loaded (${waited}s)" grep -q 'rebuilt bloom filters' "$LOG"
check "https://$TRACKER is blocked by the filter lists" bash -c "! curl -sS -o /dev/null --max-time 10 https://$TRACKER/"
# The block is logged by the nameserver or the filter, whichever sees it
# first, and the log is written asynchronously.
check "the block names the filter lists" bash -c "for i in 1 2 3 4 5; do grep -q '$TRACKER\\. in activated lists' '$LOG' && exit 0; sleep 1; done; exit 1"

step "API"
check "ping endpoint" curl -sf --max-time 5 "$API/ping"
sleep 3 # let netquery save the connections
n=$(netquery_count "$ALLOWED" 2>/dev/null || echo 0)
if [[ "$n" =~ ^[0-9]+$ ]] && ((n > 0)); then pass "netquery lists connections to $ALLOWED ($n)"; else fail "netquery lists connections to $ALLOWED (got '$n')"; fi
code=$(python3 -c "
import urllib.request, urllib.error
req = urllib.request.Request('$API/netquery/query', data=b'{}', method='POST')
try:
    print(urllib.request.urlopen(req, timeout=10).status)
except urllib.error.HTTPError as e:
    print(e.code)
" 2>/dev/null)
if [[ "$code" == 403 || "$code" == 401 ]]; then pass "API denies an unauthorized local client ($code)"; else fail "API denies an unauthorized local client (got '$code')"; fi
# app/auth identifies the calling process and waits for the user to answer
# the prompt; without the process (safing/portmaster#2302 B1) it fails at once.
auth_code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$API/app/auth?app-name=vmtest&read=user&write=user&ttl=1h")
if [[ "$auth_code" == 000 ]]; then pass "app/auth identifies the caller and waits for the prompt"; else fail "app/auth identifies the caller and waits for the prompt (got HTTP $auth_code)"; fi

step "Load"
load_pids=()
for i in $(seq 1 40); do
	curl -s -o /dev/null --max-time 10 "https://$ALLOWED/?n=$i" &
	load_pids+=($!)
done
wait "${load_pids[@]}" # not "wait" alone, which would wait for portmaster-core
check "service alive after 40 parallel connections" kill -0 "$core_pid"
check "https://$ALLOWED still reachable" https_ok "$ALLOWED"

step "Bandwidth"
# Several requests over one kept-alive connection, so that the connection
# is open long enough for the eBPF bandwidth monitor to sample it.
curl -s -o /dev/null --max-time 20 --rate 2/s "https://$ALLOWED/?bw=[1-8]" || true
sleep 3 # let the bandwidth updates reach netquery
bw=$(curl -sf --max-time 10 -X POST "$API/netquery/query" -H 'Content-Type: application/json' \
	-d "{\"query\":{\"domain\":{\"\$like\":\"%$ALLOWED%\"}},\"select\":[\"bytes_received\",\"bytes_sent\"],\"pageSize\":100}" |
	jq '[.results[] | (.bytes_received // 0) + (.bytes_sent // 0)] | add // 0' 2>/dev/null || echo 0)
if [[ "$bw" =~ ^[0-9]+$ ]] && ((bw > 0)); then pass "eBPF bandwidth data recorded ($bw bytes)"; else fail "eBPF bandwidth data recorded (got '$bw')"; fi

step "Shutdown"
kill -TERM "$core_pid"
stopped=0
for _ in $(seq 1 60); do
	if ! kill -0 "$core_pid" 2>/dev/null; then stopped=1; break; fi
	sleep 1
done
if ((stopped)); then pass "portmaster-core stops within 60s"; else fail "portmaster-core stops within 60s"; kill -9 "$core_pid"; fi
wait "$core_pid" 2>/dev/null
rc=$?
if ((rc == 0)); then pass "exit code 0"; else fail "exit code $rc"; fi
check "interception rules removed" bash -c '! { iptables-save | grep -q PORTMASTER; }'
check "https://$ALLOWED reachable without Portmaster" https_ok "$ALLOWED"

step "Crash recovery"
write_config ""
if start_core; then
	kill -9 "$core_pid"
	wait "$core_pid" 2>/dev/null
	if rules_present; then
		echo "  info: rules remain after a crash; reachable meanwhile: $(https_ok "$ALLOWED" && echo yes || echo no)"
	else
		echo "  info: no rules left after a crash"
	fi
	"$CORE" recover-iptables >>"$LOG" 2>&1
	check "recover-iptables removes the rules" bash -c '! { iptables-save | grep -q PORTMASTER; }'
	check "https://$ALLOWED reachable after recovery" https_ok "$ALLOWED"
else
	fail "portmaster-core restarts with existing data"
fi

step "Log"
panics=$(grep -c -E '^panic:|fatal error:|DATA RACE' "$LOG" || true)
if ((panics == 0)); then pass "no panics in the log"; else fail "$panics panics in the log"; fi
errors=$(grep -cE ' (ERR|CRT) ' "$LOG" || true)
echo "  log: $(wc -l <"$LOG") lines"
if ((errors == 0)); then
	pass "no errors in the log"
else
	fail "$errors errors in the log"
	grep -E ' (ERR|CRT) ' "$LOG" | head -10 | cut -c1-200
fi

if ((failures > 0)); then
	echo "  last log lines:"
	tail -40 "$LOG" | sed 's/^/    /'
fi
echo
if ((failures == 0)); then echo "SMOKE TEST PASSED"; else echo "SMOKE TEST FAILED: $failures checks"; fi
((failures == 0))
