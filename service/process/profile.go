package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		if p.IsPortmasterUi(context.Background()) {
			specialProfileID = profile.PortmasterAppProfileID
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

// IsPortmasterUi checks if the process is the Portmaster UI or its child (up to 3 parent levels).
func (p *Process) IsPortmasterUi(ctx context.Context) bool {
	if module.portmasterUIPath == "" {
		return false
	}

	// Find parent for up to two levels, if we don't match the path.
	const checkLevels = 3

	var previousPid int
	proc := p

	hasPmWebviewEnvVar := false

	for i := 0; i < checkLevels; i++ {
		if proc.Pid == UnidentifiedProcessID || proc.Pid == SystemProcessID {
			break
		}

		realPath, err := filepath.EvalSymlinks(proc.Path)
		if err == nil && realPath == module.portmasterUIPath {
			if runtime.GOOS != "windows" {
				return true
			}

			// On Windows, avoid false positive detection of the Portmaster UI.
			// For example:
			//   There may be cases where a system browser is launched from the Portmaster UI,
			//   making it a child of the Portmaster UI process (e.g., user clicked a link in the UI).
			//   In this case, the parent process tree may look like this:
			//       Portmaster.exe
			//       ├─ WebView  (PM UI)
			//       │   └─ WebView (PM UI child)
			//       └─ System Web Browser ...
			//
			// To ensure that 'p' is the actual Portmaster UI process, we check for the presence
			// of the 'PORTMASTER_UI_WEBVIEW_PROCESS' environment variable in the process and its parents.
			// If the env var is set, we are a child (WebView window) of the Portmaster UI process.
			// Otherwise, the process was launched by the Portmaster UI, but should not be trusted as the Portmaster UI process.
			if i == 0 {
				return true // We are the main Portmaster UI process.
			}
			if hasPmWebviewEnvVar {
				return true // We are a WebView window of the Portmaster UI process.
			}
			// The process was launched by the Portmaster UI, but should not be trusted as the Portmaster UI process.
			log.Tracer(ctx).Warningf("process: %d %q is a child of the Portmaster UI, but does not have the PORTMASTER_UI_WEBVIEW_PROCESS environment variable set. Ignoring.", p.Pid, p.Path)
			return false
		}

		// Check if the process has the environment variable set.
		//
		// It is OK to check for the existence of the environment variable in all
		// processes in the parent chain (on all loop iterations). This increases the
		// chance of correct detection, even if a child or grandchild WebView process
		// did not inherit the environment variable for some reason.
		if _, ok := proc.Env["PORTMASTER_UI_WEBVIEW_PROCESS"]; ok {
			hasPmWebviewEnvVar = true
		}

		if i < checkLevels-1 { // no need to check parent if we are at the last level
			previousPid = proc.Pid
			proc, err = GetOrFindProcess(ctx, proc.ParentPid)
			if err != nil || proc.Pid == previousPid {
				break
			}
		}
	}

	return false
}
