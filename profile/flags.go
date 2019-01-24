package profile

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Safing/portmaster/status"
)

// Flags are used to quickly add common attributes to profiles
type Flags map[uint8]uint8

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
	// ErrFlagsParseFailed is returned if a an invalid flag is encountered while parsing
	ErrFlagsParseFailed = errors.New("profiles: failed to parse flags")

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

// Check checks if a flag is set at all and if it's active in the given security level.
func (flags Flags) Check(flag, level uint8) (active bool, ok bool) {
	if flags == nil {
		return false, false
	}

	setting, ok := flags[flag]
	if ok {
		if setting&level > 0 {
			return true, true
		}
		return false, true
	}
	return false, false
}

func getLevelMarker(levels, level uint8) string {
	if levels&level > 0 {
		return "+"
	}
	return "-"
}

// String return a string representation of Flags
func (flags Flags) String() string {
	var markedFlags []string
	for _, flag := range sortedFlags {
		levels, ok := flags[flag]
		if ok {
			s := flagNames[flag]
			if levels != status.SecurityLevelsAll {
				s += getLevelMarker(levels, status.SecurityLevelDynamic)
				s += getLevelMarker(levels, status.SecurityLevelSecure)
				s += getLevelMarker(levels, status.SecurityLevelFortress)
			}
			markedFlags = append(markedFlags, s)
		}
	}
	return fmt.Sprintf("[%s]", strings.Join(markedFlags, ", "))
}

// Add adds a flag to the Flags with the given level.
func (flags Flags) Add(flag, levels uint8) {
	flags[flag] = levels
}

// Remove removes a flag from the Flags.
func (flags Flags) Remove(flag uint8) {
	delete(flags, flag)
}
