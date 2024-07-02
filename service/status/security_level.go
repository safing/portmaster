package status

import "github.com/safing/portmaster/base/config"

// MigrateSecurityLevelToBoolean migrates a security level (int) option value to a boolean option value.
func MigrateSecurityLevelToBoolean(option *config.Option, value any) any {
	// Check new (target) option type.
	if option.OptType != config.OptTypeBool {
		// This migration converts to boolean.
		// Thus, conversion is not applicable.
		return value
	}

	// Convert value to uint8.
	var nVal uint8
	switch v := value.(type) {
	case int:
		nVal = uint8(v)
	case int8:
		nVal = uint8(v)
	case int16:
		nVal = uint8(v)
	case int32:
		nVal = uint8(v)
	case int64:
		nVal = uint8(v)
	case uint:
		nVal = uint8(v)
	case uint8:
		nVal = v
	case uint16:
		nVal = uint8(v)
	case uint32:
		nVal = uint8(v)
	case uint64:
		nVal = uint8(v)
	case float32:
		nVal = uint8(v)
	case float64:
		nVal = uint8(v)
	default:
		// Input type not compatible.
		return value
	}

	// Convert to boolean.
	return nVal&SecurityLevelNormal > 0
}

// DisplayHintSecurityLevel is an external option hint for security levels.
// It's meant to be used as a value for config.DisplayHintAnnotation.
const DisplayHintSecurityLevel string = "security level"

// Security levels.
const (
	SecurityLevelOff     uint8 = 0
	SecurityLevelNormal  uint8 = 1
	SecurityLevelHigh    uint8 = 2
	SecurityLevelExtreme uint8 = 4
)
