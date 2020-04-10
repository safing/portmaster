// +build linux

package proc

import (
	"errors"
	"net"
)

// GetTCP4PacketInfo searches the network state tables for a TCP4 connection
func GetTCP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(TCP4, localIP, localPort, pktDirection)
}

// GetTCP6PacketInfo searches the network state tables for a TCP6 connection
func GetTCP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(TCP6, localIP, localPort, pktDirection)
}

// GetUDP4PacketInfo searches the network state tables for a UDP4 connection
func GetUDP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(UDP4, localIP, localPort, pktDirection)
}

// GetUDP6PacketInfo searches the network state tables for a UDP6 connection
func GetUDP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(UDP6, localIP, localPort, pktDirection)
}

func search(protocol uint8, localIP net.IP, localPort uint16, pktDirection bool) (pid int, direction bool, err error) {

	var status uint8
	if pktDirection {
		pid, status = GetPidOfIncomingConnection(localIP, localPort, protocol)
		if pid >= 0 {
			return pid, true, nil
		}
		// pid, status = GetPidOfConnection(localIP, localPort, protocol)
		// if pid >= 0 {
		// 	return pid, false, nil
		// }
	} else {
		pid, status = GetPidOfConnection(localIP, localPort, protocol)
		if pid >= 0 {
			return pid, false, nil
		}
		// pid, status = GetPidOfIncomingConnection(localIP, localPort, protocol)
		// if pid >= 0 {
		// 	return pid, true, nil
		// }
	}

	switch status {
	case NoSocket:
		return -1, direction, errors.New("could not find socket")
	case NoProcess:
		return -1, direction, errors.New("could not find PID")
	default:
		return -1, direction, nil
	}

}
