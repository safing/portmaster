package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/intel/filterlists"
	"github.com/safing/portmaster/service/profile/binmeta"
	"github.com/safing/portmaster/service/profile/endpoints"
)

// ProfileSource is the source of the profile.
type ProfileSource string //nolint:golint

// Profile Sources.
const (
	SourceLocal   ProfileSource = "local"   // local, editable
	SourceSpecial ProfileSource = "special" // specials (read-only)
)

// Default Action IDs.
const (
	DefaultActionNotSet uint8 = 0
	DefaultActionBlock  uint8 = 1
	DefaultActionAsk    uint8 = 2
	DefaultActionPermit uint8 = 3
)

// Profile is used to predefine a security profile for applications.
type Profile struct { //nolint:maligned // not worth the effort
	record.Base
	sync.RWMutex

	// ID is a unique identifier for the profile.
	ID string // constant
	// Source describes the source of the profile.
	Source ProfileSource // constant
	// Name is a human readable name of the profile. It
	// defaults to the basename of the application.
	Name string
	// Description may hold an optional description of the
	// profile or the purpose of the application.
	Description string
	// Warning may hold an optional warning about this application.
	// It may be static or be added later on when the Portmaster detected an
	// issue with the application.
	Warning string
	// WarningLastUpdated holds the timestamp when the Warning field was last
	// updated.
	WarningLastUpdated time.Time
	// Homepage may refer to the website of the application
	// vendor.
	Homepage string

	// Deprecated: Icon holds the icon of the application. The value
	// may either be a filepath, a database key or a blob URL.
	// See IconType for more information.
	Icon string
	// Deprecated: IconType describes the type of the Icon property.
	IconType binmeta.IconType
	// Icons holds a list of icons to represent the application.
	Icons []binmeta.Icon

	// Deprecated: LinkedPath used to point to the executableis this
	// profile was created for.
	// Until removed, it will be added to the Fingerprints as an exact path match.
	LinkedPath string // constant
	// PresentationPath holds the path of an executable that should be used for
	// get representative information from, like the name of the program or the icon.
	// Is automatically removed when the path does not exist.
	// Is automatically populated with the next match when empty.
	PresentationPath string
	// UsePresentationPath can be used to enable/disable fetching information
	// from the executable at PresentationPath. In some cases, this is not
	// desirable.
	UsePresentationPath bool
	// Fingerprints holds process matching information.
	Fingerprints []Fingerprint
	// Config holds profile specific setttings. It's a nested
	// object with keys defining the settings database path. All keys
	// until the actual settings value (which is everything that is not
	// an object) need to be concatenated for the settings database
	// path.
	Config map[string]interface{}

	// LastEdited holds the UTC timestamp in seconds when the profile was last
	// edited by the user. This is not set automatically, but has to be manually
	// set by the user interface.
	LastEdited int64
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
	configPerspective   *config.Perspective
	dataParsed          bool
	defaultAction       uint8
	endpoints           endpoints.Endpoints
	serviceEndpoints    endpoints.Endpoints
	filterListsSet      bool
	filterListIDs       []string
	spnUsagePolicy      endpoints.Endpoints
	spnTransitHubPolicy endpoints.Endpoints
	spnExitHubPolicy    endpoints.Endpoints

	// Lifecycle Management
	outdated   *abool.AtomicBool
	lastActive *int64

	// savedInternally is set to true for profiles that are saved internally.
	savedInternally bool
}

func (profile *Profile) prepProfile() {
	// prepare configuration
	profile.outdated = abool.New()
	profile.lastActive = new(int64)

	// Migration of LinkedPath to PresentationPath
	if profile.PresentationPath == "" && profile.LinkedPath != "" {
		profile.PresentationPath = profile.LinkedPath
	}
}

func (profile *Profile) parseConfig() error {
	// Check if already parsed.
	if profile.dataParsed {
		return nil
	}

	// Create new perspective and marked as parsed.
	var err error
	profile.configPerspective, err = config.NewPerspective(profile.Config)
	if err != nil {
		return fmt.Errorf("failed to create config perspective: %w", err)
	}
	profile.dataParsed = true

	var lastErr error
	action, ok := profile.configPerspective.GetAsString(CfgOptionDefaultActionKey)
	profile.defaultAction = DefaultActionNotSet
	if ok {
		switch action {
		case DefaultActionPermitValue:
			profile.defaultAction = DefaultActionPermit
		case DefaultActionAskValue:
			profile.defaultAction = DefaultActionAsk
		case DefaultActionBlockValue:
			profile.defaultAction = DefaultActionBlock
		default:
			lastErr = fmt.Errorf(`default action "%s" invalid`, action)
		}
	}

	list, ok := profile.configPerspective.GetAsStringArray(CfgOptionEndpointsKey)
	profile.endpoints = nil
	if ok {
		profile.endpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionServiceEndpointsKey)
	profile.serviceEndpoints = nil
	if ok {
		profile.serviceEndpoints, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionFilterListsKey)
	profile.filterListsSet = false
	if ok {
		profile.filterListIDs, err = filterlists.ResolveListIDs(list)
		if err != nil {
			lastErr = err
		} else {
			profile.filterListsSet = true
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionSPNUsagePolicyKey)
	profile.spnUsagePolicy = nil
	if ok {
		profile.spnUsagePolicy, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionTransitHubPolicyKey)
	profile.spnTransitHubPolicy = nil
	if ok {
		profile.spnTransitHubPolicy, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	list, ok = profile.configPerspective.GetAsStringArray(CfgOptionExitHubPolicyKey)
	profile.spnExitHubPolicy = nil
	if ok {
		profile.spnExitHubPolicy, err = endpoints.ParseEndpoints(list)
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// New returns a new Profile.
// Optionally, you may supply custom configuration in the flat (key=value) form.
func New(profile *Profile) *Profile {
	// Create profile if none is given.
	if profile == nil {
		profile = &Profile{}
	}

	// Set default and internal values.
	profile.Created = time.Now().Unix()
	profile.savedInternally = true

	// Expand any given configuration.
	if profile.Config != nil {
		profile.Config = config.Expand(profile.Config)
	} else {
		profile.Config = make(map[string]interface{})
	}

	// Generate ID if none is given.
	if profile.ID == "" {
		if len(profile.Fingerprints) > 0 {
			// Derive from fingerprints.
			profile.ID = DeriveProfileID(profile.Fingerprints)
		} else {
			// Generate random ID as fallback.
			log.Warningf("profile: creating new profile without fingerprints to derive ID from")
			profile.ID = utils.RandomUUID("").String()
		}
	}

	// Make key from ID and source.
	profile.makeKey()

	// Prepare and parse initial profile config.
	profile.prepProfile()
	if err := profile.parseConfig(); err != nil {
		log.Errorf("profile: failed to parse new profile: %s", err)
	}

	return profile
}

// ScopedID returns the scoped ID (Source + ID) of the profile.
func (profile *Profile) ScopedID() string {
	return MakeScopedID(profile.Source, profile.ID)
}

// makeKey derives and sets the record Key from the profile attributes.
func (profile *Profile) makeKey() {
	profile.SetKey(MakeProfileKey(profile.Source, profile.ID))
}

// Save saves the profile to the database.
func (profile *Profile) Save() error {
	if profile.ID == "" {
		return errors.New("profile: tried to save profile without ID")
	}
	if profile.Source == "" {
		return fmt.Errorf("profile: profile %s does not specify a source", profile.ID)
	}

	return profileDB.Put(profile)
}

// delete deletes the profile from the database.
func (profile *Profile) delete() error {
	// Check if a key is set.
	if !profile.KeyIsSet() {
		return errors.New("key is not set")
	}

	// Delete from database.
	profile.Meta().Delete()
	err := profileDB.Put(profile)
	if err != nil {
		return err
	}

	// Post handling is done by the profile update feed.
	return nil
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

// String returns a string representation of the Profile.
func (profile *Profile) String() string {
	return fmt.Sprintf("<%s %s/%s>", profile.Name, profile.Source, profile.ID)
}

// IsOutdated returns whether the this instance of the profile is marked as outdated.
func (profile *Profile) IsOutdated() bool {
	return profile.outdated.IsSet()
}

// GetEndpoints returns the endpoint list of the profile. This functions
// requires the profile to be read locked.
func (profile *Profile) GetEndpoints() endpoints.Endpoints {
	return profile.endpoints
}

// GetServiceEndpoints returns the service endpoint list of the profile. This
// functions requires the profile to be read locked.
func (profile *Profile) GetServiceEndpoints() endpoints.Endpoints {
	return profile.serviceEndpoints
}

// AddEndpoint adds an endpoint to the endpoint list, saves the profile and reloads the configuration.
func (profile *Profile) AddEndpoint(newEntry string) {
	profile.addEndpointEntry(CfgOptionEndpointsKey, newEntry)
}

// AddServiceEndpoint adds a service endpoint to the endpoint list, saves the profile and reloads the configuration.
func (profile *Profile) AddServiceEndpoint(newEntry string) {
	profile.addEndpointEntry(CfgOptionServiceEndpointsKey, newEntry)
}

func (profile *Profile) addEndpointEntry(cfgKey, newEntry string) {
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
				log.Debugf("profile: ignoring new endpoint rule for %s, as identical is already present: %s", profile, newEntry)
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
		log.Errorf("profile: failed to parse %s config after adding endpoint: %s", profile, err)
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
		newProfile := &Profile{}
		err := record.Unwrap(r, newProfile)
		if err != nil {
			return nil, err
		}
		return newProfile, nil
	}

	// or adjust type
	newProfile, ok := r.(*Profile)
	if !ok {
		return nil, fmt.Errorf("record not of type *Profile, but %T", r)
	}
	return newProfile, nil
}

// updateMetadata updates meta data fields on the profile and returns whether
// the profile was changed.
func (profile *Profile) updateMetadata(binaryPath string) (changed bool) {
	// Check if this is a local profile, else warn and return.
	if profile.Source != SourceLocal {
		log.Warningf("tried to update metadata for non-local profile %s", profile.ScopedID())
		return false
	}

	// Set PresentationPath if unset.
	if profile.PresentationPath == "" && binaryPath != "" {
		profile.PresentationPath = binaryPath
		changed = true
	}

	// Migrate LinkedPath to PresentationPath.
	// TODO: Remove in v1.5
	if profile.PresentationPath == "" && profile.LinkedPath != "" {
		profile.PresentationPath = profile.LinkedPath
		changed = true
	}

	// Set Name if unset.
	if profile.Name == "" && profile.PresentationPath != "" {
		// Generate a default profile name from path.
		profile.Name = binmeta.GenerateBinaryNameFromPath(profile.PresentationPath)
		changed = true
	}

	// Migrate to Fingerprints.
	// TODO: Remove in v1.5
	if len(profile.Fingerprints) == 0 && profile.LinkedPath != "" {
		profile.Fingerprints = []Fingerprint{
			{
				Type:      FingerprintTypePathID,
				Operation: FingerprintOperationEqualsID,
				Value:     profile.LinkedPath,
			},
		}
		changed = true
	}

	// UI Backward Compatibility:
	// Fill LinkedPath with PresentationPath
	// TODO: Remove in v1.1
	if profile.LinkedPath == "" && profile.PresentationPath != "" {
		profile.LinkedPath = profile.PresentationPath
		changed = true
	}

	return changed
}

// updateMetadataFromSystem updates the profile metadata with data from the
// operating system and saves it afterwards.
func (profile *Profile) updateMetadataFromSystem(ctx context.Context, md MatchingData) error {
	var changed bool

	// This function is only valid for local profiles.
	if profile.Source != SourceLocal || profile.PresentationPath == "" {
		return fmt.Errorf("tried to update metadata for non-local or non-path profile %s", profile.ScopedID())
	}

	// Get home from ENV.
	var home string
	if env := md.Env(); env != nil {
		home = env["HOME"]
	}

	// Get binary icon and name.
	newIcon, newName, err := binmeta.GetIconAndName(ctx, profile.PresentationPath, home)
	switch {
	case err == nil:
		// Continue
	case errors.Is(err, binmeta.ErrIconIgnored):
		newIcon = nil
		// Continue
	default:
		log.Warningf("profile: failed to get binary icon/name for %s: %s", profile.PresentationPath, err)
	}

	// Apply new data to profile.
	func() {
		// Lock profile for applying metadata.
		profile.Lock()
		defer profile.Unlock()

		// Apply new name if it changed.
		if newName != "" && profile.Name != newName {
			profile.Name = newName
			changed = true
		}

		// Apply new icon if found.
		if newIcon != nil {
			if len(profile.Icons) == 0 {
				profile.Icons = []binmeta.Icon{*newIcon}
			} else {
				profile.Icons = append(profile.Icons, *newIcon)
				profile.Icons = binmeta.SortAndCompactIcons(profile.Icons)
			}
		}
	}()

	// If anything changed, save the profile.
	// profile.Lock must not be held!
	if changed {
		err := profile.Save()
		if err != nil {
			log.Warningf("profile: failed to save %s after metadata update: %s", profile.ScopedID(), err)
		}
	}

	return nil
}
