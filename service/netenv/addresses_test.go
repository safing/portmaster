package netenv

import (
	"net"
	"testing"
)

func TestGetAssignedAddresses(t *testing.T) {
	t.Parallel()

	ipv4, ipv6, err := GetAssignedAddresses()
	t.Logf("all v4: %v", ipv4)
	t.Logf("all v6: %v", ipv6)
	if err != nil {
		t.Fatalf("failed to get addresses: %s", err)
	}
	if len(ipv4) == 0 && len(ipv6) == 0 {
		t.Fatal("GetAssignedAddresses did not return any addresses")
	}
}

func TestGetAssignedGlobalAddresses(t *testing.T) {
	t.Parallel()

	ipv4, ipv6, err := GetAssignedGlobalAddresses()
	t.Logf("all global v4: %v", ipv4)
	t.Logf("all global v6: %v", ipv6)
	if err != nil {
		t.Fatalf("failed to get addresses: %s", err)
	}
}

func TestGetLocalInterfaceIPs(t *testing.T) {
	t.Parallel()

	// Test empty identifier
	ipv4, ipv6 := GetLocalInterfaceIPs("")
	if ipv4 != nil || ipv6 != nil {
		t.Error("expected nil for empty identifier")
	}

	// Test non-existent interface
	ipv4, ipv6 = GetLocalInterfaceIPs("nonexistent-interface-12345")
	if ipv4 != nil || ipv6 != nil {
		t.Error("expected nil for non-existent interface")
	}

	// Find active interface for positive test
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("failed to get interfaces: %v", err)
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			ipv4, ipv6 := GetLocalInterfaceIPs(iface.Name)

			// Validate IPs are not loopback
			if ipv4 != nil && ipv4.IsLoopback() {
				t.Errorf("IPv4 should not be loopback: %v", ipv4)
			}
			if ipv6 != nil && ipv6.IsLoopback() {
				t.Errorf("IPv6 should not be loopback: %v", ipv6)
			}

			// Test lookup by MAC address
			if len(iface.HardwareAddr) > 0 {
				macIPv4, macIPv6 := GetLocalInterfaceIPs(iface.HardwareAddr.String())
				if macIPv4 == nil && macIPv6 == nil {
					t.Error("expected to find interface by MAC address")
				}
			}

			// Test lookup by IP address
			if ipv4 != nil {
				byIPv4, byIPv6 := GetLocalInterfaceIPs(ipv4.String())
				if byIPv4 == nil && byIPv6 == nil {
					t.Error("expected to find interface by IPv4 address")
				}
				if byIPv4 != nil && !byIPv4.Equal(*ipv4) {
					t.Errorf("expected IPv4 %v, got %v", ipv4, byIPv4)
				}
				if byIPv6 != nil && (ipv6 == nil || !byIPv6.Equal(*ipv6)) {
					t.Errorf("IPv6 mismatch: expected %v, got %v", ipv6, byIPv6)
				}
			}
			if ipv6 != nil {
				byIPv4, byIPv6 := GetLocalInterfaceIPs(ipv6.String())
				if byIPv4 == nil && byIPv6 == nil {
					t.Error("expected to find interface by IPv6 address")
				}
				if byIPv4 != nil && (ipv4 == nil || !byIPv4.Equal(*ipv4)) {
					t.Errorf("IPv4 mismatch: expected %v, got %v", ipv4, byIPv4)
				}
				if byIPv6 != nil && !byIPv6.Equal(*ipv6) {
					t.Errorf("expected IPv6 %v, got %v", ipv6, byIPv6)
				}
			}

			return
		}
	}

	t.Skip("no active non-loopback interface found")
}
