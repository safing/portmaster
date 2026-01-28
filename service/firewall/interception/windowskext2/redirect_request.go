package windowskext

import (
	"net"

	"github.com/safing/portmaster/service/network/packet"
)

type BindRedirectRequest struct {
	Request_ID uint64
	ProcID     uint64
	IpVersion  packet.IPVersion
	Protocol   packet.IPProtocol
	LocalIP    net.IP
	LocalPort  uint16
}

func (r *BindRedirectRequest) ReplyRedirect(localInterfaceIP *net.IP) error {
	return SendRedirectResponseCommand(r, localInterfaceIP)
}

func (r *BindRedirectRequest) ProcessID() uint64 {
	return r.ProcID
}

func (r *BindRedirectRequest) IsIPv6() bool {
	return r.IpVersion == packet.IPv6
}

func (r *BindRedirectRequest) ProtocolType() packet.IPProtocol {
	return r.Protocol
}

func (r *BindRedirectRequest) LocalAddress() net.IP {
	return r.LocalIP
}

func (r *BindRedirectRequest) LocalPortNumber() uint16 {
	return r.LocalPort
}
