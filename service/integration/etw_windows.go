//go:build windows
// +build windows

package integration

import (
	"fmt"

	"golang.org/x/sys/windows"
)

type ETWFunctions struct {
	createState       *windows.Proc
	initializeSession *windows.Proc
	startTrace        *windows.Proc
	flushTrace        *windows.Proc
	stopTrace         *windows.Proc
	destroySession    *windows.Proc
	stopOldSession    *windows.Proc
}

func initializeETW(dll *windows.DLL) (*ETWFunctions, error) {
	functions := &ETWFunctions{}
	var err error
	functions.createState, err = dll.FindProc("PM_ETWCreateState")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWCreateState: %q", err)
	}
	functions.initializeSession, err = dll.FindProc("PM_ETWInitializeSession")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWInitializeSession: %q", err)
	}
	functions.startTrace, err = dll.FindProc("PM_ETWStartTrace")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWStartTrace: %q", err)
	}
	functions.flushTrace, err = dll.FindProc("PM_ETWFlushTrace")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWFlushTrace: %q", err)
	}
	functions.stopTrace, err = dll.FindProc("PM_ETWStopTrace")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWStopTrace: %q", err)
	}
	functions.destroySession, err = dll.FindProc("PM_ETWDestroySession")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWDestroySession: %q", err)
	}
	functions.stopOldSession, err = dll.FindProc("PM_ETWStopOldSession")
	if err != nil {
		return functions, fmt.Errorf("failed to load function PM_ETWDestroySession: %q", err)
	}
	return functions, nil
}

// CreateState calls the dll createState C function.
func (etw ETWFunctions) CreateState(callback uintptr) uintptr {
	state, _, _ := etw.createState.Call(callback)
	return state
}

// InitializeSession calls the dll initializeSession C function.
func (etw ETWFunctions) InitializeSession(state uintptr) error {
	rc, _, _ := etw.initializeSession.Call(state)
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}

// StartTrace calls the dll startTrace C function.
func (etw ETWFunctions) StartTrace(state uintptr) error {
	rc, _, _ := etw.startTrace.Call(state)
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}

// FlushTrace calls the dll flushTrace C function.
func (etw ETWFunctions) FlushTrace(state uintptr) error {
	rc, _, _ := etw.flushTrace.Call(state)
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}

// StopTrace calls the dll stopTrace C function.
func (etw ETWFunctions) StopTrace(state uintptr) error {
	rc, _, _ := etw.stopTrace.Call(state)
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}

// DestroySession calls the dll destroySession C function.
func (etw ETWFunctions) DestroySession(state uintptr) error {
	rc, _, _ := etw.destroySession.Call(state)
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}

// StopOldSession calls the dll stopOldSession C function.
func (etw ETWFunctions) StopOldSession() error {
	rc, _, _ := etw.stopOldSession.Call()
	if rc != 0 {
		return fmt.Errorf("failed with status code: %d", rc)
	}
	return nil
}
