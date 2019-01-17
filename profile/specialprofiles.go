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
		globalProfile.Save(SpecialNamespace)
	}

	fallbackProfile, err = getSpecialProfile("fallback")
	if err != nil {
		if err != database.ErrNotFound {
			return err
		}
		fallbackProfile = makeDefaultFallbackProfile()
		ensureServiceEndpointsDenyAll(fallbackProfile)
		fallbackProfile.Save(SpecialNamespace)
	}
	ensureServiceEndpointsDenyAll(fallbackProfile)

	return nil
}

func getSpecialProfile(ID string) (*Profile, error) {
	return getProfile(SpecialNamespace, ID)
}

func ensureServiceEndpointsDenyAll(p *Profile) (changed bool) {
	for _, ep := range p.ServiceEndpoints {
		if ep.DomainOrIP == "" &&
			ep.Wildcard == true &&
			ep.Protocol == 0 &&
			ep.StartPort == 0 &&
			ep.EndPort == 0 &&
			ep.Permit == false {
			return false
		}
	}

	p.ServiceEndpoints = append(p.ServiceEndpoints, &EndpointPermission{
		DomainOrIP: "",
		Wildcard:   true,
		Protocol:   0,
		StartPort:  0,
		EndPort:    0,
		Permit:     false,
	})
	return true
}
