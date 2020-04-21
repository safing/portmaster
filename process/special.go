package process

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
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
)

// GetUnidentifiedProcess returns the special process assigned to unidentified processes.
func GetUnidentifiedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, UnidentifiedProcessID, unidentifiedProcess, profile.GetUnidentifiedProfile)
}

// GetSystemProcess returns the special process used for the Kernel.
func GetSystemProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, SystemProcessID, systemProcess, profile.GetSystemProfile)
}

func getSpecialProcess(ctx context.Context, pid int, template *Process, getProfile func() *profile.Profile) *Process {
	// check storage
	p, ok := GetProcessFromStorage(pid)
	if ok {
		return p
	}

	// assign template
	p = template

	p.Lock()
	defer p.Unlock()

	if p.FirstSeen == 0 {
		p.FirstSeen = time.Now().Unix()
	}

	// only find profiles if not already done.
	if p.profile != nil {
		log.Tracer(ctx).Trace("process: special profile already loaded")
		// mark profile as used
		p.profile.MarkUsed()
		return p
	}
	log.Tracer(ctx).Trace("process: loading special profile")

	// get profile
	localProfile := getProfile()

	// mark profile as used
	localProfile.MarkUsed()

	p.LocalProfileKey = localProfile.Key()
	p.profile = profile.NewLayeredProfile(localProfile)

	go p.Save()
	return p
}
