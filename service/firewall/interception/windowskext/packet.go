//go:build windows
// +build windows

package windowskext

import (
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// Packet represents an IP packet.
type Packet struct {
	packet.Base

	verdictRequest *VerdictRequest
	verdictSet     *abool.AtomicBool

	payloadLoaded bool
	lock          sync.Mutex
}

// FastTrackedByIntegration returns whether the packet has been fast-track
// accepted by the OS integration.
func (pkt *Packet) FastTrackedByIntegration() bool {
	return pkt.verdictRequest.flags&VerdictRequestFlagFastTrackPermitted > 0
}

// InfoOnly returns whether the packet is informational only and does not
// represent an actual packet.
func (pkt *Packet) InfoOnly() bool {
	return pkt.verdictRequest.flags&VerdictRequestFlagSocketAuth > 0
}

// ExpectInfo returns whether the next packet is expected to be informational only.
func (pkt *Packet) ExpectInfo() bool {
	return pkt.verdictRequest.flags&VerdictRequestFlagExpectSocketAuth > 0
}

// GetPayload returns the full raw packet.
func (pkt *Packet) LoadPacketData() error {
	pkt.lock.Lock()
	defer pkt.lock.Unlock()

	if pkt.verdictRequest.id == 0 {
		return ErrNoPacketID
	}

	if !pkt.payloadLoaded {
		pkt.payloadLoaded = true

		payload, err := GetPayload(pkt.verdictRequest.id, pkt.verdictRequest.packetSize)
		if err != nil {
			log.Tracer(pkt.Ctx()).Warningf("windowskext: failed to load payload: %s", err)
			return packet.ErrFailedToLoadPayload
		}

		err = packet.ParseLayer3(payload, &pkt.Base)
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
		return SetVerdict(pkt, -network.VerdictAccept)
	}
	return nil
}

// Block blocks the packet.
func (pkt *Packet) Block() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, -network.VerdictBlock)
	}
	return nil
}

// Drop drops the packet.
func (pkt *Packet) Drop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, -network.VerdictDrop)
	}
	return nil
}

// PermanentAccept permanently accepts connection (and the current packet).
func (pkt *Packet) PermanentAccept() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, network.VerdictAccept)
	}
	return nil
}

// PermanentBlock permanently blocks connection (and the current packet).
func (pkt *Packet) PermanentBlock() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, network.VerdictBlock)
	}
	return nil
}

// PermanentDrop permanently drops connection (and the current packet).
func (pkt *Packet) PermanentDrop() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, network.VerdictDrop)
	}
	return nil
}

// RerouteToNameserver permanently reroutes the connection to the local nameserver (and the current packet).
func (pkt *Packet) RerouteToNameserver() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, network.VerdictRerouteToNameserver)
	}
	return nil
}

// RerouteToTunnel permanently reroutes the connection to the local tunnel entrypoint (and the current packet).
func (pkt *Packet) RerouteToTunnel() error {
	if pkt.verdictSet.SetToIf(false, true) {
		return SetVerdict(pkt, network.VerdictRerouteToTunnel)
	}
	return nil
}
