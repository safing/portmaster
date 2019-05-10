// +build windows

package iphelper

import (
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	tcp4Connections []*ConnectionEntry
	tcp4Listeners   []*ConnectionEntry
	tcp6Connections []*ConnectionEntry
	tcp6Listeners   []*ConnectionEntry

	udp4Connections []*ConnectionEntry
	udp4Listeners   []*ConnectionEntry
	udp6Connections []*ConnectionEntry
	udp6Listeners   []*ConnectionEntry

	ipHelper *IPHelper
	lock     sync.RWMutex

	waitTime = 15 * time.Millisecond
)

func checkIPHelper() (err error) {
	if ipHelper == nil {
		ipHelper, err = New()
		return err
	}
	return nil
}

// GetTCP4PacketInfo returns the pid of the given IPv4/TCP connection.
func GetTCP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {

	// search
	pid, _ = search(tcp4Connections, tcp4Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
	if pid >= 0 {
		return pid, pktDirection, nil
	}

	for i := 0; i < 3; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")

		// if unable to find, refresh
		lock.Lock()
		err = checkIPHelper()
		if err == nil {
			tcp4Connections, tcp4Listeners, err = ipHelper.GetTables(TCP, IPv4)
		}
		lock.Unlock()
		if err != nil {
			return -1, pktDirection, err
		}

		// search
		pid, _ = search(tcp4Connections, tcp4Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
		if pid >= 0 {
			return pid, pktDirection, nil
		}

		time.Sleep(waitTime)
	}

	return -1, pktDirection, nil
}

// GetTCP6PacketInfo returns the pid of the given IPv6/TCP connection.
func GetTCP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {

	// search
	pid, _ = search(tcp6Connections, tcp6Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
	if pid >= 0 {
		return pid, pktDirection, nil
	}

	for i := 0; i < 3; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")

		// if unable to find, refresh
		lock.Lock()
		err = checkIPHelper()
		if err == nil {
			tcp6Connections, tcp6Listeners, err = ipHelper.GetTables(TCP, IPv6)
		}
		lock.Unlock()
		if err != nil {
			return -1, pktDirection, err
		}

		// search
		pid, _ = search(tcp6Connections, tcp6Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
		if pid >= 0 {
			return pid, pktDirection, nil
		}

		time.Sleep(waitTime)
	}

	return -1, pktDirection, nil
}

// GetUDP4PacketInfo returns the pid of the given IPv4/UDP connection.
func GetUDP4PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {

	// search
	pid, _ = search(udp4Connections, udp4Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
	if pid >= 0 {
		return pid, pktDirection, nil
	}

	for i := 0; i < 3; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")

		// if unable to find, refresh
		lock.Lock()
		err = checkIPHelper()
		if err == nil {
			udp4Connections, udp4Listeners, err = ipHelper.GetTables(UDP, IPv4)
		}
		lock.Unlock()
		if err != nil {
			return -1, pktDirection, err
		}

		// search
		pid, _ = search(udp4Connections, udp4Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
		if pid >= 0 {
			return pid, pktDirection, nil
		}

		time.Sleep(waitTime)
	}

	return -1, pktDirection, nil
}

// GetUDP6PacketInfo returns the pid of the given IPv6/UDP connection.
func GetUDP6PacketInfo(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, pktDirection bool) (pid int, direction bool, err error) {

	// search
	pid, _ = search(udp6Connections, udp6Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
	if pid >= 0 {
		return pid, pktDirection, nil
	}

	for i := 0; i < 3; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")

		// if unable to find, refresh
		lock.Lock()
		err = checkIPHelper()
		if err == nil {
			udp6Connections, udp6Listeners, err = ipHelper.GetTables(UDP, IPv6)
		}
		lock.Unlock()
		if err != nil {
			return -1, pktDirection, err
		}

		// search
		pid, _ = search(udp6Connections, udp6Listeners, localIP, remoteIP, localPort, remotePort, pktDirection)
		if pid >= 0 {
			return pid, pktDirection, nil
		}

		time.Sleep(waitTime)
	}

	return -1, pktDirection, nil
}

func search(connections, listeners []*ConnectionEntry, localIP, remoteIP net.IP, localPort, remotePort uint16, pktDirection bool) (pid int, direction bool) {
	lock.RLock()
	defer lock.RUnlock()

	if pktDirection {
		// inbound
		pid = searchListeners(listeners, localIP, localPort)
		if pid >= 0 {
			return pid, true
		}
		pid = searchConnections(connections, localIP, remoteIP, localPort, remotePort)
		if pid >= 0 {
			return pid, false
		}
	} else {
		// outbound
		pid = searchConnections(connections, localIP, remoteIP, localPort, remotePort)
		if pid >= 0 {
			return pid, false
		}
		pid = searchListeners(listeners, localIP, localPort)
		if pid >= 0 {
			return pid, true
		}
	}

	return -1, pktDirection
}

func searchConnections(list []*ConnectionEntry, localIP, remoteIP net.IP, localPort, remotePort uint16) (pid int) {

	for _, entry := range list {
		if localPort == entry.localPort &&
			remotePort == entry.remotePort &&
			remoteIP.Equal(entry.remoteIP) &&
			localIP.Equal(entry.localIP) {
			return entry.pid
		}
	}

	return -1
}

func searchListeners(list []*ConnectionEntry, localIP net.IP, localPort uint16) (pid int) {

	for _, entry := range list {
		if localPort == entry.localPort &&
			(entry.localIP == nil || // nil IP means zero IP, see tables.go
				localIP.Equal(entry.localIP)) {
			return entry.pid
		}
	}

	return -1
}

// GetActiveConnectionIDs returns all currently active connection IDs.
func GetActiveConnectionIDs() (connections []string) {
	lock.Lock()
	defer lock.Unlock()

	for _, entry := range tcp4Connections {
		connections = append(connections, fmt.Sprintf("%d-%s-%d-%s-%d", TCP, entry.localIP, entry.localPort, entry.remoteIP, entry.remotePort))
	}
	for _, entry := range tcp6Connections {
		connections = append(connections, fmt.Sprintf("%d-%s-%d-%s-%d", TCP, entry.localIP, entry.localPort, entry.remoteIP, entry.remotePort))
	}
	for _, entry := range udp4Connections {
		connections = append(connections, fmt.Sprintf("%d-%s-%d-%s-%d", UDP, entry.localIP, entry.localPort, entry.remoteIP, entry.remotePort))
	}
	for _, entry := range udp6Connections {
		connections = append(connections, fmt.Sprintf("%d-%s-%d-%s-%d", UDP, entry.localIP, entry.localPort, entry.remoteIP, entry.remotePort))
	}

	return
}
