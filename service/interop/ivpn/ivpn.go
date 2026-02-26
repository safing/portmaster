package ivpn

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

const DNS_LOCKED_DESCRIPTION = "Portmaster controls all DNS resolution on this system, which prevents conflicts with IVPN's DNS settings. To let IVPN manage DNS again, remove all DNS servers from Portmaster's DNS configuration."

type interopBase interface {
	DnsListenAddress() string
	DnsNameServers() []string
	EvtConfigChange() <-chan struct{}
	Manager() *mgr.Manager
}

type InteropIvpn struct {
	owner       interopBase
	connHandler *mgr.WorkerMgr

	locker        sync.RWMutex
	serviceBinary string
	dstPort       uint16
	dstAddress    net.IP
	protocol      uint8
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
	var helloResp ivpnclient.HelloResp
	err = client.SendRecv(&hello, &helloResp)
	if err != nil {
		client.Disconnect()
		return err
	}
	wc.Debug(fmt.Sprintf("Connected to IVPN client %s", helloResp.Version))

	// Store IVPN client binary path for use in firewall verdict reasons.
	i.setServiceBinary(helloResp.ServiceBinary)
	defer i.setServiceBinary("") // do not leave stale binary path if client disconnects

	// Configure IVPN client DNS settings based on current Portmaster config at startup
	customDnsActive := i.updateIvpnClientDnsSettings(wc, client, false)

	done := false
	for !done {
		select {
		case <-i.owner.EvtConfigChange():
			customDnsActive = i.updateIvpnClientDnsSettings(wc, client, customDnsActive)
		case <-wc.Done():
			client.Disconnect()
			done = true
		case <-client.Disconnected():
			done = true
		}
	}

	i.resetVpnConnectionInfo()
	wc.Debug("IVPN client disconnected")

	return nil
}

// updateIvpnClientDnsSettings configures the IVPN client to use Portmaster's local DNS resolver if custom DNS servers are configured in Portmaster.
// Parameter:
// - customDnsActive: Whether the IVPN client is currently configured to use custom DNS.
// Returns:
// - The new state of whether custom DNS is active in the IVPN client after attempting to update the settings.
func (i *InteropIvpn) updateIvpnClientDnsSettings(wc *mgr.WorkerCtx, client *ivpnclient.Client, customDnsActive bool) bool {
	wantCustomDns := len(i.owner.DnsNameServers()) > 0
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

func (i *InteropIvpn) onConnectionStarting(wc *mgr.WorkerCtx, _ string, messageData string) {
	info := ivpnclient.ConnectionStarting{}
	err := json.Unmarshal([]byte(messageData), &info)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to parse ConnectionStarting message: %v", err))
		return
	}

	wc.Debug(fmt.Sprintf("IVPN: VPN connection starting %s:%d %s", info.Address, info.Port, packet.IPProtocol(info.Protocol)))

	i.locker.Lock()
	defer i.locker.Unlock()

	i.protocol = info.Protocol
	i.dstPort = info.Port
	i.dstAddress = net.ParseIP(info.Address)
	if i.dstAddress == nil {
		i.dstPort = 0
	}
}

func (i *InteropIvpn) onConnectionStopped(wc *mgr.WorkerCtx, _ string, messageData string) {
	wc.Debug("IVPN: VPN connection stopped")

	i.resetVpnConnectionInfo()
}

func (i *InteropIvpn) setServiceBinary(serviceBinary string) {
	i.locker.Lock()
	defer i.locker.Unlock()
	i.serviceBinary = serviceBinary
}

func (i *InteropIvpn) resetVpnConnectionInfo() {
	i.locker.Lock()
	defer i.locker.Unlock()
	i.dstPort = 0
	i.dstAddress = nil
	i.protocol = 0
}

func (i *InteropIvpn) VerdictHandler(conn *network.Connection) (network.Verdict, string, bool) {
	i.locker.RLock()
	defer i.locker.RUnlock()

	if i.dstPort != 0 {
		if conn.Inbound == false &&
			conn.Entity.Port == i.dstPort &&
			conn.Entity.Protocol == i.protocol {
			return network.VerdictAccept, "IVPN VPN connection", false
		}
	}

	if i.serviceBinary != "" && conn.Process().Path == i.serviceBinary {
		return network.VerdictAccept, "IVPN Service connection", false
	}

	return network.VerdictUndecided, "", false
}
