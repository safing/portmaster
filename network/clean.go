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

		connsLock.Lock()
		defer connsLock.Unlock()

		for key, conn := range conns {
			// get conn.Ended
			conn.Lock()
			ended := conn.Ended
			conn.Unlock()

			// delete inactive connections
			switch {
			case ended == 0:
				// Step 1: check if still active
				_, ok := activeIDs[key]
				if ok {
					activePIDs[conn.process.Pid] = struct{}{}
				} else {
					// Step 2: mark end
					activePIDs[conn.process.Pid] = struct{}{}
					conn.Lock()
					conn.Ended = now
					conn.Unlock()
					// "save"
					dbController.PushUpdate(conn)
				}
			case ended < deleteOlderThan:
				// Step 3: delete
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

		}

		return nil
	})

	return activePIDs
}
