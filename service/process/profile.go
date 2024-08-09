package process

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile"
)

var ownPID = os.Getpid()

// GetProfile finds and assigns a profile set to the process.
func (p *Process) GetProfile(ctx context.Context) (changed bool, err error) {
	p.Lock()
	defer p.Unlock()

	// Check if profile is already loaded.
	if p.profile != nil {
		log.Tracer(ctx).Trace("process: profile already loaded")
		return
	}

	// If not, continue with loading the profile.
	log.Tracer(ctx).Trace("process: loading profile")

	// Get special or regular profile.
	localProfile, err := profile.GetLocalProfile(p.getSpecialProfileID(), p.MatchingData(), p.CreateProfileCallback)
	if err != nil {
		return false, fmt.Errorf("failed to find profile: %w", err)
	}

	// Assign profile to process.
	p.PrimaryProfileID = localProfile.ScopedID()
	p.profile = localProfile.LayeredProfile()

	return true, nil
}

// RefetchProfile removes the profile and finds and assigns a new profile.
func (p *Process) RefetchProfile(ctx context.Context) error {
	p.Lock()
	defer p.Unlock()

	// Get special or regular profile.
	localProfile, err := profile.GetLocalProfile(p.getSpecialProfileID(), p.MatchingData(), p.CreateProfileCallback)
	if err != nil {
		return fmt.Errorf("failed to find profile: %w", err)
	}

	// Assign profile to process.
	p.PrimaryProfileID = localProfile.ScopedID()
	p.profile = localProfile.LayeredProfile()

	return nil
}

// getSpecialProfileID returns the special profile ID for the process, if any.
func (p *Process) getSpecialProfileID() (specialProfileID string) {
	// Check if we need a special profile.
	switch p.Pid {
	case UnidentifiedProcessID:
		specialProfileID = profile.UnidentifiedProfileID
	case UnsolicitedProcessID:
		specialProfileID = profile.UnsolicitedProfileID
	case SystemProcessID:
		specialProfileID = profile.SystemProfileID
	case ownPID:
		specialProfileID = profile.PortmasterProfileID
	default:
		// Check if this is another Portmaster component.
		if updatesPath != "" && strings.HasPrefix(p.Path, updatesPath) {
			switch {
			case strings.Contains(p.Path, "portmaster-app"):
				specialProfileID = profile.PortmasterAppProfileID
			case strings.Contains(p.Path, "portmaster-notifier"):
				specialProfileID = profile.PortmasterNotifierProfileID
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
			// Depending on the OS version System32 may be capitalized or not.
			if (p.Path == `C:\Windows\System32\svchost.exe` ||
				p.Path == `C:\Windows\system32\svchost.exe`) &&
				// This comes from the windows tasklist command and should be pretty consistent.
				(profile.KeyAndValueInTags(p.Tags, "svchost", "Dnscache") ||
					// As an alternative in case of failure, we try to match the svchost.exe service parameter.
					strings.Contains(p.CmdLine, "-s Dnscache")) {
				specialProfileID = profile.SystemResolverProfileID
			}
		case "linux":
			switch p.Path {
			case "/lib/systemd/systemd-resolved",
				"/usr/lib/systemd/systemd-resolved",
				"/lib64/systemd/systemd-resolved",
				"/usr/lib64/systemd/systemd-resolved",
				"/usr/bin/nscd",
				"/usr/sbin/nscd",
				"/usr/bin/dnsmasq",
				"/usr/sbin/dnsmasq":
				specialProfileID = profile.SystemResolverProfileID
			}
		}
	}

	return specialProfileID
}
