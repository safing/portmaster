package status

import (
	"github.com/safing/portbase/config"
)

// DisplayHintSecurityLevel is an external option hint for security levels.
// It's meant to be used as a value for config.DisplayHintAnnotation.
const DisplayHintSecurityLevel string = "security level"

// Security levels
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
		Name:  "Normal",
		Value: SecurityLevelsAll,
	},
	{
		Name:  "High",
		Value: SecurityLevelsHighAndExtreme,
	},
	{
		Name:  "Extreme",
		Value: SecurityLevelExtreme,
	},
}

// AllSecurityLevelValues is like SecurityLevelValues but also includes Off.
var AllSecurityLevelValues = append([]config.PossibleValue{
	{
		Name:  "Off",
		Value: SecurityLevelOff,
	},
},
	SecurityLevelValues...,
)

// Status constants
const (
	StatusOff     uint8 = 0
	StatusError   uint8 = 1
	StatusWarning uint8 = 2
	StatusOk      uint8 = 3
)
