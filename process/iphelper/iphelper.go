// +build windows

package iphelper

import (
	"errors"
	"fmt"

	"github.com/tevino/abool"
	"golang.org/x/sys/windows"
)

var (
	errInvalid = errors.New("IPHelper not initialzed or broken")
)

// IPHelper represents a subset of the Windows iphlpapi.dll.
type IPHelper struct {
	dll *windows.LazyDLL

	getExtendedTCPTable *windows.LazyProc
	getExtendedUDPTable *windows.LazyProc
	// getOwnerModuleFromTcpEntry  *windows.LazyProc
	// getOwnerModuleFromTcp6Entry *windows.LazyProc
	// getOwnerModuleFromUdpEntry  *windows.LazyProc
	// getOwnerModuleFromUdp6Entry *windows.LazyProc

	valid *abool.AtomicBool
}

// New returns a new IPHelper API (with an instance of iphlpapi.dll loaded).
func New() (*IPHelper, error) {

	new := &IPHelper{}
	new.valid = abool.NewBool(false)
	var err error

	// load dll
	new.dll = windows.NewLazySystemDLL("iphlpapi.dll")
	err = new.dll.Load()
	if err != nil {
		return nil, err
	}

	// load functions
	new.getExtendedTCPTable = new.dll.NewProc("GetExtendedTcpTable")
	err = new.getExtendedTCPTable.Find()
	if err != nil {
		return nil, fmt.Errorf("could find proc GetExtendedTcpTable: %s", err)
	}
	new.getExtendedUDPTable = new.dll.NewProc("GetExtendedUdpTable")
	err = new.getExtendedUDPTable.Find()
	if err != nil {
		return nil, fmt.Errorf("could find proc GetExtendedUdpTable: %s", err)
	}
	// new.getOwnerModuleFromTcpEntry = new.dll.NewProc("GetOwnerModuleFromTcpEntry")
	// err = new.getOwnerModuleFromTcpEntry.Find()
	// if err != nil {
	// 	return nil, fmt.Errorf("could find proc GetOwnerModuleFromTcpEntry: %s", err)
	// }
	// new.getOwnerModuleFromTcp6Entry = new.dll.NewProc("GetOwnerModuleFromTcp6Entry")
	// err = new.getOwnerModuleFromTcp6Entry.Find()
	// if err != nil {
	// 	return nil, fmt.Errorf("could find proc GetOwnerModuleFromTcp6Entry: %s", err)
	// }
	// new.getOwnerModuleFromUdpEntry = new.dll.NewProc("GetOwnerModuleFromUdpEntry")
	// err = new.getOwnerModuleFromUdpEntry.Find()
	// if err != nil {
	// 	return nil, fmt.Errorf("could find proc GetOwnerModuleFromUdpEntry: %s", err)
	// }
	// new.getOwnerModuleFromUdp6Entry = new.dll.NewProc("GetOwnerModuleFromUdp6Entry")
	// err = new.getOwnerModuleFromUdp6Entry.Find()
	// if err != nil {
	// 	return nil, fmt.Errorf("could find proc GetOwnerModuleFromUdp6Entry: %s", err)
	// }

	new.valid.Set()
	return new, nil
}
