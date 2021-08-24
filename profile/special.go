package profile

import (
	"time"

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
		systemResolverProfile := New(
			SourceLocal,
			SystemResolverProfileID,
			linkedPath,
			map[string]interface{}{
				// Explicitly setting the default action to "permit" will improve the
				// user experience for people who set the global default to "prompt".
				// Resolved domain from the system resolver are checked again when
				// attributed to a connection of a regular process. Otherwise, users
				// would see two connection prompts for the same domain.
				CfgOptionDefaultActionKey: "permit",
				// Explicitly allow localhost and answers to multicast protocols that
				// are commonly used by system resolvers.
				// TODO: When the Portmaster gains the ability to attribute multicast
				// responses to their requests, these rules can probably be removed
				// again.
				CfgOptionServiceEndpointsKey: []string{
					"+ Localhost",    // Allow everything from localhost.
					"+ LAN UDP/5353", // Allow inbound mDNS requests and multicast replies.
					"+ LAN UDP/5355", // Allow inbound LLMNR requests and multicast replies.
					"+ LAN UDP/1900", // Allow inbound SSDP requests and multicast replies.
				},
				// Explicitly disable all filter lists, as these will be checked later
				// with the attributed connection. As this is the system resolver, this
				// list can instead be used as a global enforcement of filter lists, if
				// the system resolver is used. Users who want to
				CfgOptionFilterListsKey: []string{},
			},
		)
		// Add description to tell users about the quirks of this profile.
		systemResolverProfile.Description = `The System DNS Client is a system service that requires special handling. For regular network connections, the configured settings will apply as usual, but DNS requests coming from the System DNS Client are handled in a special way, as they could actually be coming from any other application on the system.
		
In order to respect the app settings of the actual application, DNS requests from the System DNS Client are only subject to the following settings:

- Outgoing Rules (without global rules)
- Block Bypassing
- Filter Lists
`
		return systemResolverProfile

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
					"+ .safing.io",
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
		return canBeUpgraded(profile, "1.6.2021")
	case PortmasterAppProfileID:
		return canBeUpgraded(profile, "8.9.2021")
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
	if profile.Created < upgradeTime.Unix() {
		log.Infof("profile: upgrading special profile %s", profile.ScopedID())
		return true
	}

	return false
}
