package profile

import (
	"fmt"
	"sync"

	uuid "github.com/satori/go.uuid"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portmaster/status"
)

// Profile is used to predefine a security profile for applications.
type Profile struct {
	record.Base
	sync.Mutex

	// Profile Metadata
	ID          string
	Name        string
	Description string
	Homepage    string
	// Icon is a path to the icon and is either prefixed "f:" for filepath, "d:" for a database path or "e:" for the encoded data.
	Icon string

	// Fingerprints
	Fingerprints []string

	// The mininum security level to apply to connections made with this profile
	SecurityLevel uint8
	Flags         Flags
	Domains       Domains
	Ports         Ports

	StampProfileKey      string
	StampProfileAssigned int64

	// If a Profile is declared as a Framework (i.e. an Interpreter and the likes), then the real process must be found
	// Framework *Framework `json:",omitempty bson:",omitempty"`

	// When this Profile was approximately last used (for performance reasons not every single usage is saved)
	ApproxLastUsed int64
}

// New returns a new Profile.
func New() *Profile {
	return &Profile{}
}

// Save saves the profile to the database
func (profile *Profile) Save(namespace string) error {
	if profile.ID == "" {
		u, err := uuid.NewV4()
		if err != nil {
			return err
		}
		profile.ID = u.String()
	}

	if profile.Key() == "" {
		if namespace == "" {
			return fmt.Errorf("no key or namespace defined for profile %s", profile.String())
		}
		profile.SetKey(fmt.Sprintf("config:profiles/%s/%s", namespace, profile.ID))
	}

	return profileDB.Put(profile)
}

// String returns a string representation of the Profile.
func (profile *Profile) String() string {
	return profile.Name
}

// DetailedString returns a more detailed string representation of  theProfile.
func (profile *Profile) DetailedString() string {
	return fmt.Sprintf("%s(SL=%s Flags=[%s] Ports=[%s] #Domains=%d)", profile.Name, status.FmtSecurityLevel(profile.SecurityLevel), profile.Flags.String(), profile.Ports.String(), len(profile.Domains))
}

// GetUserProfile loads a profile from the database.
func GetUserProfile(ID string) (*Profile, error) {
	return nil, nil
}

// GetStampProfile loads a profile from the database.
func GetStampProfile(ID string) (*Profile, error) {
	return nil, nil
}
