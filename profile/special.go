package profile

import (
	"fmt"
	"time"

	"github.com/safing/portbase/notifications"

	"github.com/safing/portbase/log"
)

const (
	// UnidentifiedProfileID is the profile ID used for unidentified processes.
	UnidentifiedProfileID = "_unidentified"
	// UnidentifiedProfileName is the name used for unidentified processes.
	UnidentifiedProfileName = "Unidentified Processes"

	// SystemProfileID is the profile ID used for the system/kernel.
	SystemProfileID = "_system"
	// SystemProfileName is the name used for the system/kernel.
	SystemProfileName = "Operating System"

	// SystemResolverProfileID is the profile ID used for the system's DNS resolver.
	SystemResolverProfileID = "_system-resolver"
	// SystemResolverProfileName is the name used for the system's DNS resolver.
	SystemResolverProfileName = "System DNS Client"

	// PortmasterProfileID is the profile ID used for the Portmaster Core itself.
	PortmasterProfileID = "_portmaster"
	// PortmasterProfileName is the name used for the Portmaster Core itself.
	PortmasterProfileName = "Portmaster Core Service"

	// PortmasterAppProfileID is the profile ID used for the Portmaster App.
	PortmasterAppProfileID = "_portmaster-app"
	// PortmasterAppProfileName is the name used for the Portmaster App.
	PortmasterAppProfileName = "Portmaster User Interface"

	// PortmasterNotifierProfileID is the profile ID used for the Portmaster Notifier.
	PortmasterNotifierProfileID = "_portmaster-notifier"
	// PortmasterNotifierProfileName is the name used for the Portmaster Notifier.
	PortmasterNotifierProfileName = "Portmaster Notifier"
)

func updateSpecialProfileMetadata(profile *Profile, binaryPath string) (ok, changed bool) {
	// Get new profile name and check if profile is applicable to special handling.
	var newProfileName string
	switch profile.ID {
	case UnidentifiedProfileID:
		newProfileName = UnidentifiedProfileName
	case SystemProfileID:
		newProfileName = SystemProfileName
	case SystemResolverProfileID:
		newProfileName = SystemResolverProfileName
	case PortmasterProfileID:
		newProfileName = PortmasterProfileName
	case PortmasterAppProfileID:
		newProfileName = PortmasterAppProfileName
	case PortmasterNotifierProfileID:
		newProfileName = PortmasterNotifierProfileName
	default:
		return false, false
	}

	// Update profile name if needed.
	if profile.Name != newProfileName {
		profile.Name = newProfileName
		changed = true
	}

	// Update LinkedPath to new value.
	if profile.LinkedPath != binaryPath {
		profile.LinkedPath = binaryPath
		changed = true
	}

	return true, changed
}

func getSpecialProfile(profileID, linkedPath string) *Profile {
	switch profileID {
	case UnidentifiedProfileID:
		return New(SourceLocal, UnidentifiedProfileID, linkedPath, nil)

	case SystemProfileID:
		return New(SourceLocal, SystemProfileID, linkedPath, nil)

	case SystemResolverProfileID:
		return New(
			SourceLocal,
			SystemResolverProfileID,
			linkedPath,
			map[string]interface{}{
				CfgOptionServiceEndpointsKey: []string{
					"+ Localhost",    // Allow everything from localhost.
					"+ LAN UDP/5353", // Allow inbound mDNS requests and multicast replies.
					"+ LAN UDP/5355", // Allow inbound LLMNR requests and multicast replies.
					"+ LAN UDP/1900", // Allow inbound SSDP requests and multicast replies.
				},
			},
		)

	case PortmasterProfileID:
		profile := New(SourceLocal, PortmasterProfileID, linkedPath, nil)
		profile.Internal = true
		return profile

	case PortmasterAppProfileID:
		profile := New(
			SourceLocal,
			PortmasterAppProfileID,
			linkedPath,
			map[string]interface{}{
				CfgOptionDefaultActionKey: "block",
				CfgOptionEndpointsKey: []string{
					"+ Localhost",
				},
			},
		)
		profile.Internal = true
		return profile

	case PortmasterNotifierProfileID:
		profile := New(
			SourceLocal,
			PortmasterNotifierProfileID,
			linkedPath,
			map[string]interface{}{
				CfgOptionDefaultActionKey: "block",
				CfgOptionEndpointsKey: []string{
					"+ Localhost",
				},
			},
		)
		profile.Internal = true
		return profile

	default:
		return nil
	}
}

// specialProfileNeedsReset is used as a workaround until we can properly use
// profile layering in a way that it is also correctly handled by the UI. We
// check if the special profile has not been changed by the user and if not,
// check if the profile is outdated and can be upgraded.
func specialProfileNeedsReset(profile *Profile) bool {
	switch {
	case profile.Source != SourceLocal:
		// Special profiles live in the local scope only.
		return false
	case profile.LastEdited > 0:
		// Profile was edited - don't override user settings.
		return false
	}

	switch profile.ID {
	case SystemResolverProfileID:
		return canBeUpgraded(profile, "18.5.2021")
	default:
		// Not a special profile or no upgrade available yet.
		return false
	}
}

func canBeUpgraded(profile *Profile, upgradeDate string) bool {
	// Parse upgrade date.
	upgradeTime, err := time.Parse("2.1.2006", upgradeDate)
	if err != nil {
		log.Warningf("profile: failed to parse date %q: %s", upgradeDate, err)
		return false
	}

	// Check if the upgrade is applicable.
	if profile.Meta().Created < upgradeTime.Unix() {
		log.Infof("profile: upgrading special profile %s", profile.ScopedID())

		notifications.NotifyInfo(
			"profiles:upgraded-special-profile-"+profile.ID,
			profile.Name+" Settings Upgraded",
			// TODO: Remove disclaimer.
			fmt.Sprintf(
				"The %s settings were automatically upgraded. The current app settings have been replaced, as the Portmaster did not detect any changes made by you. Please note that settings upgrades before June 2021 might not detect previous changes correctly and you might want to review the new settings.",
				profile.Name,
			),
			notifications.Action{
				ID:   "ack",
				Text: "OK",
			},
			notifications.Action{
				Text:    "Open Settings",
				Type:    notifications.ActionTypeOpenProfile,
				Payload: profile.ScopedID(),
			},
		)

		return true
	}

	return false
}
