package osdetail

import "testing"

func TestWindowsNTVersion(t *testing.T) {
	if str, err := WindowsNTVersion(); str == "" || err != nil {
		t.Fatalf("failed to obtain windows version: %s", err)
	}
}

func TestIsAtLeastWindowsNTVersion(t *testing.T) {
	ret, err := IsAtLeastWindowsNTVersion("6")
	if err != nil {
		t.Fatalf("failed to compare windows versions: %s", err)
	}
	if !ret {
		t.Fatalf("WindowsNTVersion is less than 6 (Vista)")
	}
}

func TestIsAtLeastWindowsVersion(t *testing.T) {
	ret, err := IsAtLeastWindowsVersion("7")
	if err != nil {
		t.Fatalf("failed to compare windows versions: %s", err)
	}
	if !ret {
		t.Fatalf("WindowsVersion is less than 7")
	}
}
