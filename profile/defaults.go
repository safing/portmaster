package profile

import (
	"github.com/safing/portmaster/status"
)

func makeDefaultGlobalProfile() *Profile {
	return &Profile{
		ID:   "global",
		Name: "Global Profile",
	}
}

func makeDefaultFallbackProfile() *Profile {
	return &Profile{
		ID:   "fallback",
		Name: "Fallback Profile",
		Flags: map[uint8]uint8{
			// Profile Modes
			Blacklist: status.SecurityLevelDynamic,
			Prompt:    status.SecurityLevelSecure,
			Whitelist: status.SecurityLevelFortress,

			// Network Locations
			Internet:  status.SecurityLevelsDynamicAndSecure,
			LAN:       status.SecurityLevelsDynamicAndSecure,
			Localhost: status.SecurityLevelsAll,

			// Specials
			Related: status.SecurityLevelDynamic,
		},
		ServiceEndpoints: []*EndpointPermission{
			&EndpointPermission{
				Type:      EptAny,
				Protocol:  0,
				StartPort: 0,
				EndPort:   0,
				Permit:    false,
			},
		},
	}
}
