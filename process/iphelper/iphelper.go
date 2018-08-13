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

type IPHelper struct {
	dll *windows.LazyDLL

	getExtendedTcpTable *windows.LazyProc
	getExtendedUdpTable *windows.LazyProc
	// getOwnerModuleFromTcpEntry  *windows.LazyProc
	// getOwnerModuleFromTcp6Entry *windows.LazyProc
	// getOwnerModuleFromUdpEntry  *windows.LazyProc
	// getOwnerModuleFromUdp6Entry *windows.LazyProc

	valid *abool.AtomicBool
}

func New() (*IPHelper, error) {

	new := &IPHelper{}
	new.valid = abool.NewBool(false)
	var err error

	// load dll
	new.dll = windows.NewLazySystemDLL("iphlpapi.dll")
	new.dll.Load()
	if err != nil {
		return nil, err
	}

	// load functions
	new.getExtendedTcpTable = new.dll.NewProc("GetExtendedTcpTable")
	err = new.getExtendedTcpTable.Find()
	if err != nil {
		return nil, fmt.Errorf("could find proc GetExtendedTcpTable: %s", err)
	}
	new.getExtendedUdpTable = new.dll.NewProc("GetExtendedUdpTable")
	err = new.getExtendedUdpTable.Find()
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
