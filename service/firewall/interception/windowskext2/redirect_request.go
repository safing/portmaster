package windowskext

import (
	"net"
)

type BindRedirectRequest struct {
	Request_ID uint64
	ProcID     uint64
}

func (r *BindRedirectRequest) ReplyRedirect(localInterface_IPv4 *net.IP, localInterface_IPv6 *net.IP) error {
	return SendRedirectResponseCommand(r, localInterface_IPv4, localInterface_IPv6)
}

func (r *BindRedirectRequest) ProcessID() uint64 {
	return r.ProcID
}
