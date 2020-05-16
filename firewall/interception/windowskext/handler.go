// +build windows

package windowskext

import (
	"encoding/binary"
	"net"

	"github.com/tevino/abool"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// VerdictRequest is the request structure from the Kext.
type VerdictRequest struct {
	id         uint32 // ID from RegisterPacket
	_          uint64 // Process ID - does not yet work
	direction  uint8
	ipV6       uint8 // True: IPv6, False: IPv4
	protocol   uint8 // Protocol
	_          uint8
	localIP    [4]uint32 // Source Address
	remoteIP   [4]uint32 // Destination Address
	localPort  uint16    // Source Port
	remotePort uint16    // Destination port
	_          uint32    // compartmentID
	_          uint32    // interfaceIndex
	_          uint32    // subInterfaceIndex
	packetSize uint32
}

// Handler transforms received packets to the Packet interface.
func Handler(packets chan packet.Packet) {
	if !ready.IsSet() {
		return
	}

	defer close(packets)

	for {
		if !ready.IsSet() {
			return
		}

		packetInfo, err := RecvVerdictRequest()
		if err != nil {
			log.Warningf("failed to get packet from windows kext: %s", err)
			continue
		}

		if packetInfo == nil {
			continue
		}

		// log.Tracef("packet: %+v", packetInfo)

		// New Packet
		new := &Packet{
			verdictRequest: packetInfo,
			verdictSet:     abool.NewBool(false),
		}

		info := new.Info()
		info.Direction = packetInfo.direction > 0
		info.InTunnel = false
		info.Protocol = packet.IPProtocol(packetInfo.protocol)

		// IP version
		if packetInfo.ipV6 == 1 {
			info.Version = packet.IPv6
		} else {
			info.Version = packet.IPv4
		}

		// IPs
		if info.Version == packet.IPv4 {
			// IPv4
			if info.Direction {
				// Inbound
				info.Src = convertIPv4(packetInfo.remoteIP)
				info.Dst = convertIPv4(packetInfo.localIP)
			} else {
				// Outbound
				info.Src = convertIPv4(packetInfo.localIP)
				info.Dst = convertIPv4(packetInfo.remoteIP)
			}
		} else {
			// IPv6
			if info.Direction {
				// Inbound
				info.Src = convertIPv6(packetInfo.remoteIP)
				info.Dst = convertIPv6(packetInfo.localIP)
			} else {
				// Outbound
				info.Src = convertIPv6(packetInfo.localIP)
				info.Dst = convertIPv6(packetInfo.remoteIP)
			}
		}

		// Ports
		if info.Direction {
			// Inbound
			info.SrcPort = packetInfo.remotePort
			info.DstPort = packetInfo.localPort
		} else {
			// Outbound
			info.SrcPort = packetInfo.localPort
			info.DstPort = packetInfo.remotePort
		}

		packets <- new
	}
}

// convertIPv4 as needed for data from the kernel
func convertIPv4(input [4]uint32) net.IP {
	addressBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(addressBuf, input[0])
	return net.IP(addressBuf)
}

func convertIPv6(input [4]uint32) net.IP {
	addressBuf := make([]byte, 16)
	for i := 0; i < 4; i++ {
		binary.BigEndian.PutUint32(addressBuf[i*4:i*4+4], input[i])
	}
	return net.IP(addressBuf)
}
