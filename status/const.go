package status

// Definitions of Security and Status Levels
const (
	SecurityLevelOff uint8 = 0

	SecurityLevelDynamic  uint8 = 1
	SecurityLevelSecure   uint8 = 2
	SecurityLevelFortress uint8 = 4

	SecurityLevelsDynamicAndSecure   uint8 = SecurityLevelDynamic | SecurityLevelSecure
	SecurityLevelsDynamicAndFortress uint8 = SecurityLevelDynamic | SecurityLevelFortress
	SecurityLevelsSecureAndFortress  uint8 = SecurityLevelSecure | SecurityLevelFortress
	SecurityLevelsAll                uint8 = SecurityLevelDynamic | SecurityLevelSecure | SecurityLevelFortress

	StatusOff     uint8 = 0
	StatusError   uint8 = 1
	StatusWarning uint8 = 2
	StatusOk      uint8 = 3
)
