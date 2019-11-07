package firewall

import (
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/rng"
)

type portStatus struct {
	lastSeen time.Time
	isMe     bool
}

var (
	portsInUse     = make(map[uint16]*portStatus)
	portsInUseLock sync.Mutex

	cleanerTickDuration = 10 * time.Second
	cleanTimeout        = 10 * time.Minute
)

func getPortStatusAndMarkUsed(port uint16) *portStatus {
	portsInUseLock.Lock()
	defer portsInUseLock.Unlock()

	ps, ok := portsInUse[port]
	if ok {
		ps.lastSeen = time.Now()
		return ps
	}

	new := &portStatus{
		lastSeen: time.Now(),
		isMe:     false,
	}
	portsInUse[port] = new
	return new
}

// GetPermittedPort returns a local port number that is already permitted for communication.
// This bypasses the process attribution step to guarantee connectivity.
// Communication on the returned port is attributed to the Portmaster.
func GetPermittedPort() uint16 {
	portsInUseLock.Lock()
	defer portsInUseLock.Unlock()

	for i := 0; i < 1000; i++ {
		// generate port between 10000 and 65535
		rN, err := rng.Number(55535)
		if err != nil {
			log.Warningf("firewall: failed to generate random port: %s", err)
			return 0
		}
		port := uint16(rN + 10000)

		// check if free, return if it is
		_, ok := portsInUse[port]
		if !ok {
			portsInUse[port] = &portStatus{
				lastSeen: time.Now(),
				isMe:     true,
			}
			return port
		}
	}

	return 0
}

func portsInUseCleaner() {
	for {
		time.Sleep(cleanerTickDuration)
		cleanPortsInUse()
	}
}

func cleanPortsInUse() {
	portsInUseLock.Lock()
	defer portsInUseLock.Unlock()

	threshold := time.Now().Add(-cleanTimeout)

	for port, status := range portsInUse {
		if status.lastSeen.Before(threshold) {
			delete(portsInUse, port)
		}
	}
}
