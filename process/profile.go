package process

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

var (
	ownPID = os.Getpid()
)

// GetProfile finds and assigns a profile set to the process.
func (p *Process) GetProfile(ctx context.Context) (changed bool, err error) {
	// Update profile metadata outside of *Process lock.
	defer p.UpdateProfileMetadata()

	p.Lock()
	defer p.Unlock()

	// Check if profile is already loaded.
	if p.profile != nil {
		log.Tracer(ctx).Trace("process: profile already loaded")
		return
	}

	// If not, continue with loading the profile.
	log.Tracer(ctx).Trace("process: loading profile")

	// Check if we need a special profile.
	profileID := ""
	switch p.Pid {
	case UnidentifiedProcessID:
		profileID = profile.UnidentifiedProfileID
	case SystemProcessID:
		profileID = profile.SystemProfileID
	case ownPID:
		profileID = profile.PortmasterProfileID
	default:
		// Check if this is another Portmaster component.
		if updatesPath != "" && strings.HasPrefix(p.Path, updatesPath) {
			switch {
			case strings.Contains(p.Path, "portmaster-app"):
				profileID = profile.PortmasterAppProfileID
			case strings.Contains(p.Path, "portmaster-notifier"):
				profileID = profile.PortmasterNotifierProfileID
			default:
				// Unexpected binary from within the Portmaster updates directpry.
				log.Warningf("process: unexpected binary in the updates directory: %s", p.Path)
				// TODO: Assign a fully restricted profile in the future when we are
				// sure that we won't kill any of our own things.
			}
		}
		// Check if this is the system resolver.
		switch runtime.GOOS {
		case "windows":
			if (p.Path == `C:\Windows\System32\svchost.exe` || p.Path == `C:\Windows\system32\svchost.exe`) &&
				(strings.Contains(p.SpecialDetail, "Dnscache") || strings.Contains(p.CmdLine, "-k NetworkService")) {
				profileID = profile.SystemResolverProfileID
			}
		case "linux":
			switch p.Path {
			case "/lib/systemd/systemd-resolved",
				"/usr/lib/systemd/systemd-resolved",
				"/lib64/systemd/systemd-resolved",
				"/usr/lib64/systemd/systemd-resolved":
				profileID = profile.SystemResolverProfileID
			}
		}
	}

	// Get the (linked) local profile.
	localProfile, err := profile.GetProfile(profile.SourceLocal, profileID, p.Path)
	if err != nil {
		return false, err
	}

	// Assign profile to process.
	p.LocalProfileKey = localProfile.Key()
	p.profile = localProfile.LayeredProfile()

	return true, nil
}

// UpdateProfileMetadata updates the metadata of the local profile
// as required.
func (p *Process) UpdateProfileMetadata() {
	// Check if there is a profile to work with.
	localProfile := p.Profile().LocalProfile()
	if localProfile == nil {
		return
	}

	// Update metadata of profile.
	metadataUpdated := localProfile.UpdateMetadata(p.Path)

	// Mark profile as used.
	profileChanged := localProfile.MarkUsed()

	// Save the profile if we changed something.
	if metadataUpdated || profileChanged {
		err := localProfile.Save()
		if err != nil {
			log.Warningf("process: failed to save profile %s: %s", localProfile.ScopedID(), err)
		}
	}
}
