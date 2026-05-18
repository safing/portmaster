package netenv

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/network/netutils"
)

// isRoutableIP returns true for IPs that the cache keeps: site-local or global.
// Matches the isRoutableUnicastIP predicate used in production code.
func isRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	scope := netutils.GetIPScope(ip)
	return scope == netutils.SiteLocal || scope == netutils.Global
}

// getTestInterface picks the first network interface that matches the same
// criteria as the cache: FlagUp and at least one routable (non-link-local)
// unicast address. Falls back to loopback if no other candidate is found.
func getTestInterface(t *testing.T) net.Interface {
	t.Helper()

	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces() failed: %v", err)
	}

	for i := range ifaces {
		iface := ifaces[i]

		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		// Mirror the cache filter: loopback is excluded.
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, _ := iface.Addrs()
		hasRoutable := false
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if isRoutableIP(ip) {
				hasRoutable = true
				break
			}
		}
		if !hasRoutable {
			continue
		}

		return iface
	}

	t.Skip("no usable non-loopback network interface found – skipping test")
	panic("unreachable")
}

// firstRoutableIP returns the first routable (non-link-local) unicast IP
// assigned to iface, or nil if none exists.
func firstRoutableIP(iface net.Interface) net.IP {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if isRoutableIP(ip) {
			return ip
		}
	}
	return nil
}

// firstRoutableIPv4 returns the first routable IPv4 address on iface, or nil.
func firstRoutableIPv4(iface net.Interface) net.IP {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if isRoutableIP(ip) {
			if ip4 := ip.To4(); ip4 != nil {
				return ip4
			}
		}
	}
	return nil
}

// firstRoutableIPv6 returns the first routable IPv6 address on iface, or nil.
func firstRoutableIPv6(iface net.Interface) net.IP {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if isRoutableIP(ip) && ip.To4() == nil {
			return ip
		}
	}
	return nil
}

// ---- GetInterfaceByName -------------------------------------------------------

func TestGetInterfaceByName(t *testing.T) {
	t.Parallel()

	want := getTestInterface(t)

	got, err := GetInterfaceByName(want.Name)
	if err != nil {
		t.Fatalf("GetInterfaceByName(%q): unexpected error: %v", want.Name, err)
	}
	if got.Interface.Name != want.Name {
		t.Errorf("GetInterfaceByName(%q): got %q", want.Name, got.Interface.Name)
	}
}

func TestGetInterfaceByName_NotFound(t *testing.T) {
	t.Parallel()

	_, err := GetInterfaceByName("__no_such_interface__")
	if err == nil {
		t.Fatal("expected error for unknown interface name, got nil")
	}
}

// ---- GetInterfaceByIP --------------------------------------------------------

func TestGetInterfaceByIP(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	ip := firstRoutableIP(iface)
	if ip == nil {
		t.Skipf("interface %q has no routable address – skipping", iface.Name)
	}

	got, err := GetInterfaceByIP(ip)
	if err != nil {
		t.Fatalf("GetInterfaceByIP(%s): unexpected error: %v", ip, err)
	}
	if got.Interface.Name != iface.Name {
		t.Errorf("GetInterfaceByIP(%s): got interface %q, want %q", ip, got.Interface.Name, iface.Name)
	}
}

func TestGetInterfaceByIP_NotFound(t *testing.T) {
	t.Parallel()

	// 192.0.2.0/24 is TEST-NET-1 (RFC 5737) – never assigned on a real host.
	ip := net.ParseIP("192.0.2.1")
	_, err := GetInterfaceByIP(ip)
	if err == nil {
		t.Fatal("expected error for unassigned IP, got nil")
	}
}

// ---- GetInterfaceByMAC -------------------------------------------------------

func TestGetInterfaceByMAC(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	if len(iface.HardwareAddr) == 0 {
		t.Skipf("interface %q has no hardware address – skipping", iface.Name)
	}

	got, err := GetInterfaceByMAC(iface.HardwareAddr)
	if err != nil {
		t.Fatalf("GetInterfaceByMAC(%s): unexpected error: %v", iface.HardwareAddr, err)
	}
	if got.Interface.Name != iface.Name {
		t.Errorf("GetInterfaceByMAC(%s): got interface %q, want %q",
			iface.HardwareAddr, got.Interface.Name, iface.Name)
	}
}

// ---- GetInterface (multi-mode) -----------------------------------------------

func TestGetInterface_ByName(t *testing.T) {
	t.Parallel()

	want := getTestInterface(t)

	got, err := GetInterface(want.Name)
	if err != nil {
		t.Fatalf("GetInterface(%q) by name: unexpected error: %v", want.Name, err)
	}
	if got.Interface.Name != want.Name {
		t.Errorf("GetInterface(%q): got %q", want.Name, got.Interface.Name)
	}
}

func TestGetInterface_ByIP(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	ip := firstRoutableIP(iface)
	if ip == nil {
		t.Skipf("interface %q has no routable address – skipping", iface.Name)
	}
	ipStr := ip.String()

	got, err := GetInterface(ipStr)
	if err != nil {
		t.Fatalf("GetInterface(%q) by IP: unexpected error: %v", ipStr, err)
	}
	if got.Interface.Name != iface.Name {
		t.Errorf("GetInterface(%q): got %q, want %q", ipStr, got.Interface.Name, iface.Name)
	}
}

func TestGetInterface_ByMAC(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	if len(iface.HardwareAddr) == 0 {
		t.Skipf("interface %q has no hardware address – skipping", iface.Name)
	}
	macStr := iface.HardwareAddr.String()

	got, err := GetInterface(macStr)
	if err != nil {
		t.Fatalf("GetInterface(%q) by MAC: unexpected error: %v", macStr, err)
	}
	if got.Interface.Name != iface.Name {
		t.Errorf("GetInterface(%q): got %q, want %q", macStr, got.Interface.Name, iface.Name)
	}
}

func TestGetInterface_NotFound(t *testing.T) {
	t.Parallel()

	_, err := GetInterface("__no_such_interface__")
	if err == nil {
		t.Fatal("expected error for unrecognised ifinfo, got nil")
	}
}

// TestGetInterfaceByIP_LinkLocalIPv6 verifies that IPv6 link-local addresses
// are filtered out of the cache and therefore never match a lookup.
func TestGetInterfaceByIP_LinkLocalIPv6(t *testing.T) {
	t.Parallel()

	ip := net.ParseIP("fe80::1")
	_, err := GetInterfaceByIP(ip)
	if err == nil {
		t.Error("expected error for link-local IP fe80::1, got nil")
	}
}

// TestGetInterfaceByIP_LinkLocalIPv4 verifies that IPv4 link-local addresses
// (APIPA range 169.254.x.x) are filtered out of the cache.
func TestGetInterfaceByIP_LinkLocalIPv4(t *testing.T) {
	t.Parallel()

	ip := net.ParseIP("169.254.0.1")
	_, err := GetInterfaceByIP(ip)
	if err == nil {
		t.Error("expected error for link-local IP 169.254.0.1, got nil")
	}
}

// TestGetInterface_RepeatedCall verifies that repeated calls with the same
// argument succeed consistently (exercises the list cache path).
func TestGetInterface_RepeatedCall(t *testing.T) {
	t.Parallel()

	want := getTestInterface(t)

	got1, err := GetInterface(want.Name)
	if err != nil {
		t.Fatalf("first GetInterface(%q): %v", want.Name, err)
	}

	got2, err := GetInterface(want.Name)
	if err != nil {
		t.Fatalf("second GetInterface(%q): %v", want.Name, err)
	}

	if got1.Interface.Name != got2.Interface.Name {
		t.Errorf("inconsistent results: got %q then %q", got1.Interface.Name, got2.Interface.Name)
	}
}

// ---- InterfaceInfo bind-address fields ---------------------------------------

// TestGetInterfaceByIP_MatchedIPv4InInfo verifies that when an interface is
// found by an IPv4 address, that exact IP is returned in InterfaceInfo.IPv4.
func TestGetInterfaceByIP_MatchedIPv4InInfo(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	ip := firstRoutableIPv4(iface)
	if ip == nil {
		t.Skipf("interface %q has no routable IPv4 address – skipping", iface.Name)
	}

	info, err := GetInterfaceByIP(ip)
	if err != nil {
		t.Fatalf("GetInterfaceByIP(%s): unexpected error: %v", ip, err)
	}
	if !info.IPv4.Equal(ip) {
		t.Errorf("InterfaceInfo.IPv4: got %s, want %s", info.IPv4, ip)
	}
}

// TestGetInterfaceByIP_MatchedIPv6InInfo verifies that when an interface is
// found by an IPv6 address, that exact IP is returned in InterfaceInfo.IPv6.
func TestGetInterfaceByIP_MatchedIPv6InInfo(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	ip := firstRoutableIPv6(iface)
	if ip == nil {
		t.Skipf("interface %q has no routable IPv6 address – skipping", iface.Name)
	}

	info, err := GetInterfaceByIP(ip)
	if err != nil {
		t.Fatalf("GetInterfaceByIP(%s): unexpected error: %v", ip, err)
	}
	if !info.IPv6.Equal(ip) {
		t.Errorf("InterfaceInfo.IPv6: got %s, want %s", info.IPv6, ip)
	}
}

// TestGetInterfaceByName_IPv4InInfo verifies that when an interface is found
// by name, InterfaceInfo.IPv4 is populated with the first routable IPv4 address.
func TestGetInterfaceByName_IPv4InInfo(t *testing.T) {
	t.Parallel()

	iface := getTestInterface(t)
	expectedIPv4 := firstRoutableIPv4(iface)
	if expectedIPv4 == nil {
		t.Skipf("interface %q has no routable IPv4 address – skipping", iface.Name)
	}

	info, err := GetInterfaceByName(iface.Name)
	if err != nil {
		t.Fatalf("GetInterfaceByName(%q): unexpected error: %v", iface.Name, err)
	}
	if !info.IPv4.Equal(expectedIPv4) {
		t.Errorf("InterfaceInfo.IPv4: got %s, want %s", info.IPv4, expectedIPv4)
	}
}

// ---- Helper functions for logging -------------------------------------------------------

// logInterfaceInfo logs IPv4 and IPv6 interface info from PhysicalDefaultInterfaces.
func logInterfaceInfo(t *testing.T, label string, result PhysicalDefaultInterfaces) {
	logIP := func(version string, info *InterfaceInfo) {
		if info == nil {
			t.Logf("%s - %s: <nil>", label, version)
			return
		}

		var ip net.IP
		if version == "IPv4" {
			ip = info.IPv4
		} else {
			ip = info.IPv6
		}

		name := info.Interface.Name
		if ip != nil {
			t.Logf("%s - %s: %s (%s)", label, version, name, ip)
		} else {
			t.Logf("%s - %s: %s", label, version, name)
		}
	}

	logIP("IPv4", result.ForIPv4)
	logIP("IPv6", result.ForIPv6)
}

// ---- GetBestPhysicalDefaultInterfaces() -----------------------------------------------------

// TestGetBestPhysicalDefaultInterfaces verifies that GetBestPhysicalDefaultInterfaces
// returns at least one valid physical interface and that each non-nil result
// has a routable address for its respective family.
func TestGetBestPhysicalDefaultInterfaces(t *testing.T) {
	t.Parallel()

	result, err := GetBestPhysicalDefaultInterfaces()
	if err != nil {
		t.Fatalf("GetBestPhysicalDefaultInterfaces: unexpected error: %v", err)
	}

	// Print found interfaces
	logInterfaceInfo(t, "Result", PhysicalDefaultInterfaces{ForIPv4: result.ForIPv4, ForIPv6: result.ForIPv6})

	// At least one family must be resolved on any connected machine.
	if result.ForIPv4 == nil && result.ForIPv6 == nil {
		t.Fatal("GetBestPhysicalDefaultInterfaces: both ForIPv4 and ForIPv6 are nil; expected at least one")
	}

	if result.ForIPv4 != nil && !hasRoutableIPv4(result.ForIPv4.Interface) {
		t.Errorf("GetBestPhysicalDefaultInterfaces: ForIPv4 interface %q has no routable IPv4 address", result.ForIPv4.Interface.Name)
	}

	if result.ForIPv6 != nil && !hasRoutableIPv6(result.ForIPv6.Interface) {
		t.Errorf("GetBestPhysicalDefaultInterfaces: ForIPv6 interface %q has no routable IPv6 address", result.ForIPv6.Interface.Name)
	}
}

// TestGetBestPhysicalDefaultInterfaces_Repeated verifies that repeated calls
// return consistent results (exercises the cache fast-path).
func TestGetBestPhysicalDefaultInterfaces_Repeated(t *testing.T) {
	t.Parallel()

	first, err := GetBestPhysicalDefaultInterfaces()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := GetBestPhysicalDefaultInterfaces()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	firstName4 := ifaceName(first.ForIPv4)
	firstName6 := ifaceName(first.ForIPv6)
	secondName4 := ifaceName(second.ForIPv4)
	secondName6 := ifaceName(second.ForIPv6)

	// Print found interfaces from both calls
	logInterfaceInfo(t, "First call", first)
	logInterfaceInfo(t, "Second call", second)

	if firstName4 != secondName4 {
		t.Errorf("IPv4: inconsistent results across calls: %q then %q", firstName4, secondName4)
	}
	if firstName6 != secondName6 {
		t.Errorf("IPv6: inconsistent results across calls: %q then %q", firstName6, secondName6)
	}
}

// ifaceName returns the interface name or "<nil>" for a nil InterfaceInfo.
// Used to produce readable test failure messages.
func ifaceName(info *InterfaceInfo) string {
	if info == nil {
		return "<nil>"
	}
	return info.Interface.Name
}
