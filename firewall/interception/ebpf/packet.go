//go:build linux

package ebpf

import (
	pmpacket "github.com/safing/portmaster/network/packet"
)

// packet implements the packet.Packet interface.
type infoPacket struct {
	pmpacket.Base
}

// LoadPacketData does nothing on Linux, as data is always fully parsed.
func (pkt *infoPacket) LoadPacketData() error {
	return nil // fmt.Errorf("can't load data info only packet")
}

func (pkt *infoPacket) Accept() error {
	return nil // fmt.Errorf("can't accept info only packet")
}

func (pkt *infoPacket) Block() error {
	return nil // fmt.Errorf("can't block info only packet")
}

func (pkt *infoPacket) Drop() error {
	return nil // fmt.Errorf("can't block info only packet")
}

func (pkt *infoPacket) PermanentAccept() error {
	return pkt.Accept()
}

func (pkt *infoPacket) PermanentBlock() error {
	return pkt.Block()
}

func (pkt *infoPacket) PermanentDrop() error {
	return nil // fmt.Errorf("can't drop info only packet")
}

func (pkt *infoPacket) RerouteToNameserver() error {
	return nil // fmt.Errorf("can't reroute info only packet")
}

func (pkt *infoPacket) RerouteToTunnel() error {
	return nil // fmt.Errorf("can't reroute info only packet")
}
