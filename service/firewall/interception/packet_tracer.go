package interception

import (
	"time"

	"github.com/safing/portmaster/service/network/packet"
)

type tracedPacket struct {
	start time.Time
	packet.Packet
}

func tracePacket(p packet.Packet) packet.Packet {
	return &tracedPacket{
		start:  time.Now(),
		Packet: p,
	}
}

func (p *tracedPacket) markServed(v string) {
	if packetMetricsDestination == "" {
		return
	}

	metrics.record(p, v)
}

func (p *tracedPacket) Accept() error {
	defer p.markServed("accept")
	return p.Packet.Accept()
}

func (p *tracedPacket) Block() error {
	defer p.markServed("block")
	return p.Packet.Block()
}

func (p *tracedPacket) Drop() error {
	defer p.markServed("drop")
	return p.Packet.Drop()
}

func (p *tracedPacket) PermanentAccept() error {
	defer p.markServed("perm-accept")
	return p.Packet.PermanentAccept()
}

func (p *tracedPacket) PermanentBlock() error {
	defer p.markServed("perm-block")
	return p.Packet.PermanentBlock()
}

func (p *tracedPacket) PermanentDrop() error {
	defer p.markServed("perm-drop")
	return p.Packet.PermanentDrop()
}

func (p *tracedPacket) RerouteToNameserver() error {
	defer p.markServed("reroute-ns")
	return p.Packet.RerouteToNameserver()
}

func (p *tracedPacket) RerouteToTunnel() error {
	defer p.markServed("reroute-tunnel")
	return p.Packet.RerouteToTunnel()
}
