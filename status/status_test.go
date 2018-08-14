package status

import "testing"

func TestStatus(t *testing.T) {

	SetCurrentSecurityLevel(SecurityLevelOff)
	SetSelectedSecurityLevel(SecurityLevelOff)
	if FmtSecurityLevel() != "Off" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelDynamic)
	SetSelectedSecurityLevel(SecurityLevelDynamic)
	if FmtSecurityLevel() != "Dynamic" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelSecure)
	SetSelectedSecurityLevel(SecurityLevelSecure)
	if FmtSecurityLevel() != "Secure" {
		t.Error("unexpected string representation")
	}

	SetCurrentSecurityLevel(SecurityLevelFortress)
	SetSelectedSecurityLevel(SecurityLevelFortress)
	if FmtSecurityLevel() != "Fortress" {
		t.Error("unexpected string representation")
	}

	SetSelectedSecurityLevel(SecurityLevelDynamic)
	if FmtSecurityLevel() != "Fortress*" {
		t.Error("unexpected string representation")
	}

}
