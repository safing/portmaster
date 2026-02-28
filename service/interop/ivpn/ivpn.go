package ivpn

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

const DNS_LOCKED_DESCRIPTION = "Portmaster controls all DNS resolution on this system, which prevents conflicts with IVPN's DNS settings. To let IVPN manage DNS again, remove all DNS servers from Portmaster's DNS configuration."

// interopBase defines the interface that the InteropIvpn module expects from its owner
// (the main Interoperability module).
type interopBase interface {
	EnsureVerdictHandlerRegistered()
	DnsListenAddress() string
	DnsNameServers() []string
	EvtConfigChange() <-chan struct{}
	Interception() *interception.Interception
	Manager() *mgr.Manager
}

// vpnConnectionInfo holds information about the active VPN connection of the IVPN client,
// used for identifying VPN traffic in firewall verdicts.
type vpnConnectionInfo struct {
	dstPort    uint16 // Port of the active VPN connection, used for identifying VPN traffic in firewall verdicts.
	dstAddress net.IP // Destination address of the active VPN connection, used for identifying VPN traffic in firewall verdicts.
	protocol   uint8
}

// clientStatus holds information about the connected IVPN client
// and its current VPN connection status, used for providing context in firewall verdicts.
type clientStatus struct {
	serviceBinary string // Path to the IVPN client binary, used for identifying client connections in firewall verdicts.
	vpnConnection vpnConnectionInfo
}

// InteropIvpn handles interoperability with the IVPN client application,
// allowing Portmaster to receive information about IVPN VPN connections
// and adjust firewall verdicts accordingly for better compatibility and user experience.
type InteropIvpn struct {
	owner       interopBase
	connHandler *mgr.WorkerMgr

	status atomic.Pointer[clientStatus]
}

func NewInteropIvpn(owner interopBase) *InteropIvpn {
	return &InteropIvpn{
		owner: owner,
	}
}

func (i *InteropIvpn) Start() error {
	i.connHandler = i.owner.Manager().NewWorkerMgr("ivpn client interoperability", i.connectIvpnClient, nil)
	i.PingHandler()
	return nil
}

func (i *InteropIvpn) PingHandler() error {
	i.connHandler.Go()
	return nil
}

// Synchronously connects to the IVPN client, sets up message handlers
func (i *InteropIvpn) connectIvpnClient(wc *mgr.WorkerCtx) error {
	ci := ivpnclient.ClientInfo{
		Type:    ivpnclient.ClientPortmaster,
		Name:    "Portmaster",
		Version: info.Version()}

	// Create client.
	client, err := ivpnclient.NewClientAsRoot(nil, time.Second*10, ci)
	if err != nil {
		// do not log this as an error, since it is expected that the client may not be installed
		return nil
	}

	// Register handler for VPN connection messages
	client.SetMessageEventHandler("ConnectionStarting", func(messageName string, messageData string) {
		i.onConnectionStarting(wc, messageName, messageData)
	})
	client.SetMessageEventHandler("ConnectionStopped", func(messageName string, messageData string) {
		i.onConnectionStopped(wc, messageName, messageData)
	})

	// Connect to client.
	err = client.Connect()
	if err != nil {
		// do not log this as an error, since it is expected that the client may not be in running state
		return nil
	}

	// Send hello request, which is required to start receiving messages.
	hello := client.InitHelloRequest()
	hello.GetServiceBinaryPath = true
	hello.GetActiveRemoteEndpoint = true
	var helloResp ivpnclient.HelloResp
	err = client.SendRecv(&hello, &helloResp)
	if err != nil {
		client.Disconnect()
		return err
	}

	// Notify owner that we can now provide verdicts for firewall module
	i.owner.EnsureVerdictHandlerRegistered()

	wc.Debug(fmt.Sprintf("Connected to IVPN client %s", helloResp.Version))

	// Store IVPN client binary path
	i.status.Store(&clientStatus{serviceBinary: helloResp.ServiceBinary})
	// Clear status on disconnect to avoid stale info in firewall verdicts
	defer i.status.Store(nil)

	// Configure IVPN client DNS settings based on current Portmaster config at startup
	customDnsActive := i.updateIvpnClientDnsSettings(wc, client, false)

	interceptionStatus := i.owner.Interception().EventStartStopState.Subscribe("ivpn", 10)
	defer interceptionStatus.Cancel()

	done := false
	for !done {
		select {
		case <-interceptionStatus.Events():
			customDnsActive = i.updateIvpnClientDnsSettings(wc, client, customDnsActive)
		case <-i.owner.EvtConfigChange():
			customDnsActive = i.updateIvpnClientDnsSettings(wc, client, customDnsActive)
		case <-wc.Done():
			client.Disconnect()
			done = true
		case <-client.Disconnected():
			done = true
		}
	}

	wc.Debug("IVPN client disconnected")
	return nil
}

// updateIvpnClientDnsSettings configures the IVPN client to use Portmaster's local DNS resolver if custom DNS servers are configured in Portmaster.
// Parameter:
//   - customDnsActive: Whether the IVPN client is currently configured to use custom DNS.
//     It is a cached value that should be updated with the new state after attempting to update the settings,
//     and is used to avoid unnecessary updates if the desired state is already active.
//
// Returns:
//   - The new state of whether custom DNS is active in the IVPN client after attempting to update the settings.
//     (New value of customDnsActive should be used by the caller to keep track of the current state).
func (i *InteropIvpn) updateIvpnClientDnsSettings(wc *mgr.WorkerCtx, client *ivpnclient.Client, customDnsActive bool) (retCustomDnsActive bool) {
	// Custom DNS should be applied only if there are any custom DNS servers configured in Portmaster,
	// and interception is active (PM not in Paused state).
	wantCustomDns := len(i.owner.DnsNameServers()) > 0 && i.owner.Interception().IsStarted()
	if wantCustomDns == customDnsActive {
		return customDnsActive // Already in the desired state, nothing to do.
	}

	if wantCustomDns {
		// Configure IVPN to use Portmaster's local DNS resolver.
		// This prevents IVPN from modifying system DNS, letting Portmaster handle all DNS requests.
		customDns := ivpnclient.DnsSettings{Servers: []ivpnclient.DnsServerConfig{{Address: i.owner.DnsListenAddress()}}}
		if err := client.SetTempPrioritizedDns(customDns, DNS_LOCKED_DESCRIPTION); err != nil {
			wc.Warn(fmt.Sprintf("IVPN: Failed to set manual DNS: %v", err))
			return customDnsActive // Preserve last known state; will retry on next config change.
		}
		wc.Debug(fmt.Sprintf("IVPN: Manual DNS set successfully to %q", i.owner.DnsListenAddress()))
		return true
	}

	// Reset IVPN back to its default DNS handling.
	if err := client.SetTempPrioritizedDns(ivpnclient.DnsSettings{}, ""); err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to restore manual DNS: %v", err))
		return customDnsActive // Preserve last known state; will retry on next config change.
	}
	wc.Debug("IVPN: Manual DNS restored successfully")
	return false
}

// notification handler: VPN connection is going to start
func (i *InteropIvpn) onConnectionStarting(wc *mgr.WorkerCtx, _ string, messageData string) {
	info := ivpnclient.ConnectionStarting{}
	err := json.Unmarshal([]byte(messageData), &info)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to parse ConnectionStarting message: %v", err))
		return
	}

	// Update/Store VPN connection info for use in firewall verdicts
	status := clientStatus{}
	if old := i.status.Load(); old != nil {
		status = *old // Preserve existing status fields
	}
	conn := vpnConnectionInfo{
		dstPort:    info.Port,
		dstAddress: net.ParseIP(info.Address),
		protocol:   info.Protocol,
	}
	if conn.dstAddress == nil {
		conn.dstPort = 0 // Invalidate port if address is invalid, to avoid false matches in firewall verdicts.
	}
	status.vpnConnection = conn
	i.status.Store(&status)

	wc.Debug(fmt.Sprintf("IVPN: VPN connection starting %s:%d %s", info.Address, info.Port, packet.IPProtocol(info.Protocol)))
}

// notification handler: VPN connection stopped
func (i *InteropIvpn) onConnectionStopped(wc *mgr.WorkerCtx, _ string, messageData string) {
	i.resetVpnConnectionInfo()
	wc.Debug("IVPN: VPN connection stopped")
}

func (i *InteropIvpn) setServiceBinary(serviceBinary string) {
	status := clientStatus{}
	if old := i.status.Load(); old != nil {
		status = *old // Preserve existing status fields
	}
	status.serviceBinary = serviceBinary
	i.status.Store(&status)
}

func (i *InteropIvpn) resetVpnConnectionInfo() {
	status := clientStatus{}
	if old := i.status.Load(); old != nil {
		status = *old // copy before mutating
	}
	status.vpnConnection = vpnConnectionInfo{}
	i.status.Store(&status)
}

// VerdictHandler provides firewall verdicts for IVPN client connections
// based on the current VPN connection status and client binary path.
// It is registered with the firewall module to be called for firewall decisions,
// allowing it to adjust verdicts for better compatibility and user experience with the IVPN client.
func (i *InteropIvpn) VerdictHandler(conn *network.Connection) (network.Verdict, string, bool) {
	status := i.status.Load()
	if status == nil {
		return network.VerdictUndecided, "", false
	}

	if status.vpnConnection.dstPort != 0 {
		if !conn.Inbound &&
			conn.Entity.Port == status.vpnConnection.dstPort &&
			conn.Entity.Protocol == status.vpnConnection.protocol &&
			conn.Entity.IP.Equal(status.vpnConnection.dstAddress) {

			return network.VerdictAccept, "IVPN VPN connection", false
		}
	}

	if status.serviceBinary != "" && conn.Process().Path == status.serviceBinary {
		return network.VerdictAccept, "IVPN Service connection", false
	}

	return network.VerdictUndecided, "", false
}
