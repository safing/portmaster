package status

import "testing"

func TestSet(t *testing.T) {

	// only test for panics
	SetCurrentSecurityLevel(0)
	SetSelectedSecurityLevel(0)
	SetThreatLevel(0)
	SetPortmasterStatus(0)
	SetGate17Status(0)

}
