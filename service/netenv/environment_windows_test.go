package netenv

import "testing"

func TestWindowsEnvironment(t *testing.T) {
	defaultIf := getDefaultInterface()
	if defaultIf == nil {
		t.Error("failed to get default interface")
	}
	t.Logf("default interface: %+v", defaultIf)
}
