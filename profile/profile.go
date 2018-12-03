package profile

import (
	"fmt"
	"sync"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portmaster/status"
)

var (
	lastUsedUpdateThreshold = 1 * time.Hour
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
	Fingerprints []*Fingerprint

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

func makeProfileKey(namespace, ID string) string {
	return fmt.Sprintf("core:profiles/%s/%s", namespace, ID)
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
		profile.SetKey(makeProfileKey(namespace, profile.ID))
	}

	return profileDB.Put(profile)
}

// MarkUsed marks the profile as used, eventually.
func (profile *Profile) MarkUsed() (updated bool) {
	if time.Now().Add(-lastUsedUpdateThreshold).Unix() > profile.ApproxLastUsed {
		profile.ApproxLastUsed = time.Now().Unix()
		return true
	}
	return false
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
	return getProfile(userNamespace, ID)
}

// GetStampProfile loads a profile from the database.
func GetStampProfile(ID string) (*Profile, error) {
	return getProfile(stampNamespace, ID)
}

func getProfile(namespace, ID string) (*Profile, error) {
	r, err := profileDB.Get(makeProfileKey(namespace, ID))
	if err != nil {
		return nil, err
	}
	return ensureProfile(r)
}

func ensureProfile(r record.Record) (*Profile, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &Profile{}
		err := record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		return new, nil
	}

	// or adjust type
	new, ok := r.(*Profile)
	if !ok {
		return nil, fmt.Errorf("record not of type *Example, but %T", r)
	}
	return new, nil
}
