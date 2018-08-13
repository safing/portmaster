// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"time"

	"github.com/Safing/safing-core/process"
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

func markDeadLinks() {
	activeIDs := process.GetActiveConnectionIDs()

	allLinksLock.RLock()
	defer allLinksLock.RUnlock()

	now := time.Now().Unix()
	var found bool
	for key, link := range allLinks {

		// skip dead links
		if link.Ended > 0 {
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

func purgeDeadFor(age time.Duration) {
	connections := make(map[*Connection]bool)
	processes := make(map[*process.Process]bool)

	allLinksLock.Lock()
	defer allLinksLock.Unlock()

	// delete old dead links
	// make a list of connections without links
	ageAgo := time.Now().Add(-1 * age).Unix()
	for key, link := range allLinks {
		if link.Ended != 0 && link.Ended < ageAgo {
			link.Delete()
			delete(allLinks, key)
			_, ok := connections[link.Connection()]
			if !ok {
				connections[link.Connection()] = false
			}
		} else {
			connections[link.Connection()] = true
		}
	}

	// delete connections without links
	// make a list of processes without connections
	for conn, active := range connections {
		if conn != nil {
			if !active {
				conn.Delete()
				_, ok := processes[conn.Process()]
				if !ok {
					processes[conn.Process()] = false
				}
			} else {
				processes[conn.Process()] = true
			}
		}
	}

	// delete processes without connections
	for proc, active := range processes {
		if proc != nil && !active {
			proc.Delete()
		}
	}

}
