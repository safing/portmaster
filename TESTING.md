# Testing Portmaster

This page documents ways to test if Portmaster works as intended.

⚠ Work in Progress. Currently we are just collecting helpful things we find.

## Websites for Testing:

- <http://icmpcheck.popcount.org/>: Check 
  - ICMP path MTU packet delivery
  - IP fragmented packet delivery

## Rust core: privileged interception smoke test

The Rust core can be smoke-tested with iptables/nfqueue in an isolated network
namespace, so the host firewall is not modified. Build first, then run as root:

```bash
cargo build
sudo unshare --net -- bash -c '
  set -e
  ip link set lo up
  TMP=$(mktemp -d /tmp/pm-netns-test.XXXXXX)
  ./target/debug/portmaster-core-rs \
    --data-dir "$TMP/data" --bin-dir "$TMP/bin" --log-dir "$TMP/logs" \
    --api-address 127.0.0.1:8170 --nameserver-address 127.0.0.1:5300 \
    --write-packet-metrics "$TMP/metrics.txt" \
    --log-stdout --log debug --devmode >"$TMP/core.log" 2>&1 &
  CORE=$!
  sleep 3
  grep -iE "nfqueue|filter:" "$TMP/core.log" || true
  for i in 1 2 3 4 5; do curl -sS -o /dev/null http://127.0.0.1:8170/api/v1/ping || true; done
  ping -c 2 -W 1 127.0.0.1 || true
  sleep 6
  iptables -t mangle -L -v -n | grep -E "NFQUEUE|PORTMASTER" || true
  cat "$TMP/metrics.txt" 2>/dev/null || true
  cat "$TMP/core.log"
  kill -INT $CORE
  wait $CORE || true
  iptables -t mangle -S | grep PORTMASTER || true
'
```

Expected result: `filter: nfqueue interception started queues=[...]`, non-zero
NFQUEUE packet counters, lines in `metrics.txt` with verdict names such as
`PermanentAccept`, and no `PORTMASTER-*` chains after shutdown.
