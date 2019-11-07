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
			Blacklist: status.SecurityLevelsDynamicAndSecure,
			Whitelist: status.SecurityLevelFortress,

			// Network Locations
			Internet:  status.SecurityLevelsAll,
			LAN:       status.SecurityLevelDynamic,
			Localhost: status.SecurityLevelsAll,

			// Specials
			Related: status.SecurityLevelDynamic,
		},
		ServiceEndpoints: []*EndpointPermission{
			{
				Type:      EptAny,
				Protocol:  0,
				StartPort: 0,
				EndPort:   0,
				Permit:    false,
			},
		},
	}
}
