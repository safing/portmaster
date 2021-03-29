// +build windows

package windowskext

import (
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

// Packet represents an IP packet.
type Packet struct {
	packet.Base

	verdictRequest *VerdictRequest
	verdictSet     *abool.AtomicBool

	payloadLoaded bool
	lock          sync.Mutex
}

// GetPayload returns the full raw packet.
func (pkt *Packet) LoadPacketData() error {
	pkt.lock.Lock()
	defer pkt.lock.Unlock()

	if !pkt.payloadLoaded {
		pkt.payloadLoaded = true

		payload, err := GetPayload(pkt.verdictRequest.id, pkt.verdictRequest.packetSize)
		if err != nil {
			log.Tracer(pkt.Ctx()).Warningf("windowskext: failed to load payload: %s", err)
			return packet.ErrFailedToLoadPayload
		}

		err = packet.Parse(payload, &pkt.Base)
		if err != nil {
			log.Tracer(pkt.Ctx()).Warningf("windowskext: failed to parse payload: %s", err)
			return packet.ErrFailedToLoadPayload
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
		return SetVerdict(pkt.verdictRequest.id, -network.VerdictAccept)
	}
	return nil
}

// Block blocks the packet.
func (pkt *Packet) Block() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, -network.VerdictBlock)
	}
	return nil
}

// Drop drops the packet.
func (pkt *Packet) Drop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, -network.VerdictDrop)
	}
	return nil
}

// PermanentAccept permanently accepts connection (and the current packet).
func (pkt *Packet) PermanentAccept() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, network.VerdictAccept)
	}
	return nil
}

// PermanentBlock permanently blocks connection (and the current packet).
func (pkt *Packet) PermanentBlock() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, network.VerdictBlock)
	}
	return nil
}

// PermanentDrop permanently drops connection (and the current packet).
func (pkt *Packet) PermanentDrop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, network.VerdictDrop)
	}
	return nil
}

// RerouteToNameserver permanently reroutes the connection to the local nameserver (and the current packet).
func (pkt *Packet) RerouteToNameserver() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, network.VerdictRerouteToNameserver)
	}
	return nil
}

// RerouteToTunnel permanently reroutes the connection to the local tunnel entrypoint (and the current packet).
func (pkt *Packet) RerouteToTunnel() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt.verdictRequest.id, network.VerdictRerouteToTunnel)
	}
	return nil
}
