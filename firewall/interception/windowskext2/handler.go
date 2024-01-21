//go:build windows
// +build windows

package windowskext

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portmaster/process"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

type VersionInfo struct {
	major    uint8
	minor    uint8
	revision uint8
	build    uint8
}

func (v *VersionInfo) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v.major, v.minor, v.revision, v.build)
}

// Handler transforms received packets to the Packet interface.
func Handler(ctx context.Context, packets chan packet.Packet) {
	for {
		packetInfo, err := RecvVerdictRequest()
		if err != nil {
			log.Warningf("failed to get packet from windows kext: %s", err)
			return
		}

		if packetInfo.Connection != nil {
			log.Tracef("packet: %+v", packetInfo.Connection)
			conn := packetInfo.Connection
			// New Packet
			new := &Packet{
				verdictRequest: conn.Id,
				verdictSet:     abool.NewBool(false),
			}
			info := new.Info()
			info.Inbound = conn.Direction > 0
			info.InTunnel = false
			info.Protocol = packet.IPProtocol(conn.Protocol)
			info.PID = int(conn.ProcessId)
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
				info.Src = conn.RemoteIp[:]
				info.Dst = conn.LocalIp[:]
			} else {
				// Outbound
				info.Src = conn.LocalIp[:]
				info.Dst = conn.RemoteIp[:]
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

			packets <- new
		}

		// if packetInfo.LogLines != nil {
		// 	for _, line := range *packetInfo.LogLines {
		// 		switch line.Severity {
		// 		case int(log.DebugLevel):
		// 			log.Debugf("kext: %s", line.Line)
		// 		case int(log.InfoLevel):
		// 			log.Infof("kext: %s", line.Line)
		// 		case int(log.WarningLevel):
		// 			log.Warningf("kext: %s", line.Line)
		// 		case int(log.ErrorLevel):
		// 			log.Errorf("kext: %s", line.Line)
		// 		case int(log.CriticalLevel):
		// 			log.Criticalf("kext: %s", line.Line)
		// 		}
		// 	}
		// }
	}
}
