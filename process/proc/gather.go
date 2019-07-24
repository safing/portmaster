package proc

import (
	"net"
	"time"
)

const (
	Success uint8 = iota
	NoSocket
	NoProcess
)

var (
	waitTime = 15 * time.Millisecond
)

// GetPidOfConnection returns the PID of the given connection.
func GetPidOfConnection(localIP net.IP, localPort uint16, protocol uint8) (pid int, status uint8) {
	uid, inode, ok := getConnectionSocket(localIP, localPort, protocol)
	if !ok {
		uid, inode, ok = getListeningSocket(localIP, localPort, protocol)
		for i := 0; i < 3 && !ok; i++ {
			// give kernel some time, then try again
			// log.Tracef("process: giving kernel some time to think")
			time.Sleep(waitTime)
			uid, inode, ok = getConnectionSocket(localIP, localPort, protocol)
			if !ok {
				uid, inode, ok = getListeningSocket(localIP, localPort, protocol)
			}
		}
		if !ok {
			return -1, NoSocket
		}
	}

	pid, ok = GetPidOfInode(uid, inode)
	for i := 0; i < 3 && !ok; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")
		time.Sleep(waitTime)
		pid, ok = GetPidOfInode(uid, inode)
	}
	if !ok {
		return -1, NoProcess
	}

	return
}

// GetPidOfConnection returns the PID of the given incoming connection.
func GetPidOfIncomingConnection(localIP net.IP, localPort uint16, protocol uint8) (pid int, status uint8) {
	uid, inode, ok := getListeningSocket(localIP, localPort, protocol)
	if !ok {
		// for TCP4 and UDP4, also try TCP6 and UDP6, as linux sometimes treats them as a single dual socket, and shows the IPv6 version.
		switch protocol {
		case TCP4:
			uid, inode, ok = getListeningSocket(localIP, localPort, TCP6)
		case UDP4:
			uid, inode, ok = getListeningSocket(localIP, localPort, UDP6)
		}

		if !ok {
			return -1, NoSocket
		}
	}

	pid, ok = GetPidOfInode(uid, inode)
	for i := 0; i < 3 && !ok; i++ {
		// give kernel some time, then try again
		// log.Tracef("process: giving kernel some time to think")
		time.Sleep(waitTime)
		pid, ok = GetPidOfInode(uid, inode)
	}
	if !ok {
		return -1, NoProcess
	}

	return
}
