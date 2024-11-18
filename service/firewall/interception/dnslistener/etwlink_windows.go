package dnslistener

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

type ETWSession struct {
	dll *windows.DLL

	createState       *windows.Proc
	initializeSession *windows.Proc
	startTrace        *windows.Proc
	flushTrace        *windows.Proc
	stopTrace         *windows.Proc
	destroySession    *windows.Proc

	shutdownGuard atomic.Bool
	shutdownMutex sync.Mutex

	state uintptr
}

// NewSession creates new ETW event listener and initilizes it. This is a low level interface, make sure to call DestorySession when you are done using it.
func NewSession(dllpath string, callback func(domain string, result string)) (*ETWSession, error) {
	etwListener := &ETWSession{}

	// Initialize dll functions
	var err error
	etwListener.dll, err = windows.LoadDLL(dllpath)
	if err != nil {
		return nil, fmt.Errorf("faild to load dll: %q", err)
	}
	etwListener.createState, err = etwListener.dll.FindProc("PM_ETWCreateState")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWCreateState: %q", err)
	}
	etwListener.initializeSession, err = etwListener.dll.FindProc("PM_ETWInitializeSession")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWInitializeSession: %q", err)
	}
	etwListener.startTrace, err = etwListener.dll.FindProc("PM_ETWStartTrace")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWStartTrace: %q", err)
	}
	etwListener.flushTrace, err = etwListener.dll.FindProc("PM_ETWFlushTrace")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWFlushTrace: %q", err)
	}
	etwListener.stopTrace, err = etwListener.dll.FindProc("PM_ETWStopTrace")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWStopTrace: %q", err)
	}
	etwListener.destroySession, err = etwListener.dll.FindProc("PM_ETWDestroySession")
	if err != nil {
		return nil, fmt.Errorf("faild to load function PM_ETWDestroySession: %q", err)
	}

	// Initialize notification activated callback
	win32Callback := windows.NewCallback(func(domain *uint16, result *uint16) uintptr {
		callback(windows.UTF16PtrToString(domain), windows.UTF16PtrToString(result))
		return 0
	})
	// The function only allocates memory it will not fail.
	etwListener.state, _, _ = etwListener.createState.Call(win32Callback)

	// Make sure DestroySession is called even if caller forgets to call it.
	runtime.SetFinalizer(etwListener, func(l *ETWSession) {
		_ = l.DestroySession()
	})

	// Initialize session.
	var rc uintptr
	rc, _, err = etwListener.initializeSession.Call(etwListener.state)
	if rc != 0 {
		return nil, fmt.Errorf("failed to initialzie session: error code: %q", rc)
	}

	return etwListener, nil
}

// StartTrace starts the tracing session of dns events. This is a blocking call. It will not return until the trace is stopped.
func (l *ETWSession) StartTrace() error {
	rc, _, _ := l.startTrace.Call(l.state)
	if rc != 0 {
		return fmt.Errorf("error code: %q", rc)
	}
	return nil
}

// IsRunning returns true if DestroySession has NOT been called.
func (l *ETWSession) IsRunning() bool {
	return !l.shutdownGuard.Load()
}

// FlushTrace flushes the trace buffer.
func (l *ETWSession) FlushTrace() error {
	l.shutdownMutex.Lock()
	defer l.shutdownMutex.Unlock()

	rc, _, _ := l.flushTrace.Call(l.state)
	if rc != 0 {
		return fmt.Errorf("error code: %q", rc)
	}
	return nil
}

// StopTrace stopes the trace. This will cause StartTrace to return.
func (l *ETWSession) StopTrace() error {
	rc, _, _ := l.stopTrace.Call(l.state)
	if rc != 0 {
		return fmt.Errorf("error code: %q", rc)
	}
	return nil
}

// DestroySession closes the session and frees the allocated memory. Listener cannot be used after this function is called.
func (l *ETWSession) DestroySession() error {
	if l.shutdownGuard.Load() {
		return nil
	}

	l.shutdownMutex.Lock()
	defer l.shutdownMutex.Unlock()

	l.shutdownGuard.Store(true)

	rc, _, _ := l.destroySession.Call(l.state)
	if rc != 0 {
		return fmt.Errorf("error code: %q", rc)
	}
	l.state = 0
	return nil
}
