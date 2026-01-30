package packet

import (
	"net"
)

type RedirectRequest interface {
	// ReplyRedirect sends a redirect response for the request.
	// If localInterfaceIP is nil (or zeroed 0.0.0.0), no redirection will be performed (i.e., the connection will go through as normal).
	// If localInterfaceIP is non-nil, the connection will be redirected to the specified local interface IP.
	// Note: in case of problems with IP conversion, an error will be returned and no command will be sent to the kext.
	ReplyRedirect(localInterface_IPv4 *net.IP, localInterface_IPv6 *net.IP) error

	// Getters:

	ProcessID() uint64
}
