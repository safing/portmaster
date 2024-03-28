package netenv

import (
	"net"
)

// TODO: find a good way to identify a network
// best options until now:
// MAC of gateway
// domain parameter of dhcp

// TODO: get dhcp servers on windows:
// doc: https://msdn.microsoft.com/en-us/library/windows/desktop/aa365917
// this info might already be included in the interfaces api provided by golang!

// Nameserver describes a system assigned namserver.
type Nameserver struct {
	IP     net.IP
	Search []string
}
