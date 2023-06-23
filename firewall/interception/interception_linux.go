package interception

import (
	// bandwidth "github.com/safing/portmaster/firewall/interception/ebpf/bandwidth"
	conn_listener "github.com/safing/portmaster/firewall/interception/ebpf/connection_listener"
	"github.com/safing/portmaster/firewall/interception/nfq"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

// start starts the interception.
func start(ch chan packet.Packet) error {
	// Start ebpf new connection listener
	conn_listener.StartEBPFWorker(ch)
	// Start ebpf bandwidth listener
	// bandwidth.SetupBandwidthInterface()
	return StartNfqueueInterception(ch)
}

// stop starts the interception.
func stop() error {
	// Stop ebpf connection listener
	conn_listener.StopEBPFWorker()
	// Stop ebpf bandwidth listener
	// bandwidth.ShutdownBandwithInterface()
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
