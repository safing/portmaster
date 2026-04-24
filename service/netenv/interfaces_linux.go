//go:build linux

package netenv

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// selectPhysicalDefaultInterfaces finds the best physical adapter per IP family
// that carries the default route, excluding all virtual and tunnel interfaces.
//
// Physical detection: the kernel creates /sys/class/net/<name>/device only for
// adapters bound to a real hardware driver. Virtual interfaces (tun, tap,
// bridge, veth, wireguard) never have this entry — this is the most reliable
// VPN-exclusion signal available without elevated privileges.
//
// IPv4 routes: /proc/net/route — always readable without root; provides
// destination, mask, and metric (decimal) for every IPv4 route.
//
// IPv6 routes: /proc/net/ipv6_route — same access requirements; provides
// destination, prefix length, next hop, and metric (hex) for every IPv6 route.
func selectPhysicalDefaultInterfaces() (*net.Interface, *net.Interface, error) {
	type candidate struct {
		name   string
		metric uint32
	}

	var v4candidates, v6candidates []candidate

	// --- IPv4: read /proc/net/route ---
	// Columns: Iface Dest Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
	// Dest and Mask are 4-byte values in 8 hex chars, little-endian. Metric is decimal.
	f4, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, nil, fmt.Errorf("reading IPv4 routing table: %w", err)
	}
	defer f4.Close() //nolint:errcheck

	scanner4 := bufio.NewScanner(f4)
	scanner4.Scan() // skip header
	for scanner4.Scan() {
		fields := strings.Fields(scanner4.Text())
		if len(fields) < 8 {
			continue
		}
		dest, err := hex.DecodeString(fields[1])
		if err != nil || len(dest) != 4 {
			continue
		}
		mask, err := hex.DecodeString(fields[7])
		if err != nil || len(mask) != 4 {
			continue
		}
		// Default route: 0.0.0.0/0
		if binary.LittleEndian.Uint32(dest) != 0 || binary.LittleEndian.Uint32(mask) != 0 {
			continue
		}
		name := fields[0]
		if !isSysfsPhysical(name) {
			continue
		}
		// Metric column is decimal.
		metric, err := strconv.ParseUint(fields[6], 10, 32)
		if err != nil {
			continue
		}
		v4candidates = append(v4candidates, candidate{name, uint32(metric)})
	}
	if err := scanner4.Err(); err != nil {
		return nil, nil, fmt.Errorf("scanning IPv4 routing table: %w", err)
	}

	// --- IPv6: read /proc/net/ipv6_route ---
	// Columns: dest destpfxlen src srcpfxlen nexthop metric refcnt use flags iface
	// All addresses are 32 hex chars (no colons). Metric is hex. Iface is last.
	f6, err := os.Open("/proc/net/ipv6_route")
	if err == nil {
		defer f6.Close() //nolint:errcheck
		scanner6 := bufio.NewScanner(f6)
		for scanner6.Scan() {
			fields := strings.Fields(scanner6.Text())
			if len(fields) < 10 {
				continue
			}
			// Default route: destination = ::/0
			if fields[0] != "00000000000000000000000000000000" {
				continue
			}
			pfxLen, err := strconv.ParseUint(fields[1], 16, 8)
			if err != nil || pfxLen != 0 {
				continue
			}
			// Skip on-link entries that have no actual gateway.
			if fields[4] == "00000000000000000000000000000000" {
				continue
			}
			name := fields[len(fields)-1]
			if !isSysfsPhysical(name) {
				continue
			}
			// Metric is hex in ipv6_route (unlike decimal in /proc/net/route).
			metric, err := strconv.ParseUint(fields[5], 16, 32)
			if err != nil {
				continue
			}
			v6candidates = append(v6candidates, candidate{name, uint32(metric)})
		}
		// IPv6 scanner errors are non-fatal — leave result.IPv6 as nil.
	}
	// If /proc/net/ipv6_route is absent, IPv6 is not configured; that is not an error.

	// Pick the lowest-metric candidate per family that also has a routable address,
	// confirming DHCP/SLAAC has completed and the interface is actively communicating.
	var ipv4Iface, ipv6Iface *net.Interface
	var bestV4Metric, bestV6Metric uint32

	for _, c := range v4candidates {
		iface, err := net.InterfaceByName(c.name)
		if err != nil || !hasRoutableIPv4(iface) {
			continue
		}
		if ipv4Iface == nil || c.metric < bestV4Metric {
			ipv4Iface = iface
			bestV4Metric = c.metric
		}
	}

	for _, c := range v6candidates {
		iface, err := net.InterfaceByName(c.name)
		if err != nil || !hasRoutableIPv6(iface) {
			continue
		}
		if ipv6Iface == nil || c.metric < bestV6Metric {
			ipv6Iface = iface
			bestV6Metric = c.metric
		}
	}

	return ipv4Iface, ipv6Iface, nil
}

// isSysfsPhysical reports whether the named interface is backed by a real
// hardware driver. The kernel creates /sys/class/net/<name>/device only for
// adapters bound to an actual device driver (PCI/USB Ethernet, wireless card).
// Virtual interfaces — tun, tap, bridge, veth, wireguard, loopback — never
// have this sysfs entry.
func isSysfsPhysical(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name + "/device")
	return err == nil
}
