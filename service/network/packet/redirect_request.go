package packet

import (
	"net"
)

type RedirectRequest interface {
	// ReplyRedirect sends a no-redirect response for the request.
	// If localInterfaceIP is nil, no redirection will be performed (i.e., the connection will go through as normal).
	// If localInterfaceIP is non-nil, the connection will be redirected to the specified local interface IP.
	// Note: in case of problems with IP conversion, an error will be returned and no command will be sent to the kext.
	ReplyRedirect(localInterfaceIP *net.IP) error

	// Getters:

	ProcessID() uint64
	IsIPv6() bool
	ProtocolType() IPProtocol
	LocalAddress() net.IP
	LocalPortNumber() uint16
}
