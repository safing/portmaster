package interception

import (
	"context"
	"time"

	bandwidth "github.com/safing/portmaster/firewall/interception/ebpf/bandwidth"
	conn_listener "github.com/safing/portmaster/firewall/interception/ebpf/connection_listener"
	"github.com/safing/portmaster/firewall/interception/nfq"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

// start starts the interception.
func startInterception(packets chan packet.Packet) error {
	// Start packet interception via nfqueue.
	err := StartNfqueueInterception(packets)
	if err != nil {
		return err
	}

	// Start ebpf new connection listener.
	module.StartServiceWorker("ebpf connection listener", 0, func(ctx context.Context) error {
		return conn_listener.ConnectionListenerWorker(ctx, packets)
	})

	// Start ebpf bandwidth stats monitor.
	module.StartServiceWorker("ebpf bandwidth stats monitor", 0, func(ctx context.Context) error {
		return bandwidth.BandwidthStatsWorker(ctx, 1*time.Second, BandwidthUpdates)
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
