package status

import (
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("status", prep, nil, nil)
}

func prep() error {

	if CurrentSecurityLevel() == SecurityLevelOff {
		log.Infof("switching to default active security level: dynamic")
		SetCurrentSecurityLevel(SecurityLevelDynamic)
	}

	if SelectedSecurityLevel() == SecurityLevelOff {
		log.Infof("switching to default selected security level: dynamic")
		SetSelectedSecurityLevel(SecurityLevelDynamic)
	}

	return nil
}
