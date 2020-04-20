package network

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/process"
)

var (
	cleanerTickDuration            = 5 * time.Second
	deleteConnsAfterEndedThreshold = 5 * time.Minute
)

func connectionCleaner(ctx context.Context) error {
	ticker := time.NewTicker(cleanerTickDuration)

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return nil
		case <-ticker.C:
			activePIDs := cleanConnections()
			process.CleanProcessStorage(activePIDs)
		}
	}
}

func cleanConnections() (activePIDs map[int]struct{}) {
	activePIDs = make(map[int]struct{})

	name := "clean connections" // TODO: change to new fn
	_ = module.RunMediumPriorityMicroTask(&name, func(ctx context.Context) error {
		activeIDs := make(map[string]struct{})
		for _, cID := range process.GetActiveConnectionIDs() {
			activeIDs[cID] = struct{}{}
		}

		now := time.Now().Unix()
		deleteOlderThan := time.Now().Add(-deleteConnsAfterEndedThreshold).Unix()

		// network connections
		connsLock.Lock()
		for key, conn := range conns {
			conn.Lock()

			// delete inactive connections
			switch {
			case conn.Ended == 0:
				// Step 1: check if still active
				_, ok := activeIDs[key]
				if ok {
					activePIDs[conn.process.Pid] = struct{}{}
				} else {
					// Step 2: mark end
					activePIDs[conn.process.Pid] = struct{}{}
					conn.Ended = now
					conn.Save()
				}
			case conn.Ended < deleteOlderThan:
				// Step 3: delete
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

			conn.Unlock()
		}
		connsLock.Unlock()

		// dns requests
		dnsConnsLock.Lock()
		for _, conn := range dnsConns {
			conn.Lock()

			// delete old dns connections
			if conn.Ended < deleteOlderThan {
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

			conn.Unlock()
		}
		dnsConnsLock.Unlock()

		return nil
	})

	return activePIDs
}
