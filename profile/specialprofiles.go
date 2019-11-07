package profile

import (
	"sync"

	"github.com/safing/portbase/database"
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
		_ = globalProfile.Save(SpecialNamespace)
	}

	fallbackProfile, err = getSpecialProfile("fallback")
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		fallbackProfile = makeDefaultFallbackProfile()
		ensureServiceEndpointsDenyAll(fallbackProfile)
		_ = fallbackProfile.Save(SpecialNamespace)
	}
	ensureServiceEndpointsDenyAll(fallbackProfile)

	return nil
}

func getSpecialProfile(id string) (*Profile, error) {
	return getProfile(SpecialNamespace, id)
}

func ensureServiceEndpointsDenyAll(p *Profile) (changed bool) {
	for _, ep := range p.ServiceEndpoints {
		if ep != nil {
			if ep.Type == EptAny &&
				ep.Protocol == 0 &&
				ep.StartPort == 0 &&
				ep.EndPort == 0 &&
				!ep.Permit {
				return false
			}
		}
	}

	p.ServiceEndpoints = append(p.ServiceEndpoints, &EndpointPermission{
		Type:      EptAny,
		Protocol:  0,
		StartPort: 0,
		EndPort:   0,
		Permit:    false,
	})
	return true
}
