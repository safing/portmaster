package interception

import (
	"time"

	bandwidth "github.com/safing/portmaster/service/firewall/interception/ebpf/bandwidth"
	conn_listener "github.com/safing/portmaster/service/firewall/interception/ebpf/connection_listener"
	"github.com/safing/portmaster/service/firewall/interception/nfq"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// start starts the interception.
func startInterception(packets chan packet.Packet) error {
	// Start packet interception via nfqueue.
	err := StartNfqueueInterception(packets)
	if err != nil {
		return err
	}

	// Start ebpf new connection listener.
	module.mgr.Go("ebpf connection listener", func(wc *mgr.WorkerCtx) error {
		return conn_listener.ConnectionListenerWorker(wc.Ctx(), packets)
	})

	// Start ebpf bandwidth stats monitor.
	module.mgr.Go("ebpf bandwidth stats monitor", func(wc *mgr.WorkerCtx) error {
		return bandwidth.BandwidthStatsWorker(wc.Ctx(), 1*time.Second, BandwidthUpdates)
	})

	return nil
}

// stop starts the interception.
func stopInterception() error {
	return StopNfqueueInterception()
}

// ResetVerdictOfAllConnections resets all connections so they are forced to go thought the firewall again.
func ResetVerdictOfAllConnections() error {
	return nfq.DeleteAllMarkedConnection()
}

// UpdateVerdictOfConnection deletes the verdict of the given connection so it can be initialized again with the next packet.
func UpdateVerdictOfConnection(conn *network.Connection) error {
	return nfq.DeleteMarkedConnection(conn)
}
