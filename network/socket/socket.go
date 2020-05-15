package socket

import "net"

// ConnectionInfo holds socket information returned by the system.
type ConnectionInfo struct {
	Local  Address
	Remote Address
	PID    int
	UID    int
	Inode  int
}

// BindInfo holds socket information returned by the system.
type BindInfo struct {
	Local Address
	PID   int
	UID   int
	Inode int
}

// Address is an IP + Port pair.
type Address struct {
	IP   net.IP
	Port uint16
}
