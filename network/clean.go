// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"time"

	"github.com/Safing/portmaster/process"
)

var (
	cleanerTickDuration = 1 * time.Minute
	deadLinksTimeout    = 5 * time.Minute
	thresholdDuration   = 1 * time.Minute
)

func cleaner() {
	for {
		time.Sleep(cleanerTickDuration)

		cleanLinks()
		time.Sleep(10 * time.Second)
		cleanConnections()
		time.Sleep(10 * time.Second)
		cleanProcesses()
	}
}

func cleanLinks() {
	activeIDs := process.GetActiveConnectionIDs()

	now := time.Now().Unix()
	deleteOlderThan := time.Now().Add(-deadLinksTimeout).Unix()

	linksLock.RLock()
	defer linksLock.RUnlock()

	var found bool
	for key, link := range links {

		// delete dead links
		link.Lock()
		deleteThis := link.Ended > 0 && link.Ended < deleteOlderThan
		link.Unlock()
		if deleteThis {
			go link.Delete()
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
			go link.Save()
		}

	}
}

func cleanConnections() {
	connectionsLock.RLock()
	defer connectionsLock.RUnlock()

	threshold := time.Now().Add(-thresholdDuration).Unix()
	for _, conn := range connections {
		conn.Lock()
		if conn.FirstLinkEstablished < threshold && conn.LinkCount == 0 {
			go conn.Delete()
		}
		conn.Unlock()
	}
}

func cleanProcesses() {
	process.CleanProcessStorage(thresholdDuration)
}
