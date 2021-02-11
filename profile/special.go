package profile

const (
	// UnidentifiedProfileID is the profile ID used for unidentified processes.
	UnidentifiedProfileID = "_unidentified"

	// SystemProfileID is the profile ID used for the system/kernel.
	SystemProfileID = "_system"

	// PortmasterProfileID is the profile ID used for the Portmaster Core itself.
	PortmasterProfileID = "_portmaster"

	// PortmasterAppProfileID is the profile ID used for the Portmaster App.
	PortmasterAppProfileID = "_portmaster-app"

	// PortmasterNotifierProfileID is the profile ID used for the Portmaster Notifier.
	PortmasterNotifierProfileID = "_portmaster-notifier"
)

func getSpecialProfile(profileID, linkedPath string) *Profile {
	switch profileID {
	case UnidentifiedProfileID:
		return New(SourceLocal, UnidentifiedProfileID, linkedPath)

	case SystemProfileID:
		return New(SourceLocal, SystemProfileID, linkedPath)

	case PortmasterProfileID:
		profile := New(SourceLocal, PortmasterProfileID, linkedPath)
		profile.Name = "Portmaster Core Service"
		profile.Internal = true
		return profile

	case PortmasterAppProfileID:
		profile := New(SourceLocal, PortmasterAppProfileID, linkedPath)
		profile.Name = "Portmaster User Interface"
		profile.Internal = true
		profile.Config = map[string]interface{}{
			CfgOptionDefaultActionKey: "block",
			CfgOptionEndpointsKey: []string{
				"+ Localhost",
			},
		}
		return profile

	case PortmasterNotifierProfileID:
		profile := New(SourceLocal, PortmasterNotifierProfileID, linkedPath)
		profile.Name = "Portmaster Notifier"
		profile.Internal = true
		profile.Config = map[string]interface{}{
			CfgOptionDefaultActionKey: "block",
			CfgOptionEndpointsKey: []string{
				"+ Localhost",
			},
		}
		return profile

	default:
		return nil
	}
}
