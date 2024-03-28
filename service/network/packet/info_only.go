package packet

import (
	"errors"
	"fmt"
)

// InfoPacket does not represent an actual packet, but only holds metadata.
// Implements the packet.Packet interface.
type InfoPacket struct {
	Base
}

// NewInfoPacket returns a new InfoPacket with the given info.
func NewInfoPacket(info Info) *InfoPacket {
	return &InfoPacket{
		Base{
			info: info,
		},
	}
}

// InfoOnly returns whether the packet is informational only and does not
// represent an actual packet.
func (pkt *InfoPacket) InfoOnly() bool {
	return true
}

// LoadPacketData does nothing on Linux, as data is always fully parsed.
func (pkt *InfoPacket) LoadPacketData() error {
	return fmt.Errorf("%w: info-only packet", ErrFailedToLoadPayload)
}

// ErrInfoOnlyPacket is returned for unsupported operations on an info-only packet.
var ErrInfoOnlyPacket = errors.New("info-only packet")

// Accept does nothing on an info-only packet.
func (pkt *InfoPacket) Accept() error {
	return ErrInfoOnlyPacket
}

// Block does nothing on an info-only packet.
func (pkt *InfoPacket) Block() error {
	return ErrInfoOnlyPacket
}

// Drop does nothing on an info-only packet.
func (pkt *InfoPacket) Drop() error {
	return ErrInfoOnlyPacket
}

// PermanentAccept does nothing on an info-only packet.
func (pkt *InfoPacket) PermanentAccept() error {
	return ErrInfoOnlyPacket
}

// PermanentBlock does nothing on an info-only packet.
func (pkt *InfoPacket) PermanentBlock() error {
	return ErrInfoOnlyPacket
}

// PermanentDrop does nothing on an info-only packet.
func (pkt *InfoPacket) PermanentDrop() error {
	return ErrInfoOnlyPacket
}

// RerouteToNameserver does nothing on an info-only packet.
func (pkt *InfoPacket) RerouteToNameserver() error {
	return ErrInfoOnlyPacket
}

// RerouteToTunnel does nothing on an info-only packet.
func (pkt *InfoPacket) RerouteToTunnel() error {
	return ErrInfoOnlyPacket
}

var _ Packet = &InfoPacket{}
