package netenv

import (
	"fmt"
	"testing"
)

func TestGetAssignedAddresses(t *testing.T) {
	ipv4, ipv6, err := GetAssignedAddresses()
	fmt.Printf("all v4: %v", ipv4)
	fmt.Printf("all v6: %v", ipv6)
	if err != nil {
		t.Fatalf("failed to get addresses: %s", err)
	}
	if len(ipv4) == 0 && len(ipv6) == 0 {
		t.Fatal("GetAssignedAddresses did not return any addresses")
	}
}

func TestGetAssignedGlobalAddresses(t *testing.T) {
	ipv4, ipv6, err := GetAssignedGlobalAddresses()
	fmt.Printf("all global v4: %v", ipv4)
	fmt.Printf("all global v6: %v", ipv6)
	if err != nil {
		t.Fatalf("failed to get addresses: %s", err)
	}
}
