package process

import (
	"errors"
	"net"

	"github.com/Safing/safing-core/network/packet"
)

var (
	ErrConnectionNotFound = errors.New("could not find connection")
	ErrProcessNotFound    = errors.New("could not find process")
)

func GetPidByPacket(pkt packet.Packet) (pid int, direction bool, err error) {

	var localIP net.IP
	var localPort uint16
	var remoteIP net.IP
	var remotePort uint16
	if pkt.IsInbound() {
		localIP = pkt.GetIPHeader().Dst
		remoteIP = pkt.GetIPHeader().Src
	} else {
		localIP = pkt.GetIPHeader().Src
		remoteIP = pkt.GetIPHeader().Dst
	}
	if pkt.GetIPHeader().Protocol == packet.TCP || pkt.GetIPHeader().Protocol == packet.UDP {
		if pkt.IsInbound() {
			localPort = pkt.GetTCPUDPHeader().DstPort
			remotePort = pkt.GetTCPUDPHeader().SrcPort
		} else {
			localPort = pkt.GetTCPUDPHeader().SrcPort
			remotePort = pkt.GetTCPUDPHeader().DstPort
		}
	}

	switch {
	case pkt.GetIPHeader().Protocol == packet.TCP && pkt.IPVersion() == packet.IPv4:
		return getTCP4PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.GetIPHeader().Protocol == packet.UDP && pkt.IPVersion() == packet.IPv4:
		return getUDP4PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.GetIPHeader().Protocol == packet.TCP && pkt.IPVersion() == packet.IPv6:
		return getTCP6PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	case pkt.GetIPHeader().Protocol == packet.UDP && pkt.IPVersion() == packet.IPv6:
		return getUDP6PacketInfo(localIP, localPort, remoteIP, remotePort, pkt.IsInbound())
	default:
		return -1, false, errors.New("unsupported protocol for finding process")
	}

}

func GetProcessByPacket(pkt packet.Packet) (process *Process, direction bool, err error) {

	var pid int
	pid, direction, err = GetPidByPacket(pkt)
	if pid < 0 {
		return nil, direction, ErrConnectionNotFound
	}
	if err != nil {
		return nil, direction, err
	}

	process, err = GetOrFindProcess(pid)
	if err != nil {
		return nil, direction, err
	}

	return process, direction, nil

}

func GetPidByEndpoints(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol packet.IPProtocol) (pid int, direction bool, err error) {

	ipVersion := packet.IPv4
	if v4 := localIP.To4(); v4 == nil {
		ipVersion = packet.IPv6
	}

	switch {
	case protocol == packet.TCP && ipVersion == packet.IPv4:
		return getTCP4PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.UDP && ipVersion == packet.IPv4:
		return getUDP4PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.TCP && ipVersion == packet.IPv6:
		return getTCP6PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	case protocol == packet.UDP && ipVersion == packet.IPv6:
		return getUDP6PacketInfo(localIP, localPort, remoteIP, remotePort, false)
	default:
		return -1, false, errors.New("unsupported protocol for finding process")
	}

}

func GetProcessByEndpoints(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol packet.IPProtocol) (process *Process, err error) {

	var pid int
	pid, _, err = GetPidByEndpoints(localIP, localPort, remoteIP, remotePort, protocol)
	if err != nil {
		return nil, err
	}
	if pid < 0 {
		return nil, ErrConnectionNotFound
	}

	process, err = GetOrFindProcess(pid)
	if err != nil {
		return nil, err
	}

	return process, nil

}

func GetActiveConnectionIDs() []string {
	return getActiveConnectionIDs()
}

// func GetProcessByPid(pid int) *Process {
// 	process, err := GetOrFindProcess(pid)
// 	if err != nil {
// 		log.Warningf("process: failed to get process %d: %s", pid, err)
// 		return nil
// 	}
// 	return process
// }

// func GetProcessOfConnection(localIP *net.IP, localPort uint16, protocol uint8) (process *Process, status uint8) {
// 	pid, status := GetPidOfConnection(localIP, localPort, protocol)
// 	if status == Success {
// 		process = GetProcessByPid(pid)
// 		if process == nil {
// 			return nil, NoProcessInfo
// 		}
// 	}
// 	return
// }

// func GetProcessByPacket(pkt packet.Packet) (process *Process, direction bool, status uint8) {
// 	pid, direction, status := GetPidByPacket(pkt)
// 	if status == Success {
// 		process = GetProcessByPid(pid)
// 		if process == nil {
// 			return nil, direction, NoProcessInfo
// 		}
// 	}
// 	return
// }
