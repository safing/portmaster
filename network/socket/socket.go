package socket

import "net"

const (
	// UnidentifiedProcessID is originally defined in the process pkg, but duplicated here because of import loops.
	UnidentifiedProcessID = -1
)

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

// Info is a generic interface to both ConnectionInfo and BindInfo.
type Info interface {
	GetPID() int
	SetPID(int)
	GetUID() int
	GetInode() int
}

// GetPID returns the PID.
func (i *ConnectionInfo) GetPID() int { return i.PID }

// SetPID sets the PID to the given value.
func (i *ConnectionInfo) SetPID(pid int) { i.PID = pid }

// GetUID returns the UID.
func (i *ConnectionInfo) GetUID() int { return i.UID }

// GetInode returns the Inode.
func (i *ConnectionInfo) GetInode() int { return i.Inode }

// GetPID returns the PID.
func (i *BindInfo) GetPID() int { return i.PID }

// SetPID sets the PID to the given value.
func (i *BindInfo) SetPID(pid int) { i.PID = pid }

// GetUID returns the UID.
func (i *BindInfo) GetUID() int { return i.UID }

// GetInode returns the Inode.
func (i *BindInfo) GetInode() int { return i.Inode }
