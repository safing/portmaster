package status

import "testing"

func TestSet(t *testing.T) {

	// only test for panics
	// TODO: write real tests
	setSelectedSecurityLevel(0)
	SetPortmasterStatus(0, "")
	SetGate17Status(0, "")

}
