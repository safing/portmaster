// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

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

func GetPidOfConnection(localIP *net.IP, localPort uint16, protocol uint8) (pid int, status uint8) {
	uid, inode, ok := getConnectionSocket(localIP, localPort, protocol)
	if !ok {
		uid, inode, ok = getListeningSocket(localIP, localPort, protocol)
		for i := 0; i < 3 && !ok; i++ {
			// give kernel some time, then try again
			// log.Tracef("process: giving kernel some time to think")
			time.Sleep(15 * time.Millisecond)
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
		time.Sleep(15 * time.Millisecond)
		pid, ok = GetPidOfInode(uid, inode)
	}
	if !ok {
		return -1, NoProcess
	}
	return
}

func GetPidOfIncomingConnection(localIP *net.IP, localPort uint16, protocol uint8) (pid int, status uint8) {
	uid, inode, ok := getListeningSocket(localIP, localPort, protocol)
	if !ok {
		return -1, NoSocket
	}
	pid, ok = GetPidOfInode(uid, inode)
	if !ok {
		return -1, NoProcess
	}
	return
}
