package ivpn

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/notifications"
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
	dstPort    uint16
	dstAddress net.IP
	protocol   uint8
}

// clientStatus holds information about the connected IVPN client
// and its current VPN connection status, used for providing context in firewall verdicts.
type clientStatus struct {
	serviceBinary string
	vpnConnection vpnConnectionInfo
}

// InteropIvpn handles interoperability with the IVPN client application,
// allowing Portmaster to receive information about IVPN VPN connections
// and adjust firewall verdicts accordingly for better compatibility and user experience.
type InteropIvpn struct {
	owner             interopBase
	connHandler       *mgr.WorkerMgr
	status            atomic.Pointer[clientStatus]
	isCustomDnsActive atomic.Bool
	firstTryDone      atomic.Pointer[chan struct{}]
}

func NewInteropIvpn(owner interopBase) *InteropIvpn {
	return &InteropIvpn{
		owner: owner,
	}
}

func (i *InteropIvpn) Start() error {
	i.connHandler = i.owner.Manager().NewWorkerMgr("ivpn client interoperability", i.connectIvpnClient, nil)

	firstTryChan := make(chan struct{})
	i.firstTryDone.Store(&firstTryChan)

	i.connHandler.Go()

	// Wait for the first connection attempt to complete.
	// The 'status' must be initialized before allowing firewall verdicts to proceed.
	// The wait is bounded by the connection timeouts inside connectIvpnClient.
	<-firstTryChan

	return nil
}

func (i *InteropIvpn) PingHandler() error {
	i.connHandler.Go()
	return nil
}

func (i *InteropIvpn) setFirstTryDone() {
	if chanPtr := i.firstTryDone.Load(); chanPtr != nil {
		close(*chanPtr)
		i.firstTryDone.Store(nil)
	}
}

func (i *InteropIvpn) setServiceBinary(path string) {
	status := clientStatus{}
	if old := i.status.Load(); old != nil {
		status = *old
	}
	status.serviceBinary = path
	i.status.Store(&status)
}

var notifWarnOldVersion atomic.Pointer[notifications.Notification]

// Synchronously connects to the IVPN client, sets up message handlers
func (i *InteropIvpn) connectIvpnClient(wc *mgr.WorkerCtx) error {
	defer func() {
		// Clear client status on disconnect
		i.status.Store(nil)
		// Reset DNS tracking state
		i.isCustomDnsActive.Store(false)
		// Mark that the first connection attempt is done, even if it failed
		i.setFirstTryDone()
	}()

	notifWarn := notifWarnOldVersion.Load()
	if notifWarn != nil {
		notifWarn.Delete()
		notifWarnOldVersion.Store(nil)
	}

	ci := ivpnclient.ClientInfo{
		Type:    ivpnclient.ClientPortmaster,
		Name:    "Portmaster",
		Version: info.Version()}

	// Create client.
	// Ignoring error here, since it is expected that the client may not be in running state
	client, err := ivpnclient.NewClientAsRoot(nil, time.Second*10, ci)
	if err != nil {
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
	// Ignoring error here, since it is expected that the client may not be in running state
	err = client.Connect(time.Second * 2)
	if err != nil {
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

	if helloResp.ServiceBinary == "" {
		// IVPN client version > v3.15.0 must provide the service binary path in the hello response.
		wc.Warn(fmt.Sprintf("Detected IVPN Client version '%v' is incompatible. The hello response did not include all required fields.", helloResp.Version))
		notif := i.showNotificationWarnOldVersion()
		notifWarnOldVersion.Store(notif)
		return nil
	}

	// Save ServiceBinary.
	i.setServiceBinary(helloResp.ServiceBinary)
	// The status.vpnConnection must be already initialized (ConnectionStarting message already received).
	i.setFirstTryDone()
	// Notify owner that we can now provide verdicts for firewall module
	i.owner.EnsureVerdictHandlerRegistered()

	wc.Debug(fmt.Sprintf("Connected to IVPN client %s", helloResp.Version))

	// Show UI notification if not suppressed by user
	if !isNotificationSuppressed() {
		notification := i.initAndShowNotification()
		defer notification.Delete()
	}

	// Configure IVPN client DNS settings based on current Portmaster config at startup
	i.updateIvpnClientDnsSettings(wc, client)

	// Subscribe to interception start/stop events to update IVPN client DNS settings
	interceptionStatus := i.owner.Interception().EventStartStopState.Subscribe("ivpn", 10)
	defer interceptionStatus.Cancel()

	done := false
	for !done {
		select {
		case <-interceptionStatus.Events():
			i.updateIvpnClientDnsSettings(wc, client)
		case <-i.owner.EvtConfigChange():
			i.updateIvpnClientDnsSettings(wc, client)
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

// updateIvpnClientDnsSettings configures the IVPN client to use Portmaster's local DNS resolver
// when custom DNS servers are configured in Portmaster and interception is active.
func (i *InteropIvpn) updateIvpnClientDnsSettings(wc *mgr.WorkerCtx, client *ivpnclient.Client) {
	// Custom DNS should be applied only if there are any custom DNS servers configured in Portmaster,
	// and interception is active (PM not in Paused state).
	wantCustomDns := len(i.owner.DnsNameServers()) > 0 && i.owner.Interception().IsStarted()
	if wantCustomDns == i.isCustomDnsActive.Load() {
		return
	}

	if wantCustomDns {
		// Configure IVPN to use Portmaster's local DNS resolver.
		// This prevents IVPN from modifying system DNS, letting Portmaster handle all DNS requests.
		customDns := ivpnclient.DnsSettings{Servers: []ivpnclient.DnsServerConfig{{Address: i.owner.DnsListenAddress()}}}
		if err := client.SetTempPrioritizedDns(customDns, DNS_LOCKED_DESCRIPTION); err != nil {
			wc.Warn(fmt.Sprintf("IVPN: Failed to set manual DNS: %v", err))
			return
		}
		wc.Debug(fmt.Sprintf("IVPN: Manual DNS set successfully to %q", i.owner.DnsListenAddress()))
		i.isCustomDnsActive.Store(true)
		return
	}

	// Reset IVPN back to its default DNS handling.
	if err := client.SetTempPrioritizedDns(ivpnclient.DnsSettings{}, ""); err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to restore manual DNS: %v", err))
		return
	}

	i.isCustomDnsActive.Store(false)
	wc.Debug("IVPN: Manual DNS restored successfully")
}

// notification handler: VPN connection is going to start
func (i *InteropIvpn) onConnectionStarting(wc *mgr.WorkerCtx, _ string, messageData string) {
	connInfo := ivpnclient.ConnectionStarting{}
	err := json.Unmarshal([]byte(messageData), &connInfo)
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
		dstPort:    connInfo.Port,
		dstAddress: net.ParseIP(connInfo.Address),
		protocol:   connInfo.Protocol,
	}
	if conn.dstAddress == nil {
		conn.dstPort = 0 // Invalidate port if address is invalid, to avoid false matches in firewall verdicts.
	}
	status.vpnConnection = conn
	i.status.Store(&status)

	wc.Debug(fmt.Sprintf("IVPN: VPN connection starting %s:%d %s", connInfo.Address, connInfo.Port, packet.IPProtocol(connInfo.Protocol)))
}

// notification handler: VPN connection stopped
func (i *InteropIvpn) onConnectionStopped(wc *mgr.WorkerCtx, _ string, messageData string) {
	status := clientStatus{}
	if old := i.status.Load(); old != nil {
		status = *old
	}
	status.vpnConnection = vpnConnectionInfo{}
	i.status.Store(&status)
	wc.Debug("IVPN: VPN connection stopped")
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
		if conn.Entity.Port == status.vpnConnection.dstPort &&
			conn.Entity.Protocol == status.vpnConnection.protocol &&
			conn.Entity.IP.Equal(status.vpnConnection.dstAddress) {
			// By default, we accept outbound connections.
			// But UDP traffic is bidirectional, so we also accept inbound connections
			// to avoid breaking incoming UDP responses from the VPN server.
			if !conn.Inbound || status.vpnConnection.protocol == uint8(packet.UDP) {
				return network.VerdictAccept, "IVPN VPN connection", false
			}
		}
	}

	if status.serviceBinary != "" && conn.Process().Path == status.serviceBinary {
		return network.VerdictAccept, "IVPN Service connection", false
	}

	return network.VerdictUndecided, "", false
}
