package profile

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	lastUsedUpdateThreshold = 24 * time.Hour
)

// profileSource is the source of the profile.
type profileSource string

// Profile Sources
const (
	SourceLocal      profileSource = "local"   // local, editable
	SourceSpecial    profileSource = "special" // specials (read-only)
	SourceCommunity  profileSource = "community"
	SourceEnterprise profileSource = "enterprise"
)

// Default Action IDs
const (
	DefaultActionNotSet uint8 = 0
	DefaultActionBlock  uint8 = 1
	DefaultActionAsk    uint8 = 2
	DefaultActionPermit uint8 = 3
)

// iconType describes the type of the Icon property
// of a profile.
type iconType string

// Supported icon types.
const (
	IconTypeFile     iconType = "path"
	IconTypeDatabase iconType = "database"
	IconTypeBlob     iconType = "blob"
)

// Profile is used to predefine a security profile for applications.
type Profile struct { //nolint:maligned // not worth the effort
	record.Base
	sync.RWMutex

	// ID is a unique identifier for the profile.
	ID string
	// Source describes the source of the profile.
	Source profileSource
	// Name is a human readable name of the profile. It
	// defaults to the basename of the application.
	Name string
	// Description may holds an optional description of the
	// profile or the purpose of the application.
	Description string
	// Homepage may refer the the website of the application
	// vendor.
	Homepage string
	// Icon holds the icon of the application. The value
	// may either be a filepath, a database key or a blob URL.
	// See IconType for more information.
	Icon string
	// IconType describes the type of the Icon property.
	IconType iconType
	// LinkedPath is a filesystem path to the executable this
	// profile was created for.
	LinkedPath string
	// LinkedProfiles is a list of other profiles
	LinkedProfiles []string
	// SecurityLevel is the mininum security level to apply to
	// connections made with this profile.
	// Note(ppacher): we may deprecate this one as it can easily
	//			      be "simulated" by adjusting the settings
	//				  directly.
	SecurityLevel uint8
	// Config holds profile specific setttings. It's a nested
	// object with keys defining the settings database path. All keys
	// until the actual settings value (which is everything that is not
	// an object) need to be concatenated for the settings database
	// path.
	Config map[string]interface{}
	// ApproxLastUsed holds a UTC timestamp in seconds of
	// when this Profile was approximately last used.
	// For performance reasons not every single usage is saved.
	ApproxLastUsed int64
	// Created holds the UTC timestamp in seconds when the
	// profile has been created.
	Created int64

	// Internal is set to true if the profile is attributed to a
	// Portmaster internal process. Internal is set during profile
	// creation and may be accessed without lock.
	Internal bool

	// layeredProfile is a link to the layered profile with this profile as the
	// main profile.
	// All processes with the same binary should share the same instance of the
	// local profile and the associated layered profile.
	layeredProfile *LayeredProfile

	// Interpreted Data
	configPerspective *config.Perspective
	dataParsed        bool
	defaultAction     uint8
	endpoints         endpoints.Endpoints
	serviceEndpoints  endpoints.Endpoints
	filterListIDs     []string

	// Lifecycle Management
	outdated   *abool.AtomicBool
	lastActive *int64

	internalSave bool
}

func (profile *Profile) prepConfig() (err error) {
	// prepare configuration
	profile.configPerspective, err = config.NewPerspective(profile.Config)
	profile.outdated = abool.New()
	profile.lastActive = new(int64)
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
func New(source profileSource, id string) *Profile {
	profile := &Profile{
		ID:           id,
		Source:       source,
		Created:      time.Now().Unix(),
		Config:       make(map[string]interface{}),
		internalSave: true,
	}

	// Generate random ID if none is given.
	if id == "" {
		profile.ID = utils.RandomUUID("").String()
	}

	// Make key from ID and source.
	profile.makeKey()

	// Prepare profile to create placeholders.
	_ = profile.prepConfig()
	_ = profile.parseConfig()

	return profile
}

// ScopedID returns the scoped ID (Source + ID) of the profile.
func (profile *Profile) ScopedID() string {
	return makeScopedID(profile.Source, profile.ID)
}

// makeKey derives and sets the record Key from the profile attributes.
func (profile *Profile) makeKey() {
	profile.SetKey(makeProfileKey(profile.Source, profile.ID))
}

// Save saves the profile to the database
func (profile *Profile) Save() error {
	if profile.ID == "" {
		return errors.New("profile: tried to save profile without ID")
	}
	if profile.Source == "" {
		return fmt.Errorf("profile: profile %s does not specify a source", profile.ID)
	}

	return profileDB.Put(profile)
}

// MarkStillActive marks the profile as still active.
func (profile *Profile) MarkStillActive() {
	atomic.StoreInt64(profile.lastActive, time.Now().Unix())
}

// LastActive returns the unix timestamp when the profile was last marked as
// still active.
func (profile *Profile) LastActive() int64 {
	return atomic.LoadInt64(profile.lastActive)
}

// MarkUsed updates ApproxLastUsed when it's been a while and saves the profile if it was changed.
func (profile *Profile) MarkUsed() (changed bool) {
	profile.Lock()
	defer profile.Unlock()

	if time.Now().Add(-lastUsedUpdateThreshold).Unix() > profile.ApproxLastUsed {
		profile.ApproxLastUsed = time.Now().Unix()
		return true
	}

	return false
}

// String returns a string representation of the Profile.
func (profile *Profile) String() string {
	return fmt.Sprintf("<%s %s/%s>", profile.Name, profile.Source, profile.ID)
}

// IsOutdated returns whether the this instance of the profile is marked as outdated.
func (profile *Profile) IsOutdated() bool {
	return profile.outdated.IsSet()
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
	changed := false

	// When finished, save the profile.
	defer func() {
		if !changed {
			return
		}

		err := profile.Save()
		if err != nil {
			log.Warningf("profile: failed to save profile %s after add an endpoint rule: %s", profile.ScopedID(), err)
		}
	}()

	// When finished increase the revision counter of the layered profile.
	defer func() {
		if !changed || profile.layeredProfile == nil {
			return
		}

		profile.layeredProfile.Lock()
		defer profile.layeredProfile.Unlock()

		profile.layeredProfile.RevisionCounter++
	}()

	// Lock the profile for editing.
	profile.Lock()
	defer profile.Unlock()

	// Get the endpoint list configuration value and add the new entry.
	endpointList, ok := profile.configPerspective.GetAsStringArray(cfgKey)
	if ok {
		// A list already exists, check for duplicates within the same prefix.
		newEntryPrefix := strings.Split(newEntry, " ")[0] + " "
		for _, entry := range endpointList {
			if !strings.HasPrefix(entry, newEntryPrefix) {
				// We found an entry with a different prefix than the new entry.
				// Beyond this entry we cannot possibly know if identical entries will
				// match, so we will have to add the new entry no matter what the rest
				// of the list has.
				break
			}

			if entry == newEntry {
				// An identical entry is already in the list, abort.
				log.Debugf("profile: ingoring new endpoint rule for %s, as identical is already present: %s", profile, newEntry)
				return
			}
		}
		endpointList = append([]string{newEntry}, endpointList...)
	} else {
		endpointList = []string{newEntry}
	}

	// Save new value back to profile.
	config.PutValueIntoHierarchicalConfig(profile.Config, cfgKey, endpointList)
	changed = true

	// Reload the profile manually in order to parse the newly added entry.
	profile.dataParsed = false
	err := profile.parseConfig()
	if err != nil {
		log.Warningf("profile: failed to parse %s config after adding endpoint: %s", profile, err)
	}
}

// LayeredProfile returns the layered profile associated with this profile.
func (profile *Profile) LayeredProfile() *LayeredProfile {
	profile.Lock()
	defer profile.Unlock()

	return profile.layeredProfile
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
