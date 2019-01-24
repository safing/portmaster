package profile

import (
	"github.com/Safing/portmaster/status"
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
			Related:    status.SecurityLevelDynamic,
			PeerToPeer: status.SecurityLevelDynamic,
		},
		ServiceEndpoints: []*EndpointPermission{
			&EndpointPermission{
				DomainOrIP: "",
				Wildcard:   true,
				Protocol:   0,
				StartPort:  0,
				EndPort:    0,
				Permit:     false,
			},
		},
	}
}
