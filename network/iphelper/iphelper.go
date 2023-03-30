//go:build windows

package iphelper

import (
	"errors"
	"fmt"

	"github.com/tevino/abool"
	"golang.org/x/sys/windows"
)

var (
	errInvalid = errors.New("IPHelper not initialized or broken")
)

// IPHelper represents a subset of the Windows iphlpapi.dll.
type IPHelper struct {
	dll *windows.LazyDLL

	getExtendedTCPTable *windows.LazyProc
	getExtendedUDPTable *windows.LazyProc

	valid *abool.AtomicBool
}

func checkIPHelper() (err error) {
	if ipHelper == nil {
		ipHelper, err = New()
		return err
	}
	return nil
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

	new.valid.Set()
	return new, nil
}
