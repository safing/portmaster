package status

import "testing"

func TestStatus(t *testing.T) {

	SetCurrentSecurityLevel(SecurityLevelOff)
	SetSelectedSecurityLevel(SecurityLevelOff)
	if FmtCurrentSecurityLevel() != "Off" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelDynamic)
	SetSelectedSecurityLevel(SecurityLevelDynamic)
	if FmtCurrentSecurityLevel() != "Dynamic" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelSecure)
	SetSelectedSecurityLevel(SecurityLevelSecure)
	if FmtCurrentSecurityLevel() != "Secure" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelFortress)
	SetSelectedSecurityLevel(SecurityLevelFortress)
	if FmtCurrentSecurityLevel() != "Fortress" {
		t.Error("unexpected string representation")
	}

	SetSelectedSecurityLevel(SecurityLevelDynamic)
	if FmtCurrentSecurityLevel() != "Fortress*" {
		t.Error("unexpected string representation")
	}

}
