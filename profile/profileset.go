package profile

var (
  emptyFlags = ProfileFlags{}
  emptyPorts = Ports{}
)

// ProfileSet handles Profile chaining.
type ProfileSet struct {
	Profiles [4]*Profile
	// Application
	// Global
	// Stamp
	// Default

  Independent bool
}

// NewSet returns a new profile set with given the profiles.
func NewSet(user, stamp *Profile) *ProfileSet {
	new := &ProfileSet{
		Profiles: [4]*Profile{
			user, // Application
			nil, // Global
			stamp, // Stamp
			nil, // Default
		},
	}
  new.Update()
  return new
}

// Update gets the new global and default profile and updates the independence status. It must be called when reusing a profile set for a series of calls.
func (ps *ProfileSet) Update() {
  specialProfileLock.RLock()
  defer specialProfileLock.RUnlock()

  // update profiles
  ps.Profiles[1] = globalProfile
  ps.Profiles[3] = defaultProfile

  // update independence
  if ps.Flags().Has(Independent, ps.SecurityLevel()) {
    // Stamp profiles do not have the Independent flag
    ps.Independent = true
  } else {
    ps.Independent = false
  }
}

// Flags returns the highest prioritized ProfileFlags configuration.
func (ps *ProfileSet) Flags() ProfileFlags {

	for _, profile := range ps.Profiles {
    if profile != nil {
      if profile.Flags.IsSet() {
        return profile.Flags
      }
    }
	}

	return emptyFlags
}

// CheckDomainStatus checks if the given domain is governed in any the lists of domains and returns whether it is permitted.
func (ps *ProfileSet) CheckDomainStatus(domain string) (permit, ok bool) {

  for i, profile := range ps.Profiles {
    if i == 2 && ps.Independent {
      continue
    }

    if profile != nil {
      if profile.Domains.IsSet() {
        permit, ok = profile.Domains.CheckStatus(domain)
        if ok {
          return
        }
      }
    }
	}

  return false, false
}

// Ports returns the highest prioritized Ports configuration.
func (ps *ProfileSet) Ports() Ports {

  for i, profile := range ps.Profiles {
    if i == 2 && ps.Independent {
      continue
    }

    if profile != nil {
      if profile.Ports.IsSet() {
        return profile.Ports
      }
    }
	}

  return emptyPorts
}

// SecurityLevel returns the highest prioritized security level.
func (ps *ProfileSet) SecurityLevel() uint8 {

	for i, profile := range ps.Profiles {
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
