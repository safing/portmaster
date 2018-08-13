// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package profiles

import (
	"encoding/hex"
	"strings"

	datastore "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"

	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/intel"
	"github.com/Safing/safing-core/log"
)

// Profile is used to predefine a security profile for applications.
type Profile struct {
	database.Base
	Name        string
	Path        string
	Description string `json:",omitempty bson:",omitempty"`
	Icon        string `json:",omitempty bson:",omitempty"`
	// Icon is a path to the icon and is either prefixed "f:" for filepath, "d:" for database cache path or "c:"/"a:" for a the icon key to fetch it from a company / authoritative node and cache it in its own cache.

	// TODO: Think more about using one profile for multiple paths
	// Refer string `json:",omitempty bson:",omitempty"`

	// If a Profile is declared as a Framework (i.e. an Interpreter and the likes), then the real process must be found
	Framework *Framework `json:",omitempty bson:",omitempty"`
	// The format how to real process is to be found is yet to be defined.
	// Ideas:
	// - Regex for finding the executed script in the arguments, prepend working directory if path is not absolute
	// - Parent Process?
	// Use Cases:
	// - Interpreters (Python, Java, ...)
	// - Sandboxes (Flatpak, Snapd, Docker, ...)
	// - Subprocesses of main application process

	SecurityLevel int8 `json:",omitempty bson:",omitempty"`
	// The mininum security level to apply to connections made with this profile
	Flags ProfileFlags

	ClassificationBlacklist    *intel.EntityClassification `json:",omitempty bson:",omitempty"`
	ClassificationWhitelist    *intel.EntityClassification `json:",omitempty bson:",omitempty"`
	DomainWhitelistIsBlacklist bool                        `json:",omitempty bson:",omitempty"`
	DomainWhitelist            []string                    `json:",omitempty bson:",omitempty"`

	ConnectPorts []uint16 `json:",omitempty bson:",omitempty"`
	ListenPorts  []uint16 `json:",omitempty bson:",omitempty"`

	Default bool `json:",omitempty bson:",omitempty"`
	// This flag indicates that this profile is a default profile. If no profile is found for a process, the default profile with the longest matching prefix path is used.
	PromptUserToAdapt bool `json:",omitempty bson:",omitempty"`
	// This flag is only valid for default profiles and indicates that should this profile be used for a process, the user will be prompted to adapt it for the process and save a new profile.
	Authoritative bool `json:",omitempty bson:",omitempty"`
	// This flag indicates that this profile was loaded from an authoritative source - the Safing Community or the Company. Authoritative Profiles that have been modified can be reverted back to their original state.
	Locked bool `json:",omitempty bson:",omitempty"`
	// This flag indicates that this profile was locked by the company. This means that the profile may not be edited by the user. If an authoritative default profile is locked, it wins over non-authoritative profiles and the user will not be prompted to adapt the profile, should the PromptUserToAdapt flag be set.
	Modified bool `json:",omitempty bson:",omitempty"`
	// This flag indicates that this profile has been modified by the user. Non-modified authoritative profiles will be automatically overwritten with new versions.
	Orphaned bool `json:",omitempty bson:",omitempty"`
	// This flag indicates that the associated program (on path) does not exist (Either this entry was manually created, or the program has been uninstalled). Only valid for non-default profiles.

	ApproxLastUsed int64 `json:",omitempty bson:",omitempty"`
	// When this Profile was approximately last used (for performance reasons not every single usage is saved)
}

var profileModel *Profile // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(profileModel, func() database.Model { return new(Profile) })
}

// Create saves Profile with the provided name in the Profiles namespace.
func (m *Profile) Create() error {
	name := hex.EncodeToString([]byte(m.Path))
	if m.Default {
		name = "d-" + name
	}
	return m.CreateObject(&database.Profiles, name, m)
}

// CreateInNamespace saves Profile with the provided name in the provided namespace.
func (m *Profile) CreateInNamespace(namespace *datastore.Key) error {
	name := hex.EncodeToString([]byte(m.Path))
	if m.Default {
		name = "d-" + name
	}
	return m.CreateObject(namespace, name, m)
}

// CreateInDist saves Profile with the (hash of the) path as the name in the Dist namespace.
func (m *Profile) CreateInDist() error {
	return m.CreateInNamespace(&database.DistProfiles)
}

// CreateInCompany saves Profile with the (hash of the) path as the name in the Company namespace.
func (m *Profile) CreateInCompany() error {
	return m.CreateInNamespace(&database.CompanyProfiles)
}

// Save saves Profile.
func (m *Profile) Save() error {
	return m.SaveObject(m)
}

// String returns a string representation of Profile.
func (m *Profile) String() string {
	if m.Default {
		return "[D] " + m.Name
	}
	return m.Name
}

// GetProfile fetches Profile with the provided name from the default namespace.
func GetProfile(name string) (*Profile, error) {
	return GetProfileFromNamespace(&database.Profiles, name)
}

// GetProfileFromNamespace fetches Profile with the provided name from the provided namespace.
func GetProfileFromNamespace(namespace *datastore.Key, name string) (*Profile, error) {
	object, err := database.GetAndEnsureModel(namespace, name, profileModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*Profile)
	if !ok {
		return nil, database.NewMismatchError(object, profileModel)
	}
	return model, nil
}

// GetActiveProfileByPath fetches Profile with the (hash of the) path as the name from the default namespace.
func GetActiveProfileByPath(path string) (*Profile, error) {
	return GetProfileFromNamespace(&database.Profiles, hex.EncodeToString([]byte(path)))
	// TODO: check for locked authoritative default profiles
}

// FindProfileByPath looks for a Profile first in the Company namespace and then in the Dist namespace. Should no Profile be available it searches for a Default Profile.
func FindProfileByPath(path, homeDir string) (profile *Profile, err error) {
	name := hex.EncodeToString([]byte(path))
	var homeName string
	slashedHomeDir := strings.TrimRight(homeDir, "/") + "/"
	if homeDir != "" && strings.HasPrefix(path, slashedHomeDir) {
		homeName = hex.EncodeToString([]byte("~/" + path[len(slashedHomeDir):]))
	}

	// check for available company profiles
	profile, err = GetProfileFromNamespace(&database.CompanyProfiles, name)
	if err != database.ErrNotFound {
		if err == nil {
			return profile.Activate()
		}
		return
	}
	if homeName != "" {
		profile, err = GetProfileFromNamespace(&database.CompanyProfiles, homeName)
		if err != database.ErrNotFound {
			if err == nil {
				return profile.Activate()
			}
			return
		}
	}

	// check for available dist profiles
	profile, err = GetProfileFromNamespace(&database.DistProfiles, name)
	if err != database.ErrNotFound {
		if err == nil {
			return profile.Activate()
		}
		return
	}
	if homeName != "" {
		profile, err = GetProfileFromNamespace(&database.DistProfiles, homeName)
		if err != database.ErrNotFound {
			if err == nil {
				return profile.Activate()
			}
			return
		}
	}

	// search for best-matching default profile
	err = nil
	profile, _ = SearchForDefaultProfile(name, homeName, len(slashedHomeDir)-2, &database.Profiles)
	return
}

func (m *Profile) Activate() (*Profile, error) {
	return m, m.Create()
}

func SearchForDefaultProfile(matchKey, matchHomeKey string, addHomeDirLen int, namespace *datastore.Key) (*Profile, int) {

	// log.Tracef("profiles: searching for default profile with %s", matchKey)

	query := dsq.Query{
		Prefix: namespace.ChildString("Profile:d-").String(),
	}

	// filter := LongestMatch{
	// 	Offset:  len(query.Prefix),
	// 	Longest: 0,
	// 	Match:   hex.EncodeToString([]byte(path)),
	// }
	// query.Filters = []dsq.Filter{
	// 	filter,
	// }

	prefixOffset := len(query.Prefix)
	longest := 0
	var longestMatch interface{}

	currentLongestIsHomeBased := false
	currentLength := 0

	iterator, err := database.Query(query)
	if err != nil {
		return nil, 0
	}

	for entry, ok := iterator.NextSync(); ok; entry, ok = iterator.NextSync() {
		// log.Tracef("profiles: checking %s for default profile", entry.Key)
		// TODO: prioritize locked profiles
		prefix := entry.Key[prefixOffset:]
		// skip directly if current longest match is longer than the key
		// log.Tracef("profiles: comparing %s to %s", matchKey, prefix)

		switch {
		case strings.HasPrefix(matchKey, prefix):
			currentLength = len(prefix)
			currentLongestIsHomeBased = false
		case strings.HasPrefix(matchHomeKey, prefix):
			currentLength = len(prefix) + addHomeDirLen
			currentLongestIsHomeBased = true
		default:
			continue
		}
		// longest wins, if a root-based and home-based tie, root-based wins.
		if currentLength > longest || (currentLongestIsHomeBased && currentLength == longest) {
			longest = currentLength
			longestMatch = entry.Value
			// log.Tracef("profiles: found new longest (%d) default profile match: %s", currentLength, entry.Key)
		}

	}

	if longestMatch == nil {
		return nil, 0
	}
	matched, ok := longestMatch.(database.Model)
	if !ok {
		log.Warningf("profiles: matched default profile is not of type database.Model")
		return nil, 0
	}

	profile, ok := database.SilentEnsureModel(matched, profileModel).(*Profile)
	if !ok {
		log.Warningf("profiles: matched default profile is not of type *Profile")
		return nil, 0
	}
	return profile, longest
}
