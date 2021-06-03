package main

import (
	"sync"
)

var (
	startupComplete = make(chan struct{}) // signal that the start procedure completed (is never closed, just signaled once)
	shuttingDown    = make(chan struct{}) // signal that we are shutting down (will be closed, may not be closed directly, use initiateShutdown)
	//nolint:unused // false positive on linux, currently used by windows only
	shutdownError error // protected by shutdownLock
	shutdownLock  sync.Mutex
)

func initiateShutdown(err error) {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()

	select {
	case <-shuttingDown:
		return
	default:
		shutdownError = err
		close(shuttingDown)
	}
}

func isShuttingDown() bool {
	select {
	case <-shuttingDown:
		return true
	default:
		return false
	}
}

//nolint:deadcode,unused // false positive on linux, currently used by windows only
func getShutdownError() error {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()

	return shutdownError
}
