//go:build !linux && !windows

package netenv

import "net"

// selectPhysicalDefaultInterfaces is not implemented on this platform.
func selectPhysicalDefaultInterfaces() (*net.Interface, *net.Interface, error) {
	return nil, nil, errNoPhysicalDefaultInterface
}
