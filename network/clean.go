package network

import (
	"context"
	"time"

	"github.com/safing/portmaster/network/state"

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
			// clean connections and processes
			activePIDs := cleanConnections()
			process.CleanProcessStorage(activePIDs)

			// clean udp connection states
			state.CleanUDPStates(ctx)
		}
	}
}

func cleanConnections() (activePIDs map[int]struct{}) {
	activePIDs = make(map[int]struct{})

	name := "clean connections" // TODO: change to new fn
	_ = module.RunMediumPriorityMicroTask(&name, func(ctx context.Context) error {

		now := time.Now().UTC()
		nowUnix := now.Unix()
		deleteOlderThan := time.Now().Add(-deleteConnsAfterEndedThreshold).Unix()

		// lock both together because we cannot fully guarantee in which map a connection lands
		// of course every connection should land in the correct map, but this increases resilience
		connsLock.Lock()
		defer connsLock.Unlock()
		dnsConnsLock.Lock()
		defer dnsConnsLock.Unlock()

		// network connections
		for _, conn := range conns {
			conn.Lock()

			// delete inactive connections
			switch {
			case conn.Ended == 0:
				// Step 1: check if still active
				exists := state.Exists(conn.IPVersion, conn.IPProtocol, conn.LocalIP, conn.LocalPort, conn.Entity.IP, conn.Entity.Port, now)
				if exists {
					activePIDs[conn.process.Pid] = struct{}{}
				} else {
					// Step 2: mark end
					activePIDs[conn.process.Pid] = struct{}{}
					conn.Ended = nowUnix
					conn.Save()
				}
			case conn.Ended < deleteOlderThan:
				// Step 3: delete
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

			conn.Unlock()
		}

		// dns requests
		for _, conn := range dnsConns {
			conn.Lock()

			// delete old dns connections
			if conn.Ended < deleteOlderThan {
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

			conn.Unlock()
		}

		return nil
	})

	return activePIDs
}
