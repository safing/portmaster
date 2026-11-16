package network

import (
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
)

// unusablePortTTL is how long a port that could not be bound is skipped.
const unusablePortTTL = 1 * time.Hour

var (
	unusablePorts     = make(map[uint32]time.Time)
	unusablePortsLock sync.Mutex
)

// MarkPortUnusable remembers that the given local port could not be bound,
// e.g. because the OS reserved it. GetUnusedLocalPort skips the port until
// the entry expires.
func MarkPortUnusable(protocol uint8, port uint16) {
	unusablePortsLock.Lock()
	defer unusablePortsLock.Unlock()

	unusablePorts[unusablePortKey(protocol, port)] = time.Now().Add(unusablePortTTL)
}

// portIsKnownUnusable reports whether the given port was marked as unusable
// and the entry has not expired yet.
func portIsKnownUnusable(protocol uint8, port uint16) bool {
	unusablePortsLock.Lock()
	defer unusablePortsLock.Unlock()

	key := unusablePortKey(protocol, port)
	expires, known := unusablePorts[key]
	if !known {
		return false
	}
	if time.Now().After(expires) {
		delete(unusablePorts, key)
		return false
	}
	return true
}

func unusablePortKey(protocol uint8, port uint16) uint32 {
	return uint32(protocol)<<16 | uint32(port)
}

// GetUnusedLocalPort returns a local port of the specified protocol that is
// currently unused and is unlikely to be used within the next seconds.
func GetUnusedLocalPort(protocol uint8) (port uint16, ok bool) {
	allConns := conns.clone()
	tries := 1000

	// Try up to 1000 times to find an unused port.
nextPort:
	for i := range tries {
		// Generate random port between 10000 and 65535
		rN, err := rng.Number(55535)
		if err != nil {
			log.Warningf("network: failed to generate random port: %s", err)
			return 0, false
		}
		port := uint16(rN + 10000)

		// Shrink range when we chew through the tries.
		portRangeStart := port - 10

		// Skip ports that could not be bound recently.
		if portIsKnownUnusable(protocol, port) {
			continue nextPort
		}

		// Check if the generated port is unused.
	nextConnection:
		for _, conn := range allConns {
			switch {
			case !conn.DataIsComplete():
				// Skip connection if the data is not complete.
				continue nextConnection

			case conn.Entity.Protocol != protocol:
				// Skip connection if the protocol does not match the protocol of interest.
				continue nextConnection

			case conn.LocalPort <= port && conn.LocalPort >= portRangeStart:
				// Skip port if the local port is in dangerous proximity.
				// Consecutive port numbers are very common.
				continue nextPort
			}
		}

		// Log if it took more than 10 attempts.
		if i >= 10 {
			log.Warningf("network: took %d attempts to find a suitable unused port for pre-auth", i+1)
		}

		// The checks have passed. We have found a good unused port.
		return port, true
	}

	return 0, false
}
