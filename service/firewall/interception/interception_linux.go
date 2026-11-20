package interception

import (
	"net"
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

	// Make apps resolve names again, so that Portmaster sees the queries.
	flushSystemDNSCache()

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

// ClearFlow deletes the conntrack entry of an earlier flow with the given
// original addresses, so that a new socket with the same addresses is not
// handled like it, e.g. redirected.
func ClearFlow(protocol uint8, localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16) (cleared bool, err error) {
	return nfq.DeleteFlow(protocol, localIP, localPort, remoteIP, remotePort)
}

// UpdateVerdictOfConnection deletes the verdict of the given connection so it can be initialized again with the next packet.
func UpdateVerdictOfConnection(conn *network.Connection) error {
	return nfq.DeleteMarkedConnection(conn)
}
