package profile

const (
	// UnidentifiedProfileID is the profile ID used for unidentified processes.
	UnidentifiedProfileID = "_unidentified"
	// UnidentifiedProfileName is the name used for unidentified processes.
	UnidentifiedProfileName = "Unidentified Processes"

	// SystemProfileID is the profile ID used for the system/kernel.
	SystemProfileID = "_system"
	// SystemProfileName is the name used for the system/kernel.
	SystemProfileName = "Operating System"

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
