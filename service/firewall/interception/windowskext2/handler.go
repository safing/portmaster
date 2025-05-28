//go:build windows
// +build windows

package windowskext

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/windows_kext/kextinterface"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
)

type VersionInfo struct {
	Major    uint8
	Minor    uint8
	Revision uint8
	Build    uint8
}

func (v *VersionInfo) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Revision, v.Build)
}

// Handler transforms received packets to the Packet interface.
func Handler(ctx context.Context, packets chan packet.Packet, bandwidthUpdate chan *packet.BandwidthUpdate) {
	for {
		packetInfo, err := RecvVerdictRequest()

		if errors.Is(err, kextinterface.ErrUnexpectedInfoSize) || errors.Is(err, kextinterface.ErrUnexpectedReadError) {
			log.Criticalf("unexpected kext info data: %s", err)
			continue // Depending on the info type this may not affect the functionality. Try to continue reading the next commands.
		}

		if err != nil {
			log.Warningf("failed to get packet from windows kext: %s", err)
			// Probably IO error, nothing else we can do.
			return
		}

		switch {
		case packetInfo.ConnectionV4 != nil:
			{
				// log.Tracef("packet: %+v", packetInfo.ConnectionV4)
				conn := packetInfo.ConnectionV4
				// New Packet
				newPacket := &Packet{
					verdictRequest: conn.ID,
					payload:        conn.Payload,
					payloadLayer:   conn.PayloadLayer,
					verdictSet:     abool.NewBool(false),
				}
				info := newPacket.Info()
				info.Inbound = conn.Direction > 0
				info.InTunnel = false
				info.Protocol = packet.IPProtocol(conn.Protocol)
				info.PID = int(conn.ProcessID)
				info.SeenAt = time.Now()

				// Check PID
				if info.PID == 0 {
					// Windows does not have zero PIDs.
					// Set to UndefinedProcessID.
					info.PID = process.UndefinedProcessID
				}

				// Set IP version
				info.Version = packet.IPv4

				// Set IPs
				if info.Inbound {
					// Inbound
					info.Src = conn.RemoteIP[:]
					info.Dst = conn.LocalIP[:]
				} else {
					// Outbound
					info.Src = conn.LocalIP[:]
					info.Dst = conn.RemoteIP[:]
				}

				// Set Ports
				if info.Inbound {
					// Inbound
					info.SrcPort = conn.RemotePort
					info.DstPort = conn.LocalPort
				} else {
					// Outbound
					info.SrcPort = conn.LocalPort
					info.DstPort = conn.RemotePort
				}

				packets <- newPacket
			}
		case packetInfo.ConnectionV6 != nil:
			{
				// log.Tracef("packet: %+v", packetInfo.ConnectionV6)
				conn := packetInfo.ConnectionV6
				// New Packet
				newPacket := &Packet{
					verdictRequest: conn.ID,
					payload:        conn.Payload,
					payloadLayer:   conn.PayloadLayer,
					verdictSet:     abool.NewBool(false),
				}
				info := newPacket.Info()
				info.Inbound = conn.Direction > 0
				info.InTunnel = false
				info.Protocol = packet.IPProtocol(conn.Protocol)
				info.PID = int(conn.ProcessID)
				info.SeenAt = time.Now()

				// Check PID
				if info.PID == 0 {
					// Windows does not have zero PIDs.
					// Set to UndefinedProcessID.
					info.PID = process.UndefinedProcessID
				}

				// Set IP version
				info.Version = packet.IPv6

				// Set IPs
				if info.Inbound {
					// Inbound
					info.Src = conn.RemoteIP[:]
					info.Dst = conn.LocalIP[:]
				} else {
					// Outbound
					info.Src = conn.LocalIP[:]
					info.Dst = conn.RemoteIP[:]
				}

				// Set Ports
				if info.Inbound {
					// Inbound
					info.SrcPort = conn.RemotePort
					info.DstPort = conn.LocalPort
				} else {
					// Outbound
					info.SrcPort = conn.LocalPort
					info.DstPort = conn.RemotePort
				}

				packets <- newPacket
			}
		case packetInfo.LogLine != nil:
			{
				line := packetInfo.LogLine
				switch line.Severity {
				case byte(log.DebugLevel):
					log.Debugf("kext: %s", line.Line)
				case byte(log.InfoLevel):
					log.Infof("kext: %s", line.Line)
				case byte(log.WarningLevel):
					log.Warningf("kext: %s", line.Line)
				case byte(log.ErrorLevel):
					log.Errorf("kext: %s", line.Line)
				case byte(log.CriticalLevel):
					log.Criticalf("kext: %s", line.Line)
				}
			}
		case packetInfo.BandwidthStats != nil:
			{
				bandwidthStats := packetInfo.BandwidthStats
				for _, stat := range bandwidthStats.ValuesV4 {
					connID := packet.CreateConnectionID(
						packet.IPProtocol(bandwidthStats.Protocol),
						net.IP(stat.LocalIP[:]), stat.LocalPort,
						net.IP(stat.RemoteIP[:]), stat.RemotePort,
						false,
					)
					update := &packet.BandwidthUpdate{
						ConnID:        connID,
						BytesReceived: stat.ReceivedBytes,
						BytesSent:     stat.TransmittedBytes,
						Method:        packet.Additive,
					}
					bandwidthUpdate <- update
				}
				for _, stat := range bandwidthStats.ValuesV6 {
					connID := packet.CreateConnectionID(
						packet.IPProtocol(bandwidthStats.Protocol),
						net.IP(stat.LocalIP[:]), stat.LocalPort,
						net.IP(stat.RemoteIP[:]), stat.RemotePort,
						false,
					)
					update := &packet.BandwidthUpdate{
						ConnID:        connID,
						BytesReceived: stat.ReceivedBytes,
						BytesSent:     stat.TransmittedBytes,
						Method:        packet.Additive,
					}
					bandwidthUpdate <- update
				}
			}
		}
	}
}
