package netenv

import (
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
