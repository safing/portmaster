//go:build !android

package netenv

import (
	"net"
	"time"
)

var (
	monitorNetworkChangeOnlineTicker  = time.NewTicker(15 * time.Second)
	monitorNetworkChangeOfflineTicker = time.NewTicker(time.Second)
)

func osGetInterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

func osGetNetworkInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}
