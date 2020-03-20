package profile

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portbase/log"

	"github.com/tevino/abool"

	uuid "github.com/satori/go.uuid"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	lastUsedUpdateThreshold = 1 * time.Hour
)

// Profile Sources
const (
	SourceLocal      string = "local"
	SourceCommunity  string = "community"
	SourceEnterprise string = "enterprise"
	SourceGlobal     string = "global"
)

// Default Action IDs
const (
	DefaultActionNotSet uint8 = 0
	DefaultActionBlock  uint8 = 1
	DefaultActionAsk    uint8 = 2
	DefaultActionPermit uint8 = 3
)

// Profile is used to predefine a security profile for applications.
type Profile struct { //nolint:maligned // not worth the effort
	record.Base
	sync.Mutex

	// Identity
	ID     string
	Source string

	// App Information
	Name        string
	Description string
	Homepage    string
	// Icon is a path to the icon and is either prefixed "f:" for filepath, "d:" for a database path or "e:" for the encoded data.
	Icon string

	// References - local profiles only
	// LinkedPath is a filesystem path to the executable this profile was created for.
	LinkedPath string
	// LinkedProfiles is a list of other profiles
	LinkedProfiles []string

	// Fingerprints
	// TODO: Fingerprints []*Fingerprint

	// Configuration
	// The mininum security level to apply to connections made with this profile
	SecurityLevel uint8
	Config        map[string]interface{}

	// Interpreted Data
	configPerspective *config.Perspective
	dataParsed        bool
	defaultAction     uint8
	endpoints         endpoints.Endpoints
	serviceEndpoints  endpoints.Endpoints

	// Lifecycle Management
	oudated *abool.AtomicBool

	// Framework
	// If a Profile is declared as a Framework (i.e. an Interpreter and the likes), then the real process/actor must be found
	// TODO: Framework *Framework

	// When this Profile was approximately last used.
	// For performance reasons not every single usage is saved.
	ApproxLastUsed int64
	Created        int64

	internalSave bool
}

func (profile *Profile) prepConfig() (err error) {
	profile.Lock()
	defer profile.Unlock()

	// prepare configuration
	profile.configPerspective, err = config.NewPerspective(profile.Config)
	return
}

func (profile *Profile) parseConfig() error {
	profile.Lock()
	defer profile.Unlock()

	if profile.configPerspective == nil {
		return errors.New("config not prepared")
	}

	// check if already parsed
	if profile.dataParsed {
		return nil
	}
	profile.dataParsed = true

	var err error
	var lastErr error

	action, ok := profile.configPerspective.GetAsString(cfgOptionDefaultActionKey)
	if ok {
		switch action {
		case "permit":
			profile.defaultAction = DefaultActionPermit
		case "ask":
			profile.defaultAction = DefaultActionAsk
		case "block":
			profile.defaultAction = DefaultActionBlock
		default:
			lastErr = fmt.Errorf(`default action "%s" invalid`, action)
		}
	}

	list, ok := profile.configPerspective.GetAsStringArray(cfgOptionEndpointsKey)
	if ok {
		profile.endpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(cfgOptionServiceEndpointsKey)
	if ok {
		profile.serviceEndpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// New returns a new Profile.
func New() *Profile {
	return &Profile{
		ID:      uuid.NewV4().String(),
		Source:  SourceLocal,
		Created: time.Now().Unix(),
	}
}

// ScopedID returns the scoped ID (Source + ID) of the profile.
func (profile *Profile) ScopedID() string {
	return makeScopedID(profile.Source, profile.ID)
}

// Save saves the profile to the database
func (profile *Profile) Save() error {
	if profile.ID == "" {
		return errors.New("profile: tried to save profile without ID")
	}
	if profile.Source == "" {
		return fmt.Errorf("profile: profile %s does not specify a source", profile.ID)
	}

	if !profile.KeyIsSet() {
		profile.SetKey(makeProfileKey(profile.Source, profile.ID))
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

// GetProfile loads a profile from the database.
func GetProfile(source, id string) (*Profile, error) {
	return GetProfileByScopedID(makeScopedID(source, id))
}

// GetProfileByScopedID loads a profile from the database using a scoped ID like "local/id" or "community/id".
func GetProfileByScopedID(scopedID string) (*Profile, error) {
	// check cache
	profile := getActiveProfile(scopedID)
	if profile != nil {
		return profile, nil
	}

	// get from database
	r, err := profileDB.Get(profilesDBPath + scopedID)
	if err != nil {
		return nil, err
	}

	// convert
	profile, err = EnsureProfile(r)
	if err != nil {
		return nil, err
	}

	// prepare config
	err = profile.prepConfig()
	if err != nil {
		log.Warningf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// parse config
	err = profile.parseConfig()
	if err != nil {
		log.Warningf("profiles: profile %s has (partly) invalid configuration: %s", profile.ID, err)
	}

	// mark active
	markProfileActive(profile)

	return profile, nil
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
		return nil, fmt.Errorf("record not of type *Profile, but %T", r)
	}
	return new, nil
}
