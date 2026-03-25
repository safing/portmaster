package ivpn

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
)

// notification handler: VPN connection is going to start
func (i *InteropIvpn) onConnectionStarting(wc *mgr.WorkerCtx, _ string, messageData string) {
	connInfo := ivpnclient.ConnectionStarting{}
	err := json.Unmarshal([]byte(messageData), &connInfo)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to parse ConnectionStarting message: %v", err))
		return
	}

	conn := vpnConnectionInfo{
		dstPort:    connInfo.Port,
		dstAddress: net.ParseIP(connInfo.Address),
		protocol:   connInfo.Protocol,
	}
	if conn.dstAddress == nil {
		conn.dstPort = 0 // Invalidate port if address is invalid, to avoid false matches in firewall verdicts.
	}

	// Update/Store VPN connection info for use in firewall verdicts
	status := *i.getStatus()
	status.vpnConnection = conn
	i.setStatus(&status)

	wc.Debug(fmt.Sprintf("IVPN: VPN connection starting %s:%d %s", connInfo.Address, connInfo.Port, packet.IPProtocol(connInfo.Protocol)))
}

// notification handler: VPN connection stopped
func (i *InteropIvpn) onConnectionStopped(wc *mgr.WorkerCtx, _ string, _ string) {
	status := *i.getStatus()
	status.vpnConnection = vpnConnectionInfo{}
	status.connectedInfo = nil
	i.setStatus(&status)

	wc.Debug("IVPN: VPN connection stopped")
}

// notification handler: VPN connection established successfully
func (i *InteropIvpn) onConnectedResp(wc *mgr.WorkerCtx, _ string, messageData string) {
	connectedResp := ivpnclient.ConnectedResp{}
	err := json.Unmarshal([]byte(messageData), &connectedResp)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: Failed to parse ConnectedResp message: %v", err))
		return
	}

	status := *i.getStatus()
	status.connectedInfo = &connectedResp
	i.setStatus(&status)

	wc.Debug(fmt.Sprintf("IVPN: VPN connection established (vpnType:%v; localIPv4:%v; localIPv6:%v)",
		connectedResp.VpnType, connectedResp.ClientIP, connectedResp.ClientIPv6))

	err = i.ensureSPNCompatibility(wc)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: %v", err))
	}
}
