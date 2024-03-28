package socket

import (
	"net"
	"sync"
)

const (
	// UndefinedProcessID signifies that the process ID is unknown.
	// It must match portmaster/process.UndefinedProcessID
	// It is duplicated here because of import loops.
	UndefinedProcessID = -1
)

// ConnectionInfo holds socket information returned by the system.
type ConnectionInfo struct {
	sync.Mutex

	Local  Address
	Remote Address
	PID    int
	UID    int
	Inode  int
}

// BindInfo holds socket information returned by the system.
type BindInfo struct {
	sync.Mutex

	Local Address
	PID   int
	UID   int
	Inode int

	ListensAny bool
}

// Address is an IP + Port pair.
type Address struct {
	IP   net.IP
	Port uint16
}

// Info is a generic interface to both ConnectionInfo and BindInfo.
type Info interface {
	GetPID() int
	SetPID(pid int)
	GetUID() int
	GetUIDandInode() (int, int)
}

// GetPID returns the PID.
func (i *ConnectionInfo) GetPID() int {
	i.Lock()
	defer i.Unlock()

	return i.PID
}

// SetPID sets the PID to the given value.
func (i *ConnectionInfo) SetPID(pid int) {
	i.Lock()
	defer i.Unlock()

	i.PID = pid
}

// GetUID returns the UID.
func (i *ConnectionInfo) GetUID() int {
	i.Lock()
	defer i.Unlock()

	return i.UID
}

// GetUIDandInode returns the UID and Inode.
func (i *ConnectionInfo) GetUIDandInode() (int, int) {
	i.Lock()
	defer i.Unlock()

	return i.UID, i.Inode
}

// GetPID returns the PID.
func (i *BindInfo) GetPID() int {
	i.Lock()
	defer i.Unlock()

	return i.PID
}

// SetPID sets the PID to the given value.
func (i *BindInfo) SetPID(pid int) {
	i.Lock()
	defer i.Unlock()

	i.PID = pid
}

// GetUID returns the UID.
func (i *BindInfo) GetUID() int {
	i.Lock()
	defer i.Unlock()

	return i.UID
}

// GetUIDandInode returns the UID and Inode.
func (i *BindInfo) GetUIDandInode() (int, int) {
	i.Lock()
	defer i.Unlock()

	return i.UID, i.Inode
}

// Compile time checks.
var (
	_ Info = new(ConnectionInfo)
	_ Info = new(BindInfo)
)
