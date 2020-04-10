package status

import "testing"

func TestStatus(t *testing.T) {

	setSelectedSecurityLevel(SecurityLevelOff)
	if FmtActiveSecurityLevel() != "Normal" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelNormal)
	AddOrUpdateThreat(&Threat{MitigationLevel: SecurityLevelHigh})
	if FmtActiveSecurityLevel() != "Normal*" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelHigh)
	if FmtActiveSecurityLevel() != "High" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelHigh)
	AddOrUpdateThreat(&Threat{MitigationLevel: SecurityLevelExtreme})
	if FmtActiveSecurityLevel() != "High*" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

	setSelectedSecurityLevel(SecurityLevelExtreme)
	if FmtActiveSecurityLevel() != "Extreme" {
		t.Errorf("unexpected string representation: %s", FmtActiveSecurityLevel())
	}

}
