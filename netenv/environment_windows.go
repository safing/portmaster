package netenv

import "net"

// Nameservers returns the currently active nameservers.
func Nameservers() []Nameserver {
	return nil
}

// Gateways returns the currently active gateways.
func Gateways() []*net.IP {
	return nil
}
