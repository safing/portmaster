package netenv

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/network/netutils"
)

// isRoutableIP returns true for IPs that the cache keeps: non-nil, non-link-local.
func isRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	return netutils.GetIPScope(ip) != netutils.LinkLocal
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

	var loopback *net.Interface
	for i := range ifaces {
		iface := ifaces[i]

		if iface.Flags&net.FlagUp == 0 {
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

		if iface.Flags&net.FlagLoopback != 0 {
			if loopback == nil {
				loopback = &iface
			}
			continue
		}
		return iface
	}

	if loopback != nil {
		return *loopback
	}
	t.Skip("no usable network interface found – skipping test")
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

// ---- GetInterfaceByName -------------------------------------------------------

func TestGetInterfaceByName(t *testing.T) {
	t.Parallel()

	want := getTestInterface(t)

	got, err := GetInterfaceByName(want.Name)
	if err != nil {
		t.Fatalf("GetInterfaceByName(%q): unexpected error: %v", want.Name, err)
	}
	if got.Name != want.Name {
		t.Errorf("GetInterfaceByName(%q): got %q", want.Name, got.Name)
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
	if got.Name != iface.Name {
		t.Errorf("GetInterfaceByIP(%s): got interface %q, want %q", ip, got.Name, iface.Name)
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
	if got.Name != iface.Name {
		t.Errorf("GetInterfaceByMAC(%s): got interface %q, want %q",
			iface.HardwareAddr, got.Name, iface.Name)
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
	if got.Name != want.Name {
		t.Errorf("GetInterface(%q): got %q", want.Name, got.Name)
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
	if got.Name != iface.Name {
		t.Errorf("GetInterface(%q): got %q, want %q", ipStr, got.Name, iface.Name)
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
	if got.Name != iface.Name {
		t.Errorf("GetInterface(%q): got %q, want %q", macStr, got.Name, iface.Name)
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

	if got1.Name != got2.Name {
		t.Errorf("inconsistent results: got %q then %q", got1.Name, got2.Name)
	}
}
