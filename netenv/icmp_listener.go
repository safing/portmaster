package netenv

import (
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

var (
	// listenICMPLock locks the ICMP listening system for one user at a time.
	listenICMPLock sync.Mutex

	// listenICMPEnabled defines whether or not the firewall should send ICMP
	// packets through this interface.
	listenICMPEnabled = abool.New()

	// listenICMPInput is created for every use of the ICMP listenting system.
	listenICMPInput     chan packet.Packet
	listenICMPInputLock sync.Mutex
)

func ListenToICMP() (packets chan packet.Packet, done func()) {
	// Lock for single use.
	listenICMPLock.Lock()

	// Create new input channel.
	listenICMPInputLock.Lock()
	listenICMPInput = make(chan packet.Packet, 10)
	listenICMPEnabled.Set()
	listenICMPInputLock.Unlock()

	return listenICMPInput, func() {
		// Close input channel.
		listenICMPInputLock.Lock()
		listenICMPEnabled.UnSet()
		close(listenICMPInput)
		listenICMPInputLock.Unlock()

		// Release for someone else to use.
		listenICMPLock.Unlock()
	}
}

func SubmitPacketToICMPListener(pkt packet.Packet) (submitted bool) {
	// Hot path.
	if !listenICMPEnabled.IsSet() {
		return false
	}

	// Slow path.
	submitPacketToICMPListenerSlow(pkt)
	return true
}

func submitPacketToICMPListenerSlow(pkt packet.Packet) {
	// Make sure the payload is available.
	if err := pkt.LoadPacketData(); err != nil {
		log.Warningf("netenv: failed to get payload for ICMP listener: %s", err)
		return
	}

	// Send to input channel.
	listenICMPInputLock.Lock()
	defer listenICMPInputLock.Unlock()

	// Check if still enabled.
	if !listenICMPEnabled.IsSet() {
		return
	}

	log.Criticalf("netenv: recvd ICMP packet: %s", pkt)

	// Send to channel, if possible.
	select {
	case listenICMPInput <- pkt:
	default:
		log.Warning("netenv: failed to send packet payload to ICMP listener: channel full")
	}
}
