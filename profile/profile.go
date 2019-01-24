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

	// User Profile Only
	LinkedPath           string
	StampProfileID       string
	StampProfileAssigned int64

	// Fingerprints
	Fingerprints []*Fingerprint

	// The mininum security level to apply to connections made with this profile
	SecurityLevel    uint8
	Flags            Flags
	Endpoints        Endpoints
	ServiceEndpoints Endpoints

	// If a Profile is declared as a Framework (i.e. an Interpreter and the likes), then the real process must be found
	// Framework *Framework `json:",omitempty bson:",omitempty"`

	// When this Profile was approximately last used (for performance reasons not every single usage is saved)
	Created        int64
	ApproxLastUsed int64
}

// New returns a new Profile.
func New() *Profile {
	return &Profile{
		Created: time.Now().Unix(),
	}
}

// MakeProfileKey creates the correct key for a profile with the given namespace and ID.
func MakeProfileKey(namespace, ID string) string {
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

	if !profile.KeyIsSet() {
		if namespace == "" {
			return fmt.Errorf("no key or namespace defined for profile %s", profile.String())
		}
		profile.SetKey(MakeProfileKey(namespace, profile.ID))
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
	return fmt.Sprintf("%s(SL=%s Flags=%s Endpoints=%s)", profile.Name, status.FmtSecurityLevel(profile.SecurityLevel), profile.Flags.String(), profile.Endpoints.String())
}

// GetUserProfile loads a profile from the database.
func GetUserProfile(ID string) (*Profile, error) {
	return getProfile(UserNamespace, ID)
}

// GetStampProfile loads a profile from the database.
func GetStampProfile(ID string) (*Profile, error) {
	return getProfile(StampNamespace, ID)
}

func getProfile(namespace, ID string) (*Profile, error) {
	r, err := profileDB.Get(MakeProfileKey(namespace, ID))
	if err != nil {
		return nil, err
	}
	return EnsureProfile(r)
}

// EnsureProfile ensures that the given record is a *Profile, and returns it.
func EnsureProfile(r record.Record) (*Profile, error) {
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
