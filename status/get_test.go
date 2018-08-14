package status

import "testing"

func TestGet(t *testing.T) {

	// only test for panics
	GetCurrentSecurityLevel()
	GetSelectedSecurityLevel()
	GetThreatLevel()
	GetPortmasterStatus()
	GetGate17Status()
	option := GetConfigByLevel("invalid")
	option()

}
