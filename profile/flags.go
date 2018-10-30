package profile

import (
	"errors"
	"strings"

	"github.com/Safing/portmaster/status"
)

// ProfileFlags are used to quickly add common attributes to profiles
type ProfileFlags map[uint8]uint8

// Profile Flags
const (
	// Profile Modes
	Prompt    uint8 = 0 // Prompt first-seen connections
	Blacklist uint8 = 1 // Allow everything not explicitly denied
	Whitelist uint8 = 2 // Only allow everything explicitly allowed

	// Network Locations
	Internet  uint8 = 16 // Allow connections to the Internet
	LAN       uint8 = 17 // Allow connections to the local area network
	Localhost uint8 = 18 // Allow connections on the local host

	// Specials
	Related       uint8 = 32 // If and before prompting, allow domains that are related to the program
	PeerToPeer    uint8 = 33 // Allow program to directly communicate with peers, without resolving DNS first
	Service       uint8 = 34 // Allow program to accept incoming connections
	Independent   uint8 = 35 // Ignore profile settings coming from the Community
	RequireGate17 uint8 = 36 // Require all connections to go over Gate17
)

var (
	// ErrProfileFlagsParseFailed is returned if a an invalid flag is encountered while parsing
	ErrProfileFlagsParseFailed = errors.New("profiles: failed to parse flags")

	sortedFlags = []uint8{
		Prompt,
		Blacklist,
		Whitelist,
		Internet,
		LAN,
		Localhost,
		Related,
		PeerToPeer,
		Service,
		Independent,
		RequireGate17,
	}

	flagIDs = map[string]uint8{
		"Prompt":        Prompt,
		"Blacklist":     Blacklist,
		"Whitelist":     Whitelist,
		"Internet":      Internet,
		"LAN":           LAN,
		"Localhost":     Localhost,
		"Related":       Related,
		"PeerToPeer":    PeerToPeer,
		"Service":       Service,
		"Independent":   Independent,
		"RequireGate17": RequireGate17,
	}

	flagNames = map[uint8]string{
		Prompt:        "Prompt",
		Blacklist:     "Blacklist",
		Whitelist:     "Whitelist",
		Internet:      "Internet",
		LAN:           "LAN",
		Localhost:     "Localhost",
		Related:       "Related",
		PeerToPeer:    "PeerToPeer",
		Service:       "Service",
		Independent:   "Independent",
		RequireGate17: "RequireGate17",
	}
)

// FlagsFromNames creates ProfileFlags from a comma seperated list of flagnames (e.g. "System,Strict,Secure")
// func FlagsFromNames(words []string) (*ProfileFlags, error) {
// 	var flags ProfileFlags
// 	for _, entry := range words {
// 		flag, ok := flagIDs[entry]
// 		if !ok {
// 			return nil, ErrProfileFlagsParseFailed
// 		}
// 		flags = append(flags, flag)
// 	}
// 	return &flags, nil
// }

// IsSet returns whether the ProfileFlags object is "set".
func (pf ProfileFlags) IsSet() bool {
	if pf != nil {
		return true
	}
	return false
}

// Has checks if a ProfileFlags object has a flag set in the given security level
func (pf ProfileFlags) Has(flag, level uint8) bool {
	setting, ok := pf[flag]
	if ok && setting&level > 0 {
		return true
	}
	return false
}

func getLevelMarker(levels, level uint8) string {
	if levels&level > 0 {
		return "+"
	}
	return "-"
}

// String return a string representation of ProfileFlags
func (pf ProfileFlags) String() string {
	var namedFlags []string
	for _, flag := range sortedFlags {
		levels, ok := pf[flag]
		if ok {
			s := flagNames[flag]
			if levels != status.SecurityLevelsAll {
				s += getLevelMarker(levels, status.SecurityLevelDynamic)
				s += getLevelMarker(levels, status.SecurityLevelSecure)
				s += getLevelMarker(levels, status.SecurityLevelFortress)
			}
		}
	}
	for _, flag := range pf {
		namedFlags = append(namedFlags, flagNames[flag])
	}
	return strings.Join(namedFlags, ", ")
}

// Add adds a flag to the Flags with the given level.
func (pf ProfileFlags) Add(flag, levels uint8) {
	pf[flag] = levels
}

// Remove removes a flag from the Flags.
func (pf ProfileFlags) Remove(flag uint8) {
	delete(pf, flag)
}
