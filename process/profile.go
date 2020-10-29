package process

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

// GetProfile finds and assigns a profile set to the process.
func (p *Process) GetProfile(ctx context.Context) (changed bool, err error) {
	p.Lock()
	defer p.Unlock()

	// only find profiles if not already done.
	if p.profile != nil {
		log.Tracer(ctx).Trace("process: profile already loaded")
		// Mark profile as used.
		p.profile.MarkUsed()
		return false, nil
	}
	log.Tracer(ctx).Trace("process: loading profile")

	// Check if we need a special profile.
	profileID := ""
	switch p.Pid {
	case UnidentifiedProcessID:
		profileID = profile.UnidentifiedProfileID
	case SystemProcessID:
		profileID = profile.SystemProfileID
	}

	// Get the (linked) local profile.
	localProfile, new, err := profile.GetProfile(profile.SourceLocal, profileID, p.Path)
	if err != nil {
		return false, err
	}

	// If the local profile is new, add some information from the process.
	if new {
		localProfile.Name = p.ExecName

		// Special profiles will only have a name, but not an ExecName.
		if localProfile.Name == "" {
			localProfile.Name = p.Name
		}
	}

	// Mark profile as used.
	profileChanged := localProfile.MarkUsed()

	// Save the profile if we changed something.
	if new || profileChanged {
		err := localProfile.Save()
		if err != nil {
			log.Warningf("process: failed to save profile %s: %s", localProfile.ScopedID(), err)
		}
	}

	// Assign profile to process.
	p.LocalProfileKey = localProfile.Key()
	p.profile = localProfile.LayeredProfile()

	return true, nil
}
