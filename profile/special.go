package profile

import (
	"github.com/safing/portbase/log"
)

const (
	unidentifiedProfileID = "_unidentified"
	systemProfileID       = "_system"
)

func GetUnidentifiedProfile() *Profile {
	// get profile
	profile, err := GetProfile(SourceLocal, unidentifiedProfileID)
	if err == nil {
		return profile
	}

	// create if not available (or error)
	profile = New()
	profile.Name = "Unidentified Processes"
	profile.Source = SourceLocal
	profile.ID = unidentifiedProfileID

	// save to db
	err = profile.Save()
	if err != nil {
		log.Warningf("profiles: failed to save %s: %s", profile.ScopedID(), err)
	}

	return profile
}

func GetSystemProfile() *Profile {
	// get profile
	profile, err := GetProfile(SourceLocal, systemProfileID)
	if err == nil {
		return profile
	}

	// create if not available (or error)
	profile = New()
	profile.Name = "Operating System"
	profile.Source = SourceLocal
	profile.ID = systemProfileID

	// save to db
	err = profile.Save()
	if err != nil {
		log.Warningf("profiles: failed to save %s: %s", profile.ScopedID(), err)
	}

	return profile
}
