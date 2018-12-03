package profile

import (
	"time"

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
		Ports: map[int16][]*Port{
			6: []*Port{
				&Port{ // SSH
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   22,
					End:     22,
				},
				&Port{ // HTTP
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   80,
					End:     80,
				},
				&Port{ // HTTPS
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   443,
					End:     443,
				},
				&Port{ // SMTP (TLS)
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   465,
					End:     465,
				},
				&Port{ // SMTP (STARTTLS)
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   587,
					End:     587,
				},
				&Port{ // IMAP (TLS)
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   993,
					End:     993,
				},
				&Port{ // IMAP (STARTTLS)
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   143,
					End:     143,
				},
			},
		},
	}
}
