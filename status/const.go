package status

// Definitions of Security and Status Levels
const (
	SecurityLevelOff      uint8 = 0
	SecurityLevelDynamic  uint8 = 1
	SecurityLevelSecure   uint8 = 2
	SecurityLevelFortress uint8 = 3

	StatusOff     uint8 = 0
	StatusError   uint8 = 1
	StatusWarning uint8 = 2
	StatusOk      uint8 = 3
)
