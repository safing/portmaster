//go:build windows
// +build windows

package windowskext

import (
	"fmt"
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/windows_kext/kextinterface"
)

// Packet represents an IP packet.
type Packet struct {
	packet.Base

	verdictRequest uint64
	payload        []byte
	payloadLayer   uint8
	verdictSet     *abool.AtomicBool

	payloadLoaded bool
	lock          sync.Mutex
}

// FastTrackedByIntegration returns whether the packet has been fast-track
// accepted by the OS integration.
func (pkt *Packet) FastTrackedByIntegration() bool {
	return false
}

// InfoOnly returns whether the packet is informational only and does not
// represent an actual packet.
func (pkt *Packet) InfoOnly() bool {
	return false
}

// ExpectInfo returns whether the next packet is expected to be informational only.
func (pkt *Packet) ExpectInfo() bool {
	return false
}

// GetPayload returns the full raw packet.
func (pkt *Packet) LoadPacketData() error {
	pkt.lock.Lock()
	defer pkt.lock.Unlock()

	if !pkt.payloadLoaded {
		pkt.payloadLoaded = true

		if len(pkt.payload) > 0 {
			var err error
			switch pkt.payloadLayer {
			case 3:
				err = packet.ParseLayer3(pkt.payload, &pkt.Base)
			case 4:
				err = packet.ParseLayer4(pkt.payload, &pkt.Base)
			default:
				err = fmt.Errorf("unsupported payload layer: %d", pkt.payloadLayer)
			}
			if err != nil {
				log.Tracef("payload: %#v", pkt.payload)
				log.Tracer(pkt.Ctx()).Warningf("windowskext: failed to parse payload: %s", err)
				return packet.ErrFailedToLoadPayload
			}
		}
	}

	if len(pkt.Raw()) == 0 {
		return packet.ErrFailedToLoadPayload
	}

	return nil
}

// Accept accepts the packet.
func (pkt *Packet) Accept() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictAccept)
	}
	return nil
}

// Block blocks the packet.
func (pkt *Packet) Block() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictBlock)
	}
	return nil
}

// Drop drops the packet.
func (pkt *Packet) Drop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictDrop)
	}
	return nil
}

// PermanentAccept permanently accepts connection (and the current packet).
func (pkt *Packet) PermanentAccept() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictPermanentAccept)
	}
	return nil
}

// PermanentBlock permanently blocks connection (and the current packet).
func (pkt *Packet) PermanentBlock() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictPermanentBlock)
	}
	return nil
}

// PermanentDrop permanently drops connection (and the current packet).
func (pkt *Packet) PermanentDrop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictPermanentDrop)
	}
	return nil
}

// RerouteToNameserver permanently reroutes the connection to the local nameserver (and the current packet).
func (pkt *Packet) RerouteToNameserver() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictRerouteToNameserver)
	}
	return nil
}

// RerouteToTunnel permanently reroutes the connection to the local tunnel entrypoint (and the current packet).
func (pkt *Packet) RerouteToTunnel() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, kextinterface.VerdictRerouteToTunnel)
	}
	return nil
}
