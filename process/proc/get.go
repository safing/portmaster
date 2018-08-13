package proc

import (
	"errors"
	"net"
)

func GetTCP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(TCP4, localIP, localPort, direction)
}

func GetTCP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(TCP6, localIP, localPort, direction)
}

func GetUDP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(UDP4, localIP, localPort, direction)
}

func GetUDP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {
	return search(UDP6, localIP, localPort, direction)
}

func search(protocol uint8, localIP net.IP, localPort uint16, pktDirection bool) (pid int, direction bool, err error) {

	var status uint8
	if pktDirection {
		pid, status = GetPidOfIncomingConnection(&localIP, localPort, protocol)
		if pid >= 0 {
			return pid, true, nil
		}
		// pid, status = GetPidOfConnection(&localIP, localPort, protocol)
		// if pid >= 0 {
		// 	return pid, false, nil
		// }
	} else {
		pid, status = GetPidOfConnection(&localIP, localPort, protocol)
		if pid >= 0 {
			return pid, false, nil
		}
		// pid, status = GetPidOfIncomingConnection(&localIP, localPort, protocol)
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
