// +build windows

package iphelper

import (
	"sync"

	"github.com/safing/portmaster/network/socket"
)

const (
	unidentifiedProcessID = -1
)

var (
	ipHelper *IPHelper
	lock     sync.RWMutex
)

// GetTCP4Table returns the system table for IPv4 TCP activity.
func GetTCP4Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	lock.Lock()
	defer lock.Unlock()

	err = checkIPHelper()
	if err != nil {
		return nil, nil, err
	}

	return ipHelper.getTable(IPv4, TCP)
}

// GetTCP6Table returns the system table for IPv6 TCP activity.
func GetTCP6Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	lock.Lock()
	defer lock.Unlock()

	err = checkIPHelper()
	if err != nil {
		return nil, nil, err
	}

	return ipHelper.getTable(IPv6, TCP)
}

// GetUDP4Table returns the system table for IPv4 UDP activity.
func GetUDP4Table() (binds []*socket.BindInfo, err error) {
	lock.Lock()
	defer lock.Unlock()

	err = checkIPHelper()
	if err != nil {
		return nil, err
	}

	_, binds, err = ipHelper.getTable(IPv4, UDP)
	return
}

// GetUDP6Table returns the system table for IPv6 UDP activity.
func GetUDP6Table() (binds []*socket.BindInfo, err error) {
	lock.Lock()
	defer lock.Unlock()

	err = checkIPHelper()
	if err != nil {
		return nil, err
	}

	_, binds, err = ipHelper.getTable(IPv6, UDP)
	return
}
