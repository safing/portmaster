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
	localProfile, err := profile.GetProfile(profile.SourceLocal, profileID, p.Path)
	if err != nil {
		return false, err
	}

	// Update metadata of profile.
	metadataUpdated := localProfile.UpdateMetadata(p.Name)

	// Mark profile as used.
	profileChanged := localProfile.MarkUsed()

	// Save the profile if we changed something.
	if metadataUpdated || profileChanged {
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
