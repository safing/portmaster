package process

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

// GetProfile finds and assigns a profile set to the process.
func (p *Process) GetProfile(ctx context.Context) error {
	p.Lock()
	defer p.Unlock()

	// only find profiles if not already done.
	if p.profile != nil {
		log.Tracer(ctx).Trace("process: profile already loaded")
		// mark profile as used
		p.profile.MarkUsed()
		return nil
	}
	log.Tracer(ctx).Trace("process: loading profile")

	// get profile
	localProfile, new, err := profile.FindOrCreateLocalProfileByPath(p.Path)
	if err != nil {
		return err
	}
	// add more information if new
	if new {
		localProfile.Name = p.ExecName
	}

	// mark profile as used
	localProfile.MarkUsed()

	p.LocalProfileKey = localProfile.Key()
	p.profile = profile.NewLayeredProfile(localProfile)

	go p.Save()
	return nil
}
