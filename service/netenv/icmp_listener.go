package netenv

import (
	"net"
	"sync"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
)

/*
This ICMP listening system is a simple system for components to listen to ICMP
packets via the firewall.

The main use case for this is to receive ICMP packets that are not always
delivered correctly, or need special permissions and or sockets to receive
them. This is the case when doing a traceroute.

In order to keep it simple, the system is only designed to be used by one
"user" at a time. Further calls to ListenToICMP will wait for the previous
operation to complete.
*/

var (
	// listenICMPLock locks the ICMP listening system for one user at a time.
	listenICMPLock sync.Mutex

	// listenICMPEnabled defines whether or not the firewall should submit ICMP
	// packets to this interface.
	listenICMPEnabled = abool.New()

	// listenICMPInput is created for every use of the ICMP listenting system.
	listenICMPInput         chan packet.Packet
	listenICMPInputTargetIP net.IP
	listenICMPInputLock     sync.Mutex
)

// ListenToICMP returns a new channel for listenting to icmp packets. Please
// note that any icmp packet will be passed and filtering must be done on
// the side of the caller. The caller must call the returned done function when
// done with the listener.
func ListenToICMP(targetIP net.IP) (packets chan packet.Packet, done func()) {
	// Lock for single use.
	listenICMPLock.Lock()

	// Create new input channel.
	listenICMPInputLock.Lock()
	listenICMPInput = make(chan packet.Packet, 100)
	listenICMPInputTargetIP = targetIP
	listenICMPEnabled.Set()
	listenICMPInputLock.Unlock()

	return listenICMPInput, func() {
		// Release for someone else to use.
		defer listenICMPLock.Unlock()

		// Close input channel.
		listenICMPInputLock.Lock()
		listenICMPEnabled.UnSet()
		listenICMPInputTargetIP = nil
		close(listenICMPInput)
		listenICMPInputLock.Unlock()
	}
}

// SubmitPacketToICMPListener checks if an ICMP packet should be submitted to
// the listener. If so, it is submitted right away. The function returns
// whether or not the packet should be submitted, not if it was successful.
func SubmitPacketToICMPListener(pkt packet.Packet) (submitted bool) {
	// Hot path.
	if !listenICMPEnabled.IsSet() {
		return false
	}

	// Slow path.
	return submitPacketToICMPListenerSlow(pkt)
}

func submitPacketToICMPListenerSlow(pkt packet.Packet) (submitted bool) {
	// Make sure the payload is available.
	if err := pkt.LoadPacketData(); err != nil {
		log.Warningf("netenv: failed to get payload for ICMP listener: %s", err)
		return false
	}

	// Send to input channel.
	listenICMPInputLock.Lock()
	defer listenICMPInputLock.Unlock()

	// Check if still enabled.
	if !listenICMPEnabled.IsSet() {
		return false
	}

	// Only listen for outbound packets to the target IP.
	if pkt.IsOutbound() &&
		listenICMPInputTargetIP != nil &&
		!pkt.Info().Dst.Equal(listenICMPInputTargetIP) {
		return false
	}

	// Send to channel, if possible.
	select {
	case listenICMPInput <- pkt:
	default:
		log.Warning("netenv: failed to send packet payload to ICMP listener: channel full")
	}
	return true
}
