package process

import (
	"context"
	"strconv"
	"time"

	"github.com/safing/portbase/log"
	"golang.org/x/sync/singleflight"
)

// Special Process IDs
const (
	UnidentifiedProcessID = -1
	SystemProcessID       = 0
)

var (
	// unidentifiedProcess is used when a process cannot be found.
	unidentifiedProcess = &Process{
		UserID:    UnidentifiedProcessID,
		UserName:  "Unknown",
		Pid:       UnidentifiedProcessID,
		ParentPid: UnidentifiedProcessID,
		Name:      "Unidentified Processes",
	}

	// systemProcess is used to represent the Kernel.
	systemProcess = &Process{
		UserID:    SystemProcessID,
		UserName:  "Kernel",
		Pid:       SystemProcessID,
		ParentPid: SystemProcessID,
		Name:      "Operating System",
	}

	getSpecialProcessSingleInflight singleflight.Group
)

// GetUnidentifiedProcess returns the special process assigned to unidentified processes.
func GetUnidentifiedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, UnidentifiedProcessID, unidentifiedProcess)
}

// GetSystemProcess returns the special process used for the Kernel.
func GetSystemProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, SystemProcessID, systemProcess)
}

func getSpecialProcess(ctx context.Context, pid int, template *Process) *Process {
	p, _, _ := getSpecialProcessSingleInflight.Do(strconv.Itoa(pid), func() (interface{}, error) {
		// Check if we have already loaded the special process.
		process, ok := GetProcessFromStorage(pid)
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
	return p.(*Process)
}
