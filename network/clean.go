// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"time"

	"github.com/Safing/portmaster/process"
)

var (
	deadLinksTimeout  = 5 * time.Minute
	thresholdDuration = 1 * time.Minute
)

func init() {
	go cleaner()
}

func cleaner() {
	time.Sleep(15 * time.Second)
	for {
		markDeadLinks()
		purgeDeadFor(5 * time.Minute)
		time.Sleep(15 * time.Second)
	}
}

func cleanLinks() {
	activeIDs := process.GetActiveConnectionIDs()

	dataLock.Lock()
	defer dataLock.Lock()

	now := time.Now().Unix()
	deleteOlderThan := time.Now().Add(-deadLinksTimeout).Unix()

	var found bool
	for key, link := range links {

		// delete dead links
		if link.Ended > 0 && link.Ended < deleteOlderThan {
			link.Delete()
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
			link.Save()
		}

	}
}

func cleanConnections() {
	dataLock.Lock()
	defer dataLock.Lock()

	threshold := time.Now().Add(-thresholdDuration).Unix()
	for _, conn := range connections {
		if conn.FirstLinkEstablished < threshold && conn.LinkCount == 0 {
			conn.Delete()
		}
	}
}

func cleanProcesses() {
	process.CleanProcessStorage(thresholdDuration)
}
