// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"time"

	"github.com/Safing/portmaster/process"
)

var (
	cleanerTickDuration = 10 * time.Second
	deadLinksTimeout    = 3 * time.Minute
	thresholdDuration   = 3 * time.Minute
)

func cleaner() {
	for {
		time.Sleep(cleanerTickDuration)

		cleanLinks()
		time.Sleep(2 * time.Second)
		cleanComms()
		time.Sleep(2 * time.Second)
		cleanProcesses()
	}
}

func cleanLinks() {
	activeIDs := process.GetActiveConnectionIDs()

	now := time.Now().Unix()
	deleteOlderThan := time.Now().Add(-deadLinksTimeout).Unix()

	// log.Tracef("network.clean: now=%d", now)
	// log.Tracef("network.clean: deleteOlderThan=%d", deleteOlderThan)

	linksLock.RLock()
	defer linksLock.RUnlock()

	var found bool
	for key, link := range links {

		// delete dead links
		if link.Ended > 0 {
			link.Lock()
			deleteThis := link.Ended < deleteOlderThan
			link.Unlock()
			if deleteThis {
				// log.Tracef("network.clean: deleted %s", link.DatabaseKey())
				go link.Delete()
			}

			continue
		}

		// check if link is dead
		found = false
		for _, activeID := range activeIDs {
			if key == activeID {
				found = true
				break
			}
		}

		// mark end time
		if !found {
			link.Ended = now
			// log.Tracef("network.clean: marked %s as ended.", link.DatabaseKey())
			go link.Save()
		}

	}
}

func cleanComms() {
	commsLock.RLock()
	defer commsLock.RUnlock()

	threshold := time.Now().Add(-thresholdDuration).Unix()
	for _, comm := range comms {
		comm.Lock()
		if comm.FirstLinkEstablished < threshold && comm.LinkCount == 0 {
			// log.Tracef("network.clean: deleted %s", comm.DatabaseKey())
			go comm.Delete()
		}
		comm.Unlock()
	}
}

func cleanProcesses() {
	process.CleanProcessStorage(thresholdDuration)
}
