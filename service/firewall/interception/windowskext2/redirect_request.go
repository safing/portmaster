package windowskext

import (
	"net"

	"github.com/safing/portmaster/service/network/packet"
)

type ConnectRedirectRequest struct {
	Request_ID uint64
	ProcID     uint64
	Inbound    bool
	IpVersion  packet.IPVersion
	Protocol   packet.IPProtocol
	LocalIP    net.IP
	RemoteIP   net.IP
	LocalPort  uint16
	RemotePort uint16
}

func (r *ConnectRedirectRequest) ReplyRedirect(localInterfaceIP *net.IP) error {
	return SendRedirectResponseCommand(r, localInterfaceIP)
}

func (r *ConnectRedirectRequest) ProcessID() uint64 {
	return r.ProcID
}

func (r *ConnectRedirectRequest) IsInbound() bool {
	return r.Inbound
}

func (r *ConnectRedirectRequest) IsIPv6() bool {
	return r.IpVersion == packet.IPv6
}

func (r *ConnectRedirectRequest) ProtocolType() packet.IPProtocol {
	return r.Protocol
}

func (r *ConnectRedirectRequest) LocalAddress() net.IP {
	return r.LocalIP
}

func (r *ConnectRedirectRequest) RemoteAddress() net.IP {
	return r.RemoteIP
}

func (r *ConnectRedirectRequest) LocalPortNumber() uint16 {
	return r.LocalPort
}

func (r *ConnectRedirectRequest) RemotePortNumber() uint16 {
	return r.RemotePort
}
