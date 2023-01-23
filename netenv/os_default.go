//go:build !android

package netenv

import (
	"net"
)

func osGetInterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

func osGetNetworkInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}
