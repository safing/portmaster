package ivpn

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/spn/hub"
)

const DNS_LOCKED_DESCRIPTION = "Portmaster controls all DNS resolution on this system, which prevents conflicts with IVPN's DNS settings. To let IVPN manage DNS again, remove all DNS servers from Portmaster's DNS configuration."

// interopBase defines the interface that the InteropIvpn module expects from its owner
// (the main Interoperability module).
type interopBase interface {
	EnsureVerdictHandlerRegistered()
	DnsListenAddress() string
	DnsNameServers() []string
	EvtConfigChange() <-chan struct{}
	GetHookSPNConnecting() *mgr.HookMgr[hub.Announcement]
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
	servicePort   uint16
	vpnConnection vpnConnectionInfo         // VPN server endpoint
	connectedInfo *ivpnclient.ConnectedResp // info about already established VPN connection, if any
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
	extra             platformSpecific // holds platform-specific fields
}

func NewInteropIvpn(owner interopBase) *InteropIvpn {
	return &InteropIvpn{
		owner: owner,
	}
}

func (i *InteropIvpn) getStatus() *clientStatus {
	if old := i.status.Load(); old != nil {
		return old // Preserve existing status fields
	}
	return &clientStatus{}
}

func (i *InteropIvpn) setStatus(status *clientStatus) {
	i.status.Store(status)
}

func (i *InteropIvpn) Start() error {
	i.connHandler = i.owner.Manager().NewWorkerMgr("ivpn client interoperability", i.connectIvpnClient, nil)

	// Subscribe to SPN connecting hook to ensure SPN compatibility rules
	// are applied before SPN connects to the network.
	i.owner.GetHookSPNConnecting().AddHook("ivpn", i.spnConnectingHook)

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

var notifWarnOldVersion atomic.Pointer[notifications.Notification]

// Synchronously connects to the IVPN client, sets up message handlers
func (i *InteropIvpn) connectIvpnClient(wc *mgr.WorkerCtx) error {
	defer func() {
		// Re-enable network-derived location methods when disconnecting from IVPN client, since VPN is no longer active.
		netenv.DisableNetworkDerivedLocation(false)

		// Clear client status on disconnect
		i.setStatus(nil)
		// Reset DNS tracking state
		i.isCustomDnsActive.Store(false)
		// Mark that the first connection attempt is done, even if it failed
		i.setFirstTryDone()

		// Ensure SPN compatibility rules are removed when Portmaster disconnects from IVPN client, either due to shutdown or connection failure.
		i.ensureSPNCompatibility(wc)
	}()

	notifWarn := notifWarnOldVersion.Load()
	if notifWarn != nil {
		notifWarn.Delete()
		notifWarnOldVersion.Store(nil)
	}

	servicePort, _, err := ivpnclient.GetConnectionPortInfo()
	if err != nil {
		return err
	}
	// Save ServicePort.
	status := *i.getStatus()
	status.servicePort = uint16(servicePort)
	i.setStatus(&status)
	// Now we know the service port, we can register the verdict handler
	// to allow accepting connections to the IVPN client service port while the client is connecting.
	// This is needed for case when Portmaster default action is to block unknown connections.
	i.owner.EnsureVerdictHandlerRegistered()

	// Create client.
	// Ignoring error here, since it is expected that the client may not be in running state
	client, err := ivpnclient.NewClientAsRoot(
		nil,
		time.Second*10,
		ivpnclient.ClientInfo{
			Type:    ivpnclient.ClientPortmaster,
			Name:    "Portmaster",
			Version: info.Version()})
	if err != nil {
		return nil
	}

	// Register handler for VPN connection messages
	client.SetMessageEventHandler(ivpnclient.GetTypeName(ivpnclient.ConnectionStarting{}), func(messageName string, messageData string) {
		i.onConnectionStarting(wc, messageName, messageData)
	})
	client.SetMessageEventHandler(ivpnclient.GetTypeName(ivpnclient.ConnectionStopped{}), func(messageName string, messageData string) {
		i.onConnectionStopped(wc, messageName, messageData)
	})
	client.SetMessageEventHandler(ivpnclient.GetTypeName(ivpnclient.ConnectedResp{}), func(messageName string, messageData string) {
		i.onConnectedResp(wc, messageName, messageData)
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
		// IVPN client version > v3.15.1 must provide the service binary path in the hello response.
		wc.Warn(fmt.Sprintf("Detected IVPN Client version '%v' is incompatible. The hello response did not include all required fields.", helloResp.Version))
		notif := i.showNotificationWarnOldVersion()
		notifWarnOldVersion.Store(notif)
		return nil
	}

	// Save ServiceBinary.
	status = *i.getStatus()
	status.serviceBinary = helloResp.ServiceBinary
	i.setStatus(&status)

	// The status.vpnConnection must be already initialized (ConnectionStarting message already received).
	i.setFirstTryDone()
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
			i.ensureSPNCompatibility(wc)
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

// VerdictHandler provides firewall verdicts for IVPN client connections
// based on the current VPN connection status and client binary path.
// It is registered with the firewall module to be called for firewall decisions,
// allowing it to adjust verdicts for better compatibility and user experience with the IVPN client.
func (i *InteropIvpn) VerdictHandler(conn *network.Connection) (verdict network.Verdict, reason string, skipTunnel bool) {
	status := i.status.Load()
	if status == nil {
		return network.VerdictUndecided, "", false
	}

	// Connection to remote VPN server
	if status.vpnConnection.dstPort != 0 {
		if conn.Entity.Port == status.vpnConnection.dstPort &&
			conn.Entity.Protocol == status.vpnConnection.protocol &&
			conn.Entity.IP.Equal(status.vpnConnection.dstAddress) {
			// By default, we accept outbound connections.
			// But UDP traffic is bidirectional, so we also accept inbound connections
			// to avoid breaking incoming UDP responses from the VPN server.
			if !conn.Inbound || status.vpnConnection.protocol == uint8(packet.UDP) {
				return network.VerdictAccept, "IVPN VPN connection", true
			}
		}
	}

	// connections to IVPN service port
	if conn.LocalIPScope == netutils.HostLocal && conn.IPProtocol == packet.TCP {
		if conn.Inbound && conn.LocalPort == status.servicePort {
			return network.VerdictAccept, "IVPN Local Service connection", true
		}
		if !conn.Inbound && conn.Entity.Port == status.servicePort {
			return network.VerdictAccept, "IVPN Local Service connection", true
		}
	}

	// Connections from/to IVPN service (only when serviceBinary initialized)
	if status.serviceBinary != "" && conn.Process().Path == status.serviceBinary {
		return network.VerdictAccept, "IVPN Service connection", true
	}

	return network.VerdictUndecided, "", false
}
