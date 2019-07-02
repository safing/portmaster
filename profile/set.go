package profile

import (
	"context"
	"net"
	"sync"

	"github.com/safing/portmaster/status"
)

var (
	emptyFlags = Flags{}
)

// Set handles Profile chaining.
type Set struct {
	sync.Mutex

	id       string
	profiles [4]*Profile
	// Application
	// Global
	// Stamp
	// Default

	combinedSecurityLevel uint8
	independent           bool
}

// NewSet returns a new profile set with given the profiles.
func NewSet(ctx context.Context, id string, user, stamp *Profile) *Set {
	new := &Set{
		id: id,
		profiles: [4]*Profile{
			user,  // Application
			nil,   // Global
			stamp, // Stamp
			nil,   // Default
		},
	}
	activateProfileSet(ctx, new)
	new.Update(status.SecurityLevelFortress)
	return new
}

// UserProfile returns the user profile.
func (set *Set) UserProfile() *Profile {
	return set.profiles[0]
}

// Update gets the new global and default profile and updates the independence status. It must be called when reusing a profile set for a series of calls.
func (set *Set) Update(securityLevel uint8) {
	set.Lock()

	specialProfileLock.RLock()
	defer specialProfileLock.RUnlock()

	// update profiles
	set.profiles[1] = globalProfile
	set.profiles[3] = fallbackProfile

	// update security level
	profileSecurityLevel := set.getSecurityLevel()
	if profileSecurityLevel > securityLevel {
		set.combinedSecurityLevel = profileSecurityLevel
	} else {
		set.combinedSecurityLevel = securityLevel
	}

	set.Unlock()
	// update independence
	if set.CheckFlag(Independent) {
		set.Lock()
		set.independent = true
		set.Unlock()
	} else {
		set.Lock()
		set.independent = false
		set.Unlock()
	}
}

// SecurityLevel returns the applicable security level for the profile set.
func (set *Set) SecurityLevel() uint8 {
	set.Lock()
	defer set.Unlock()

	return set.combinedSecurityLevel
}

// GetProfileMode returns the active profile mode.
func (set *Set) GetProfileMode() uint8 {
	switch {
	case set.CheckFlag(Whitelist):
		return Whitelist
	case set.CheckFlag(Prompt):
		return Prompt
	case set.CheckFlag(Blacklist):
		return Blacklist
	default:
		return Whitelist
	}
}

// CheckFlag returns whether a given flag is set.
func (set *Set) CheckFlag(flag uint8) (active bool) {
	set.Lock()
	defer set.Unlock()

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			active, ok := profile.Flags.Check(flag, set.combinedSecurityLevel)
			if ok {
				return active
			}
		}
	}

	return false
}

// CheckEndpointDomain checks if the given endpoint matches an entry in the corresponding list. This is for outbound communication only.
func (set *Set) CheckEndpointDomain(domain string) (result EPResult, reason string) {
	set.Lock()
	defer set.Unlock()

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			if result, reason = profile.Endpoints.CheckDomain(domain); result != NoMatch {
				return
			}
		}
	}

	return NoMatch, ""
}

// CheckEndpointIP checks if the given endpoint matches an entry in the corresponding list.
func (set *Set) CheckEndpointIP(domain string, ip net.IP, protocol uint8, port uint16, inbound bool) (result EPResult, reason string) {
	set.Lock()
	defer set.Unlock()

	for i, profile := range set.profiles {
		if i == 2 && set.independent {
			continue
		}

		if profile != nil {
			if inbound {
				if result, reason = profile.ServiceEndpoints.CheckIP(domain, ip, protocol, port, inbound, set.combinedSecurityLevel); result != NoMatch {
					return
				}
			} else {
				if result, reason = profile.Endpoints.CheckIP(domain, ip, protocol, port, inbound, set.combinedSecurityLevel); result != NoMatch {
					return
				}
			}
		}
	}

	return NoMatch, ""
}

// getSecurityLevel returns the highest prioritized security level.
func (set *Set) getSecurityLevel() uint8 {
	if set == nil {
		return 0
	}

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
