//go:build !android

package netenv

import (
	"net"
)

func osGetInterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}
