package status

import "sync/atomic"

// SetCurrentSecurityLevel sets the current security level.
func SetCurrentSecurityLevel(level uint8) {
	sysStatusLock.Lock()
  defer sysStatusLock.Unlock()
  sysStatus.CurrentSecurityLevel = level
  atomicUpdateCurrentSecurityLevel(level)
}

// SetSelectedSecurityLevel sets the selected security level.
func SetSelectedSecurityLevel(level uint8) {
	sysStatusLock.Lock()
  defer sysStatusLock.Unlock()
  sysStatus.SelectedSecurityLevel = level
  atomicUpdateSelectedSecurityLevel(level)
}

// SetThreatLevel sets the current threat level.
func SetThreatLevel(level uint8) {
	sysStatusLock.Lock()
  defer sysStatusLock.Unlock()
  sysStatus.ThreatLevel = level
  atomicUpdateThreatLevel(level)
}

// SetPortmasterStatus sets the current Portmaster status.
func SetPortmasterStatus(status uint8) {
	sysStatusLock.Lock()
  defer sysStatusLock.Unlock()
  sysStatus.PortmasterStatus = status
  atomicUpdatePortmasterStatus(status)
}

// SetGate17Status sets the current Gate17 status.
func SetGate17Status(status uint8) {
	sysStatusLock.Lock()
  defer sysStatusLock.Unlock()
  sysStatus.Gate17Status = status
  atomicUpdateGate17Status(status)
}

// update functions for atomic stuff

func atomicUpdateCurrentSecurityLevel(level uint8) {
	atomic.StoreUint32(currentSecurityLevel, uint32(level))
}

func atomicUpdateSelectedSecurityLevel(level uint8) {
	atomic.StoreUint32(selectedSecurityLevel, uint32(level))
}

func atomicUpdateThreatLevel(level uint8) {
	atomic.StoreUint32(threatLevel, uint32(level))
}

func atomicUpdatePortmasterStatus(status uint8) {
	atomic.StoreUint32(portmasterStatus, uint32(status))
}

func atomicUpdateGate17Status(status uint8) {
	atomic.StoreUint32(gate17Status, uint32(status))
}
