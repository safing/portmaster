package status

import "testing"

func TestStatus(t *testing.T) {

	setSelectedSecurityLevel(SecurityLevelOff)
	if FmtActiveSecurityLevel() != "Dynamic" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelDynamic)
	AddOrUpdateThreat(&Threat{MitigationLevel: SecurityLevelSecure})
	if FmtActiveSecurityLevel() != "Dynamic*" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelSecure)
	if FmtActiveSecurityLevel() != "Secure" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelSecure)
	AddOrUpdateThreat(&Threat{MitigationLevel: SecurityLevelFortress})
	if FmtActiveSecurityLevel() != "Secure*" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelFortress)
	if FmtActiveSecurityLevel() != "Fortress" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

}
