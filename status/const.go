package status

// Definitions of Security and Status Levels
const (
	SecurityLevelOff uint8 = 0

	SecurityLevelNormal  uint8 = 1
	SecurityLevelHigh    uint8 = 2
	SecurityLevelExtreme uint8 = 4

	SecurityLevelsNormalAndHigh    uint8 = SecurityLevelNormal | SecurityLevelHigh
	SecurityLevelsNormalAndExtreme uint8 = SecurityLevelNormal | SecurityLevelExtreme
	SecurityLevelsHighAndExtreme   uint8 = SecurityLevelHigh | SecurityLevelExtreme
	SecurityLevelsAll              uint8 = SecurityLevelNormal | SecurityLevelHigh | SecurityLevelExtreme

	StatusOff     uint8 = 0
	StatusError   uint8 = 1
	StatusWarning uint8 = 2
	StatusOk      uint8 = 3
)
