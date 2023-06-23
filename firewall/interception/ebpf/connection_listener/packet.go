//go:build linux

package ebpf

import (
	"fmt"

	pmpacket "github.com/safing/portmaster/network/packet"
)

// packet implements the packet.Packet interface.
type infoPacket struct {
	pmpacket.Base
}

// LoadPacketData does nothing on Linux, as data is always fully parsed.
func (pkt *infoPacket) LoadPacketData() error {
	return fmt.Errorf("can't load data in info only packet")
}

func (pkt *infoPacket) Accept() error {
	return nil
}

func (pkt *infoPacket) Block() error {
	return nil
}

func (pkt *infoPacket) Drop() error {
	return nil
}

func (pkt *infoPacket) PermanentAccept() error {
	return pkt.Accept()
}

func (pkt *infoPacket) PermanentBlock() error {
	return pkt.Block()
}

func (pkt *infoPacket) PermanentDrop() error {
	return nil
}

func (pkt *infoPacket) RerouteToNameserver() error {
	return nil
}

func (pkt *infoPacket) RerouteToTunnel() error {
	return nil
}
