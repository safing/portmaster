package status

import "testing"

func TestGet(t *testing.T) {

	// only test for panics
	// TODO: write real tests
	ActiveSecurityLevel()
	SelectedSecurityLevel()
	PortmasterStatus()
	Gate17Status()
	option := ConfigIsActive("invalid")
	option(0)
	option = ConfigIsActiveConcurrent("invalid")
	option(0)

}
