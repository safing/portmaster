package profile

import (
	"errors"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/status"
)

const (
	// UnidentifiedProfileID is the profile ID used for unidentified processes.
	UnidentifiedProfileID = "_unidentified"
	// UnidentifiedProfileName is the name used for unidentified processes.
	UnidentifiedProfileName = "Unidentified App"
	// UnidentifiedProfileDescription is the description used for unidentified processes.
	UnidentifiedProfileDescription = `Connections that could not be attributed to a specific app.

The Portmaster attributes connections (only TCP/UDP) to specific apps. When attribution for a connection fails, it ends up here.

Connections from unsupported protocols (like ICMP/"ping") are always collected here.
`

	// UnsolicitedProfileID is the profile ID used for unsolicited connections.
	UnsolicitedProfileID = "_unsolicited"
	// UnsolicitedProfileName is the name used for unsolicited connections.
	UnsolicitedProfileName = "Network Noise"
	// UnsolicitedProfileDescription is the description used for unsolicited connections.
	UnsolicitedProfileDescription = `Common connections coming from your Local Area Network.

Local Area Networks usually have quite a lot of traffic from applications that are trying to find things on the network. This might be a computer trying to find a printer, or a file sharing application searching for local peers. These network packets will automatically arrive at your device.

These connections - the "network noise" - can be found in this app.`

	// SystemProfileID is the profile ID used for the system/kernel.
	SystemProfileID = "_system"
	// SystemProfileName is the name used for the system/kernel.
	SystemProfileName = "Operating System"
	// SystemProfileDescription is the description used for the system/kernel.
	SystemProfileDescription = "This is the operation system itself."

	// SystemResolverProfileID is the profile ID used for the system's DNS resolver.
	SystemResolverProfileID = "_system-resolver"
	// SystemResolverProfileName is the name used for the system's DNS resolver.
	SystemResolverProfileName = "System DNS Client"
	// SystemResolverProfileDescription is the description used for the system's DNS resolver.
	SystemResolverProfileDescription = `The System DNS Client is a system service that requires special handling. For regular network connections, the configured settings will apply as usual, but DNS requests coming from the System DNS Client are handled in a special way, as they could actually be coming from any other application on the system.

In order to respect the app settings of the actual application, DNS requests from the System DNS Client are only subject to the following settings:

- Outgoing Rules (without global rules)
- Filter Lists

If you think you might have messed up the settings of the System DNS Client, just delete the profile below to reset it to the defaults.
`

	// PortmasterProfileID is the profile ID used for the Portmaster Core itself.
	PortmasterProfileID = "_portmaster"
	// PortmasterProfileName is the name used for the Portmaster Core itself.
	PortmasterProfileName = "Portmaster Core Service"
	// PortmasterProfileDescription is the description used for the Portmaster Core itself.
	PortmasterProfileDescription = `This is the Portmaster itself, which runs in the background as a system service. App specific settings have no effect.`

	// PortmasterAppProfileID is the profile ID used for the Portmaster App.
	PortmasterAppProfileID = "_portmaster-app"
	// PortmasterAppProfileName is the name used for the Portmaster App.
	PortmasterAppProfileName = "Portmaster User Interface"
	// PortmasterAppProfileDescription is the description used for the Portmaster App.
	PortmasterAppProfileDescription = `This is the Portmaster UI Windows.`

	// PortmasterNotifierProfileID is the profile ID used for the Portmaster Notifier.
	PortmasterNotifierProfileID = "_portmaster-notifier"
	// PortmasterNotifierProfileName is the name used for the Portmaster Notifier.
	PortmasterNotifierProfileName = "Portmaster Notifier"
	// PortmasterNotifierProfileDescription is the description used for the Portmaster Notifier.
	PortmasterNotifierProfileDescription = `This is the Portmaster UI Tray Notifier.`
)

// GetSpecialProfile fetches a special profile. This function ensures that the loaded profile
// is shared among all callers. Always provide all available data points.
func GetSpecialProfile(id string, path string) ( //nolint:gocognit
	profile *Profile,
	err error,
) {
	// Check if we have an ID.
	if id == "" {
		return nil, errors.New("cannot get special profile without ID")
	}
	scopedID := makeScopedID(SourceLocal, id)

	// Globally lock getting a profile.
	// This does not happen too often, and it ensures we really have integrity
	// and no race conditions.
	getProfileLock.Lock()
	defer getProfileLock.Unlock()

	// Check if there already is an active profile.
	var previousVersion *Profile
	profile = getActiveProfile(scopedID)
	if profile != nil {
		// Mark active and return if not outdated.
		if profile.outdated.IsNotSet() {
			profile.MarkStillActive()
			return profile, nil
		}

		// If outdated, get from database.
		previousVersion = profile
	}

	// Get special profile from DB and check if it needs a reset.
	var created bool
	profile, err = getProfile(scopedID)
	switch {
	case err == nil:
		// Reset profile if needed.
		if specialProfileNeedsReset(profile) {
			profile = createSpecialProfile(id, path)
			created = true
		}
	case !errors.Is(err, database.ErrNotFound):
		// Warn when fetching from DB fails, and create new profile as fallback.
		log.Warningf("profile: failed to get special profile %s: %s", id, err)
		fallthrough
	default:
		// Create new profile if it does not exist (or failed to load).
		profile = createSpecialProfile(id, path)
		created = true
	}
	// Check if creating the special profile was successful.
	if profile == nil {
		return nil, errors.New("given ID is not a special profile ID")
	}

	// Update metadata
	changed := updateSpecialProfileMetadata(profile, path)

	// Save if created or changed.
	if created || changed {
		err := profile.Save()
		if err != nil {
			log.Warningf("profile: failed to save special profile %s: %s", scopedID, err)
		}
	}

	// Prepare profile for first use.

	// If we are refetching, assign the layered profile from the previous version.
	// The internal references will be updated when the layered profile checks for updates.
	if previousVersion != nil && previousVersion.layeredProfile != nil {
		profile.layeredProfile = previousVersion.layeredProfile
	}

	// Profiles must have a layered profile, create a new one if it
	// does not yet exist.
	if profile.layeredProfile == nil {
		profile.layeredProfile = NewLayeredProfile(profile)
	}

	// Add the profile to the currently active profiles.
	addActiveProfile(profile)

	return profile, nil
}

func updateSpecialProfileMetadata(profile *Profile, binaryPath string) (changed bool) {
	// Get new profile name and check if profile is applicable to special handling.
	var newProfileName, newDescription string
	switch profile.ID {
	case UnidentifiedProfileID:
		newProfileName = UnidentifiedProfileName
		newDescription = UnidentifiedProfileDescription
	case UnsolicitedProfileID:
		newProfileName = UnsolicitedProfileName
		newDescription = UnsolicitedProfileDescription
	case SystemProfileID:
		newProfileName = SystemProfileName
		newDescription = SystemProfileDescription
	case SystemResolverProfileID:
		newProfileName = SystemResolverProfileName
		newDescription = SystemResolverProfileDescription
	case PortmasterProfileID:
		newProfileName = PortmasterProfileName
		newDescription = PortmasterProfileDescription
	case PortmasterAppProfileID:
		newProfileName = PortmasterAppProfileName
		newDescription = PortmasterAppProfileDescription
	case PortmasterNotifierProfileID:
		newProfileName = PortmasterNotifierProfileName
		newDescription = PortmasterNotifierProfileDescription
	default:
		return false
	}

	// Update profile name if needed.
	if profile.Name != newProfileName {
		profile.Name = newProfileName
		changed = true
	}

	// Update description if needed.
	if profile.Description != newDescription {
		profile.Description = newDescription
		changed = true
	}

	// Update PresentationPath to new value.
	if profile.PresentationPath != binaryPath {
		profile.PresentationPath = binaryPath
		changed = true
	}

	return changed
}

func createSpecialProfile(profileID string, path string) *Profile {
	switch profileID {
	case UnidentifiedProfileID:
		return New(&Profile{
			ID:               UnidentifiedProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
		})

	case UnsolicitedProfileID:
		return New(&Profile{
			ID:               UnsolicitedProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
		})

	case SystemProfileID:
		return New(&Profile{
			ID:               SystemProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
		})

	case SystemResolverProfileID:
		return New(&Profile{
			ID:               SystemResolverProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
			Config: map[string]interface{}{
				// Explicitly setting the default action to "permit" will improve the
				// user experience for people who set the global default to "prompt".
				// Resolved domain from the system resolver are checked again when
				// attributed to a connection of a regular process. Otherwise, users
				// would see two connection prompts for the same domain.
				CfgOptionDefaultActionKey: "permit",
				// Explicitly allow incoming connections.
				CfgOptionBlockInboundKey: status.SecurityLevelOff,
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
					"- *",            // Deny everything else.
				},
				// Explicitly disable all filter lists, as these will be checked later
				// with the attributed connection. As this is the system resolver, this
				// list can instead be used as a global enforcement of filter lists, if
				// the system resolver is used. Users who want to
				CfgOptionFilterListsKey: []string{},
			},
		})

	case PortmasterProfileID:
		return New(&Profile{
			ID:               PortmasterProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
			Internal:         true,
		})

	case PortmasterAppProfileID:
		return New(&Profile{
			ID:               PortmasterAppProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
			Config: map[string]interface{}{
				CfgOptionDefaultActionKey: "block",
				CfgOptionEndpointsKey: []string{
					"+ Localhost",
					"+ .safing.io",
				},
			},
			Internal: true,
		})

	case PortmasterNotifierProfileID:
		return New(&Profile{
			ID:               PortmasterNotifierProfileID,
			Source:           SourceLocal,
			PresentationPath: path,
			Config: map[string]interface{}{
				CfgOptionDefaultActionKey: "block",
				CfgOptionEndpointsKey: []string{
					"+ Localhost",
				},
			},
			Internal: true,
		})

	default:
		return nil
	}
}

// specialProfileNeedsReset is used as a workaround until we can properly use
// profile layering in a way that it is also correctly handled by the UI. We
// check if the special profile has not been changed by the user and if not,
// check if the profile is outdated and can be upgraded.
func specialProfileNeedsReset(profile *Profile) bool {
	if profile == nil {
		return false
	}

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
		return canBeUpgraded(profile, "21.10.2022")
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
