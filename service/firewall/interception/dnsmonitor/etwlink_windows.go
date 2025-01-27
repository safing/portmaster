//go:build windows
// +build windows

package dnsmonitor

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/service/integration"
	"golang.org/x/sys/windows"
)

type ETWSession struct {
	i *integration.ETWFunctions

	shutdownGuard atomic.Bool
	shutdownMutex sync.Mutex

	state uintptr
}

// NewSession creates new ETW event listener and initializes it. This is a low level interface, make sure to call DestroySession when you are done using it.
func NewSession(etwInterface *integration.ETWFunctions, callback func(domain string, pid uint32, result string)) (*ETWSession, error) {
	if etwInterface == nil {
		return nil, fmt.Errorf("etw interface was nil")
	}
	etwSession := &ETWSession{
		i: etwInterface,
	}

	// Make sure session from previous instances are not running.
	_ = etwSession.i.StopOldSession()

	// Initialize notification activated callback
	win32Callback := windows.NewCallback(func(domain *uint16, pid uint32, result *uint16) uintptr {
		callback(windows.UTF16PtrToString(domain), pid, windows.UTF16PtrToString(result))
		return 0
	})
	// The function only allocates memory it will not fail.
	etwSession.state = etwSession.i.CreateState(win32Callback)

	// Make sure DestroySession is called even if caller forgets to call it.
	runtime.SetFinalizer(etwSession, func(s *ETWSession) {
		_ = s.i.DestroySession(s.state)
	})

	// Initialize session.
	err := etwSession.i.InitializeSession(etwSession.state)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session: %q", err)
	}

	return etwSession, nil
}

// StartTrace starts the tracing session of dns events. This is a blocking call. It will not return until the trace is stopped.
func (l *ETWSession) StartTrace() error {
	return l.i.StartTrace(l.state)
}

// IsRunning returns true if DestroySession has NOT been called.
func (l *ETWSession) IsRunning() bool {
	return !l.shutdownGuard.Load()
}

// FlushTrace flushes the trace buffer.
func (l *ETWSession) FlushTrace() error {
	if l.i == nil {
		return fmt.Errorf("session not initialized")
	}

	l.shutdownMutex.Lock()
	defer l.shutdownMutex.Unlock()

	// Make sure session is still running.
	if l.shutdownGuard.Load() {
		return nil
	}

	return l.i.FlushTrace(l.state)
}

// StopTrace stops the trace. This will cause StartTrace to return.
func (l *ETWSession) StopTrace() error {
	return l.i.StopTrace(l.state)
}

// DestroySession closes the session and frees the allocated memory. Listener cannot be used after this function is called.
func (l *ETWSession) DestroySession() error {
	if l.i == nil {
		return fmt.Errorf("session not initialized")
	}
	l.shutdownMutex.Lock()
	defer l.shutdownMutex.Unlock()

	if l.shutdownGuard.Swap(true) {
		return nil
	}

	err := l.i.DestroySession(l.state)
	if err != nil {
		return err
	}
	l.state = 0
	return nil
}
