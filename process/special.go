package process

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

var (
	// unidentifiedProcess is used when a process cannot be found.
	unidentifiedProcess = &Process{
		UserID:    -1,
		UserName:  "Unknown",
		Pid:       -1,
		ParentPid: -1,
		Name:      "Unidentified Processes",
	}

	// systemProcess is used to represent the Kernel.
	systemProcess = &Process{
		UserID:    0,
		UserName:  "Kernel",
		Pid:       0,
		ParentPid: 0,
		Name:      "Operating System",
	}
)

func GetUnidentifiedProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, unidentifiedProcess, profile.GetUnidentifiedProfile)
}

func GetSystemProcess(ctx context.Context) *Process {
	return getSpecialProcess(ctx, systemProcess, profile.GetSystemProfile)
}

func getSpecialProcess(ctx context.Context, p *Process, getProfile func() *profile.Profile) *Process {
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
