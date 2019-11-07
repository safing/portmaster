package main

import (
	"sync"
)

var (
	startupComplete   = make(chan struct{}) // signal that the start procedure completed (is never closed, just signaled once)
	shuttingDown      = make(chan struct{}) // signal that we are shutting down (will be closed, may not be closed directly, use initiateShutdown)
	shutdownInitiated = false               // not to be used directly
	//nolint:deadcode,unused // false positive on linux, currently used by windows only
	shutdownError error // may not be read or written to directly
	shutdownLock  sync.Mutex
)

func initiateShutdown(err error) {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()

	if !shutdownInitiated {
		shutdownInitiated = true
		shutdownError = err
		close(shuttingDown)
	}
}

//nolint:deadcode,unused // false positive on linux, currently used by windows only
func getShutdownError() error {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()

	return shutdownError
}
