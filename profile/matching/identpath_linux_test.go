package matcher

import (
	"testing"

	"github.com/safing/portmaster/process"
)

func TestGetIdentifierLinux(t *testing.T) {
	p := &process.Process{
		Path: "/usr/lib/firefox/firefox",
	}

	if GetIdentificationPath(p) != "lin:lib/firefox/firefox" {
		t.Fatal("mismatch!")
	}

	p = &process.Process{
		Path: "/opt/start",
	}

	if GetIdentificationPath(p) != "lin:/opt/start" {
		t.Fatal("mismatch!")
	}
}
