package environment

import (
	"net"
	"sync"
	"time"
)

// TODO: find a good way to identify a network
// best options until now:
// MAC of gateway
// domain parameter of dhcp

// TODO: get dhcp servers on windows:
// windows: https://msdn.microsoft.com/en-us/library/windows/desktop/aa365917
// this info might already be included in the interfaces api provided by golang!

const (
	gatewaysRecheck    = 2 * time.Second
	nameserversRecheck = 2 * time.Second
)

var (
	// interfaces        = make(map[*net.IP]net.Flags)
	// interfacesLock    sync.Mutex
	// interfacesExpires = time.Now()

	gateways        = make([]*net.IP, 0)
	gatewaysLock    sync.Mutex
	gatewaysExpires = time.Now()

	nameservers        = make([]Nameserver, 0)
	nameserversLock    sync.Mutex
	nameserversExpires = time.Now()
)

// Nameserver describes a system assigned namserver.
type Nameserver struct {
	IP     net.IP
	Search []string
}
