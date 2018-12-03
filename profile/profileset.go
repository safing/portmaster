package profile

import (
	"sync"

	"github.com/Safing/portmaster/status"
)

var (
	emptyFlags = Flags{}
	emptyPorts = Ports{}
)

// Set handles Profile chaining.
type Set struct {
	sync.Mutex

	profiles [4]*Profile
	// Application
	// Global
	// Stamp
	// Default

	securityLevel uint8
	independent   bool
}

// NewSet returns a new profile set with given the profiles.
func NewSet(user, stamp *Profile) *Set {
	new := &Set{
		profiles: [4]*Profile{
			user,  // Application
			nil,   // Global
			stamp, // Stamp
			nil,   // Default
		},
	}
	activateProfileSet(new)
	new.Update(status.SecurityLevelFortress)
	return new
}

// Update gets the new global and default profile and updates the independence status. It must be called when reusing a profile set for a series of calls.
func (set *Set) Update(securityLevel uint8) {
	specialProfileLock.RLock()
	defer specialProfileLock.RUnlock()

	// update profiles
	set.profiles[1] = globalProfile
	set.profiles[3] = fallbackProfile

	// update security level
	profileSecurityLevel := set.getProfileSecurityLevel()
	if profileSecurityLevel > securityLevel {
		set.securityLevel = profileSecurityLevel
	} else {
		set.securityLevel = securityLevel
	}

	// update independence
	if set.CheckFlag(Independent) {
		set.independent = true
	} else {
		set.independent = false
	}
}

// CheckFlag returns whether a given flag is set.
func (set *Set) CheckFlag(flag uint8) (active bool) {

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			active, ok := profile.Flags.Check(flag, set.securityLevel)
			if ok {
				return active
			}
		}
	}

	return false
}

// CheckDomain checks if the given domain is governed in any the lists of domains and returns whether it is permitted.
func (set *Set) CheckDomain(domain string) (permit, ok bool) {

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			permit, ok = profile.Domains.Check(domain)
			if ok {
				return
			}
		}
	}

	return false, false
}

// CheckPort checks if the given protocol and port are governed in any the lists of ports and returns whether it is permitted.
func (set *Set) CheckPort(listen bool, protocol uint8, port uint16) (permit, ok bool) {

	signedProtocol := int16(protocol)
	if listen {
		signedProtocol = -1 * signedProtocol
	}

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			if permit, ok = profile.Ports.Check(signedProtocol, port); ok {
				return
			}
		}
	}

	return false, false
}

// SecurityLevel returns the highest prioritized security level.
func (set *Set) getProfileSecurityLevel() uint8 {

	for i, profile := range set.profiles {
		if i == 2 {
			// Stamp profiles do not have the SecurityLevel setting
			continue
		}

		if profile != nil {
			if profile.SecurityLevel > 0 {
				return profile.SecurityLevel
			}
		}
	}

	return 0
}
