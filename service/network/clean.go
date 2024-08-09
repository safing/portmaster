package network

import (
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/state"
	"github.com/safing/portmaster/service/process"
)

const (
	// EndConnsAfterInactiveFor defines the amount of time after not seen
	// connections of unsupported protocols are marked as ended.
	EndConnsAfterInactiveFor = 5 * time.Minute

	// EndICMPConnsAfterInactiveFor defines the amount of time after not seen
	// ICMP "connections" are marked as ended.
	EndICMPConnsAfterInactiveFor = 1 * time.Minute

	// DeleteConnsAfterEndedThreshold defines the amount of time after which
	// ended connections should be removed from the internal connection state.
	DeleteConnsAfterEndedThreshold = 10 * time.Minute

	// DeleteIncompleteConnsAfterStartedThreshold defines the amount of time after
	// which incomplete connections should be removed from the internal
	// connection state.
	DeleteIncompleteConnsAfterStartedThreshold = 1 * time.Minute

	cleanerTickDuration = 5 * time.Second
)

func connectionCleaner(ctx *mgr.WorkerCtx) error {
	module.connectionCleanerTicker = mgr.NewSleepyTicker(cleanerTickDuration, 0)
	defer module.connectionCleanerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-module.connectionCleanerTicker.Wait():
			// clean connections and processes
			activePIDs := cleanConnections()
			process.CleanProcessStorage(activePIDs)

			// clean udp connection states
			state.CleanUDPStates(ctx.Ctx())
		}
	}
}

func cleanConnections() (activePIDs map[int]struct{}) {
	activePIDs = make(map[int]struct{})

	_ = module.mgr.Do("clean connections", func(ctx *mgr.WorkerCtx) error {
		now := time.Now().UTC()
		nowUnix := now.Unix()
		ignoreNewer := nowUnix - 2
		endNotSeenSince := now.Add(-EndConnsAfterInactiveFor).Unix()
		endICMPNotSeenSince := now.Add(-EndICMPConnsAfterInactiveFor).Unix()
		deleteOlderThan := now.Add(-DeleteConnsAfterEndedThreshold).Unix()
		deleteIncompleteOlderThan := now.Add(-DeleteIncompleteConnsAfterStartedThreshold).Unix()

		// network connections
		for _, conn := range conns.clone() {
			conn.Lock()

			// delete inactive connections
			switch {
			case conn.Started >= ignoreNewer:
				// Skip very fresh connections to evade edge cases.
			case !conn.DataIsComplete():
				// Step 0: delete old incomplete connections
				if conn.Started < deleteIncompleteOlderThan {
					// Stop the firewall handler, in case one is running.
					conn.StopFirewallHandler()
					// Remove connection from state.
					conn.delete()
				}

			case conn.Ended == 0:
				// Step 1: check if still active
				var connActive bool
				switch conn.IPProtocol { //nolint:exhaustive
				case packet.TCP, packet.UDP:
					connActive = state.Exists(&packet.Info{
						Inbound:  false, // src == local
						Version:  conn.IPVersion,
						Protocol: conn.IPProtocol,
						Src:      conn.LocalIP,
						SrcPort:  conn.LocalPort,
						Dst:      conn.Entity.IP,
						DstPort:  conn.Entity.Port,
						PID:      process.UndefinedProcessID,
						SeenAt:   time.Unix(conn.Started, 0), // State tables will be updated if older than this.
					}, now)
					// Update last seen value for permanent verdict connections.
					if connActive && conn.VerdictPermanent {
						conn.lastSeen.Store(nowUnix)
					}

				case packet.ICMP, packet.ICMPv6:
					connActive = conn.lastSeen.Load() > endICMPNotSeenSince

				default:
					connActive = conn.lastSeen.Load() > endNotSeenSince
				}

				// Step 2: mark as ended
				if !connActive {
					conn.Ended = nowUnix

					// Stop the firewall handler, in case one is running.
					conn.StopFirewallHandler()

					// Save to database.
					conn.Save()
				}

				// If the connection has an associated process, add its PID to the active PID list.
				if conn.process != nil {
					activePIDs[conn.process.Pid] = struct{}{}
				}

			case conn.Ended < deleteOlderThan:
				// Step 3: delete
				// DEBUG:
				// log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))

				// Remove connection from state.
				conn.delete()
			}

			conn.Unlock()
		}

		// dns requests
		for _, conn := range dnsConns.clone() {
			conn.Lock()

			// delete old dns connections
			if conn.Ended < deleteOlderThan {
				log.Tracef("network.clean: deleted %s (ended at %s)", conn.DatabaseKey(), time.Unix(conn.Ended, 0))
				conn.delete()
			}

			conn.Unlock()
		}

		// rerouted dns requests
		cleanDNSRequestConnections()

		return nil
	})

	return activePIDs
}
