//go:build windows
// +build windows

package windowskext

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/safing/portmaster/service/process"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

const (
	// VerdictRequestFlagFastTrackPermitted is set on packets that have been
	// already permitted by the kernel extension and the verdict request is only
	// informational.
	VerdictRequestFlagFastTrackPermitted = 1

	// VerdictRequestFlagSocketAuth indicates that the verdict request is for a
	// connection that was intercepted on an ALE layer instead of in the network
	// stack itself. Thus, no packet data is available.
	VerdictRequestFlagSocketAuth = 2

	// VerdictRequestFlagExpectSocketAuth indicates that the next verdict
	// requests is expected to be an informational socket auth request from
	// the ALE layer.
	VerdictRequestFlagExpectSocketAuth = 4
)

// Do not change the order of the members! The structure is used to communicate with the kernel extension.
// VerdictRequest is the request structure from the Kext.
type VerdictRequest struct {
	id         uint32 // ID from RegisterPacket
	pid        uint64 // Process ID - info only packets
	direction  uint8
	ipV6       uint8     // True: IPv6, False: IPv4
	protocol   uint8     // Protocol
	flags      uint8     // Flags
	localIP    [4]uint32 // Source Address
	remoteIP   [4]uint32 // Destination Address
	localPort  uint16    // Source Port
	remotePort uint16    // Destination port
	_          uint32    // compartmentID
	_          uint32    // interfaceIndex
	_          uint32    // subInterfaceIndex
	packetSize uint32
}

// Do not change the order of the members! The structure is used to communicate with the kernel extension.
type VerdictInfo struct {
	id      uint32          // ID from RegisterPacket
	verdict network.Verdict // verdict for the connection
}

// Do not change the order of the members! The structure to communicate with the kernel extension.
type VerdictUpdateInfo struct {
	localIP    [4]uint32 // Source Address, only srcIP[0] if IPv4
	remoteIP   [4]uint32 // Destination Address
	localPort  uint16    // Source Port
	remotePort uint16    // Destination port
	ipV6       uint8     // True: IPv6, False: IPv4
	protocol   uint8     // Protocol (UDP, TCP, ...)
	verdict    uint8     // New verdict
}

type ConnectionStat struct {
	localIP          [4]uint32 //Source Address, only srcIP[0] if IPv4
	remoteIP         [4]uint32 //Destination Address
	localPort        uint16    //Source Port
	remotePort       uint16    //Destination port
	receivedBytes    uint64    //Number of bytes recived on this connection
	transmittedBytes uint64    //Number of bytes transsmited from this connection
	ipV6             uint8     //True: IPv6, False: IPv4
	protocol         uint8     //Protocol (UDP, TCP, ...)
}

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
			// Check if we are done with processing.
			if errors.Is(err, ErrKextNotReady) {
				return
			}

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
		info.Inbound = packetInfo.direction > 0
		info.InTunnel = false
		info.Protocol = packet.IPProtocol(packetInfo.protocol)
		info.PID = int(packetInfo.pid)
		info.SeenAt = time.Now()

		// Check PID
		if info.PID == 0 {
			// Windows does not have zero PIDs.
			// Set to UndefinedProcessID.
			info.PID = process.UndefinedProcessID
		}

		// Set IP version
		if packetInfo.ipV6 == 1 {
			info.Version = packet.IPv6
		} else {
			info.Version = packet.IPv4
		}

		// Set IPs
		if info.Inbound {
			// Inbound
			info.Src = convertArrayToIP(packetInfo.remoteIP, info.Version == packet.IPv6)
			info.Dst = convertArrayToIP(packetInfo.localIP, info.Version == packet.IPv6)
		} else {
			// Outbound
			info.Src = convertArrayToIP(packetInfo.localIP, info.Version == packet.IPv6)
			info.Dst = convertArrayToIP(packetInfo.remoteIP, info.Version == packet.IPv6)
		}

		// Set Ports
		if info.Inbound {
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

// convertArrayToIP converts an array of uint32 values to a net.IP address.
func convertArrayToIP(input [4]uint32, ipv6 bool) net.IP {
	if !ipv6 {
		addressBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(addressBuf, input[0])
		return net.IP(addressBuf)
	}

	addressBuf := make([]byte, 16)
	for i := 0; i < 4; i++ {
		binary.BigEndian.PutUint32(addressBuf[i*4:i*4+4], input[i])
	}
	return net.IP(addressBuf)
}

func ipAddressToArray(ip net.IP, isIPv6 bool) [4]uint32 {
	array := [4]uint32{0}
	if isIPv6 {
		for i := 0; i < 4; i++ {
			binary.BigEndian.PutUint32(asByteArrayWithLength(&array[i], 4), getUInt32Value(&ip[i]))
		}
	} else {
		binary.BigEndian.PutUint32(asByteArrayWithLength(&array[0], 4), getUInt32Value(&ip[0]))
	}

	return array
}

func asByteArray[T any](obj *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(obj)), unsafe.Sizeof(*obj))
}

func asByteArrayWithLength[T any](obj *T, size uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(obj)), size)
}

func getUInt32Value[T any](obj *T) uint32 {
	return *(*uint32)(unsafe.Pointer(obj))
}
