package process

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/socket"
	"github.com/safing/portmaster/service/profile"
)

const (
	// UndefinedProcessID is not used by any (virtual) process and signifies that
	// the PID is unset.
	UndefinedProcessID = -1

	// UnidentifiedProcessID is the PID used for outgoing connections that could
	// not be attributed to a PID for any reason.
	UnidentifiedProcessID = -2

	// UnsolicitedProcessID is the PID used for incoming connections that could
	// not be attributed to a PID for any reason.
	UnsolicitedProcessID = -3

	// NetworkHostProcessID is the PID used for requests served to the network.
	NetworkHostProcessID = -255
)

func init() {
	// Check required matching values.
	if UndefinedProcessID != socket.UndefinedProcessID {
		panic("UndefinedProcessID does not match socket.UndefinedProcessID")
	}
}

var (
	// unidentifiedProcess is used for non-attributed outgoing connections.
	unidentifiedProcess = &Process{
		UserID:     UnidentifiedProcessID,
		UserName:   "Unknown",
		Pid:        UnidentifiedProcessID,
		ParentPid:  UnidentifiedProcessID,
		Name:       profile.UnidentifiedProfileName,
		processKey: getProcessKey(UnidentifiedProcessID, 0),
	}

	// unsolicitedProcess is used for non-attributed incoming connections.
	unsolicitedProcess = &Process{
		UserID:     UnsolicitedProcessID,
		UserName:   "Unknown",
		Pid:        UnsolicitedProcessID,
		ParentPid:  UnsolicitedProcessID,
		Name:       profile.UnsolicitedProfileName,
		processKey: getProcessKey(UnsolicitedProcessID, 0),
	}

	// systemProcess is used to represent the Kernel.
	systemProcess = &Process{
		UserID:     SystemProcessID,
		UserName:   "Kernel",
		Pid:        SystemProcessID,
		ParentPid:  SystemProcessID,
		Name:       profile.SystemProfileName,
		processKey: getProcessKey(SystemProcessID, 0),
	}

	getSpecialProcessSingleInflight singleflight.Group
)

// GetUnidentifiedProcess returns the special process assigned to non-attributed outgoing connections.
func GetUnidentifiedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, unidentifiedProcess)
}

// GetUnsolicitedProcess returns the special process assigned to non-attributed incoming connections.
func GetUnsolicitedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, unsolicitedProcess)
}

// GetSystemProcess returns the special process used for the Kernel.
func GetSystemProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, systemProcess)
}

func getSpecialProcess(ctx context.Context, template *Process) *Process {
	p, _, _ := getSpecialProcessSingleInflight.Do(template.processKey, func() (interface{}, error) {
		// Check if we have already loaded the special process.
		process, ok := GetProcessFromStorage(template.processKey)
		if ok {
			return process, nil
		}

		// Create new process from template
		process = template
		process.FirstSeen = time.Now().Unix()

		// Get profile.
		_, err := process.GetProfile(ctx)
		if err != nil {
			log.Tracer(ctx).Errorf("process: failed to get profile for process %s: %s", process, err)
		}

		// Save process to storage.
		process.Save()
		return process, nil
	})
	return p.(*Process) // nolint:forcetypeassert // Can only be a *Process.
}
