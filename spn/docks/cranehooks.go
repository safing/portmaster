package docks

import (
	"sync"

	"github.com/safing/portmaster/base/log"
)

var (
	craneUpdateHook     func(crane *Crane)
	craneUpdateHookLock sync.Mutex
)

// RegisterCraneUpdateHook allows the captain to hook into receiving updates for cranes.
func RegisterCraneUpdateHook(fn func(crane *Crane)) {
	craneUpdateHookLock.Lock()
	defer craneUpdateHookLock.Unlock()

	if craneUpdateHook == nil {
		craneUpdateHook = fn
	} else {
		log.Error("spn/docks: crane update hook already registered")
	}
}

// ResetCraneUpdateHook resets the hook for receiving updates for cranes.
func ResetCraneUpdateHook() {
	craneUpdateHookLock.Lock()
	defer craneUpdateHookLock.Unlock()

	craneUpdateHook = nil
}

// NotifyUpdate calls the registers crane update hook function.
func (crane *Crane) NotifyUpdate() {
	if crane == nil {
		return
	}

	craneUpdateHookLock.Lock()
	defer craneUpdateHookLock.Unlock()

	if craneUpdateHook != nil {
		craneUpdateHook(crane)
	}
}
