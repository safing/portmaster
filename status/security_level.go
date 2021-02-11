package status

import "github.com/safing/portbase/config"

type (
	// SecurityLevelOptionFunc can be called with a minimum security level
	// and returns whether or not a given security option is enabled or
	// not.
	// Use SecurityLevelOption() to get a SecurityLevelOptionFunc for a
	// specific option.
	SecurityLevelOptionFunc func(minSecurityLevel uint8) bool
)

// DisplayHintSecurityLevel is an external option hint for security levels.
// It's meant to be used as a value for config.DisplayHintAnnotation.
const DisplayHintSecurityLevel string = "security level"

// Security levels.
const (
	SecurityLevelOff     uint8 = 0
	SecurityLevelNormal  uint8 = 1
	SecurityLevelHigh    uint8 = 2
	SecurityLevelExtreme uint8 = 4

	SecurityLevelsNormalAndHigh    uint8 = SecurityLevelNormal | SecurityLevelHigh
	SecurityLevelsNormalAndExtreme uint8 = SecurityLevelNormal | SecurityLevelExtreme
	SecurityLevelsHighAndExtreme   uint8 = SecurityLevelHigh | SecurityLevelExtreme
	SecurityLevelsAll              uint8 = SecurityLevelNormal | SecurityLevelHigh | SecurityLevelExtreme
)

// SecurityLevelValues defines all possible security levels.
var SecurityLevelValues = []config.PossibleValue{
	{
		Name:        "Trusted / Home Network",
		Value:       SecurityLevelsAll,
		Description: "Setting is always enabled.",
	},
	{
		Name:        "Untrusted / Public Network",
		Value:       SecurityLevelsHighAndExtreme,
		Description: "Setting is enabled in untrusted and dangerous networks.",
	},
	{
		Name:        "Danger / Hacked Network",
		Value:       SecurityLevelExtreme,
		Description: "Setting is enabled only in dangerous networks.",
	},
}

// AllSecurityLevelValues is like SecurityLevelValues but also includes Off.
var AllSecurityLevelValues = append([]config.PossibleValue{
	{
		Name:        "Off",
		Value:       SecurityLevelOff,
		Description: "Setting is always disabled.",
	},
},
	SecurityLevelValues...,
)

// IsValidSecurityLevel returns true if level is a valid,
// single security level. Level is also invalid if it's a
// bitmask with more that one security level set.
func IsValidSecurityLevel(level uint8) bool {
	return level == SecurityLevelOff ||
		level == SecurityLevelNormal ||
		level == SecurityLevelHigh ||
		level == SecurityLevelExtreme
}

// IsValidSecurityLevelMask returns true if level is a valid
// security level mask. It's like IsValidSecurityLevel but
// also allows bitmask combinations.
func IsValidSecurityLevelMask(level uint8) bool {
	return level <= 7
}

func max(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}

// SecurityLevelOption returns a function to check if the option
// identified by name is active at a given minimum security level.
// The returned function is safe for concurrent use with configuration
// updates.
func SecurityLevelOption(name string) SecurityLevelOptionFunc {
	activeAtLevel := config.Concurrent.GetAsInt(name, int64(SecurityLevelsAll))
	return func(minSecurityLevel uint8) bool {
		return uint8(activeAtLevel())&max(ActiveSecurityLevel(), minSecurityLevel) > 0
	}
}

// SecurityLevelString returns the given security level as a string.
func SecurityLevelString(level uint8) string {
	switch level {
	case SecurityLevelOff:
		return "Off"
	case SecurityLevelNormal:
		return "Normal"
	case SecurityLevelHigh:
		return "High"
	case SecurityLevelExtreme:
		return "Extreme"
	case SecurityLevelsNormalAndHigh:
		return "Normal and High"
	case SecurityLevelsNormalAndExtreme:
		return "Normal and Extreme"
	case SecurityLevelsHighAndExtreme:
		return "High and Extreme"
	case SecurityLevelsAll:
		return "Normal, High and Extreme"
	default:
		return "INVALID"
	}
}
