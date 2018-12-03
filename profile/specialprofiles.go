package profile

import (
	"sync"

	"github.com/Safing/portbase/database"
)

var (
	globalProfile   *Profile
	fallbackProfile *Profile

	specialProfileLock sync.RWMutex
)

func initSpecialProfiles() (err error) {

	specialProfileLock.Lock()
	defer specialProfileLock.Unlock()

	globalProfile, err = getSpecialProfile("global")
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		globalProfile = makeDefaultGlobalProfile()
		globalProfile.Save(specialNamespace)
	}

	fallbackProfile, err = getSpecialProfile("fallback")
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		fallbackProfile = makeDefaultFallbackProfile()
		fallbackProfile.Save(specialNamespace)
	}

	return nil
}

func getSpecialProfile(ID string) (*Profile, error) {
	return getProfile(specialNamespace, ID)
}
