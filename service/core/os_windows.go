package core

import (
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils/osdetail"
)

// only return on Fatal error!
func startPlatformSpecific() error {
	// We can't catch errors when calling WindowsNTVersion() in logging, so we call the function here, just to catch possible errors
	if _, err := osdetail.WindowsNTVersion(); err != nil {
		log.Errorf("failed to obtain WindowsNTVersion: %s", err)
	}

	return nil
}
