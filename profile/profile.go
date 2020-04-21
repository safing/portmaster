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
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	lastUsedUpdateThreshold = 24 * time.Hour
)

// Profile Sources
const (
	SourceLocal      string = "local"   // local, editable
	SourceSpecial    string = "special" // specials (read-only)
	SourceCommunity  string = "community"
	SourceEnterprise string = "enterprise"
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
	filterListIDs     []string

	// Lifecycle Management
	outdated *abool.AtomicBool
	lastUsed time.Time

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
	// prepare configuration
	profile.configPerspective, err = config.NewPerspective(profile.Config)
	profile.outdated = abool.New()
	return
}

func (profile *Profile) parseConfig() error {
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

	action, ok := profile.configPerspective.GetAsString(CfgOptionDefaultActionKey)
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

	list, ok := profile.configPerspective.GetAsStringArray(CfgOptionEndpointsKey)
	if ok {
		profile.endpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionServiceEndpointsKey)
	if ok {
		profile.serviceEndpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionFilterListsKey)
	if ok {
		profile.filterListIDs, err = filterlists.ResolveListIDs(list)
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// New returns a new Profile.
func New() *Profile {
	profile := &Profile{
		ID:           uuid.NewV4().String(),
		Source:       SourceLocal,
		Created:      time.Now().Unix(),
		Config:       make(map[string]interface{}),
		internalSave: true,
	}

	// create placeholders
	_ = profile.prepConfig()
	_ = profile.parseConfig()

	return profile
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

// MarkUsed marks the profile as used and saves it when it has changed.
func (profile *Profile) MarkUsed() {
	profile.Lock()
	// lastUsed
	profile.lastUsed = time.Now()

	// ApproxLastUsed
	save := false
	if time.Now().Add(-lastUsedUpdateThreshold).Unix() > profile.ApproxLastUsed {
		profile.ApproxLastUsed = time.Now().Unix()
		save = true
	}
	profile.Unlock()

	if save {
		err := profile.Save()
		if err != nil {
			log.Warningf("profiles: failed to save profile %s after marking as used: %s", profile.ScopedID(), err)
		}
	}
}

// String returns a string representation of the Profile.
func (profile *Profile) String() string {
	return profile.Name
}

// AddEndpoint adds an endpoint to the endpoint list, saves the profile and reloads the configuration.
func (profile *Profile) AddEndpoint(newEntry string) {
	profile.addEndpointyEntry(CfgOptionEndpointsKey, newEntry)
}

// AddServiceEndpoint adds a service endpoint to the endpoint list, saves the profile and reloads the configuration.
func (profile *Profile) AddServiceEndpoint(newEntry string) {
	profile.addEndpointyEntry(CfgOptionServiceEndpointsKey, newEntry)
}

func (profile *Profile) addEndpointyEntry(cfgKey, newEntry string) {
	profile.Lock()
	// get, update, save endpoints list
	endpointList, ok := profile.configPerspective.GetAsStringArray(cfgKey)
	if !ok {
		endpointList = make([]string, 0, 1)
	}
	endpointList = append(endpointList, newEntry)
	profile.Config[cfgKey] = endpointList

	profile.Unlock()
	err := profile.Save()
	if err != nil {
		log.Warningf("profile: failed to save profile after adding endpoint: %s", err)
	}

	// reload manually
	profile.Lock()
	profile.dataParsed = false
	err = profile.parseConfig()
	if err != nil {
		log.Warningf("profile: failed to parse profile config after adding endpoint: %s", err)
	}
	profile.Unlock()
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
		profile.MarkUsed()
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

	// lock for prepping
	profile.Lock()

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

	// mark as internal
	profile.internalSave = true

	profile.Unlock()

	// mark active
	profile.MarkUsed()
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
