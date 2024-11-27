package main

import (
	"sync"
)

var (
	// startupComplete signals that the start procedure completed.
	// The channel is not closed, just signaled once.
	startupComplete = make(chan struct{})

	// shuttingDown signals that we are shutting down.
	// The channel will be closed, but may not be closed directly - only via initiateShutdown.
	shuttingDown = make(chan struct{})

	// shutdownError is protected by shutdownLock.
	shutdownError error //nolint:unused,errname // Not what the linter thinks it is. Currently used on windows only.
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
