package filterlists

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/structures/dsd"
)

// the following definitions are copied from the intelhub repository
// and stripped down to only include data required by portmaster.

// Category is used to group different list sources by the type
// of entity they are blocking. Categories may be nested using
// the Parent field.
type Category struct {
	// ID is a unique ID for the category. For sub-categories
	// this ID must be used in the Parent field of any directly
	// nesteded categories.
	ID string `json:"id"`

	// Parent may hold the ID of another category. If set, this
	// category is made a sub-category of it's parent.
	Parent string `json:"parent,omitempty"`

	// Name is a human readable name for the category and can
	// be used in user interfaces.
	Name string `json:"name"`

	// Description is a human readable description that may be
	// displayed in user interfaces.
	Description string `json:"description,omitempty"`
}

// Source defines an external filterlists source.
type Source struct {
	// ID is a unique ID for the source. Entities always reference the
	// sources they have been observed in using this ID. Refer to the
	// Entry struct for more information.
	ID string `json:"id"`

	// Name is a human readable name for the source and can be used
	// in user interfaces.
	Name string `json:"name"`

	// Description may hold a human readable description for the source.
	// It may be used in user interfaces.
	Description string `json:"description"`

	// Type describes the type of entities the source provides. Refer
	// to the Type definition for more information and well-known types.
	Type string `json:"type"`

	// URL points to the filterlists file.
	URL string `json:"url"`

	// Category holds the unique ID of a category the source belongs to. Since
	// categories can be nested the source is automatically part of all categories
	// in the hierarchy. Refer to the Category struct for more information.
	Category string `json:"category"`

	// Website may holds the URL of the source maintainers website.
	Website string `json:"website,omitempty"`

	// License holds the license that is used for the source.
	License string `json:"license"`

	// Contribute may hold an opaque string that informs a user on how to
	// contribute to the source. This may be a URL or mail address.
	Contribute string `json:"contribute"`
}

// ListIndexFile describes the structure of the released list
// index file.
type ListIndexFile struct {
	record.Base
	sync.RWMutex

	Version       string     `json:"version"`
	SchemaVersion string     `json:"schemaVersion"`
	Categories    []Category `json:"categories"`
	Sources       []Source   `json:"sources"`
}

func (index *ListIndexFile) getCategorySources(id string) []string {
	ids := make(map[string]struct{})

	// find all sources that match against cat
	for _, s := range index.Sources {
		if s.Category == id {
			ids[s.ID] = struct{}{}
		}
	}

	// find all child-categories recursing into getCategorySources.
	for _, c := range index.Categories {
		if c.Parent == id {
			for _, sid := range index.getCategorySources(c.ID) {
				ids[sid] = struct{}{}
			}
		}
	}

	return mapKeys(ids)
}

func (index *ListIndexFile) getSourcesMatching(id string) []string {
	// if id is already a source ID we just return it
	for _, s := range index.Sources {
		if s.ID == id {
			return []string{s.ID}
		}
	}

	// otherwise we need to check the category tree
	return index.getCategorySources(id)
}

func (index *ListIndexFile) getDistictSourceIDs(ids ...string) []string {
	index.RLock()
	defer index.RUnlock()

	distinctIDs := make(map[string]struct{})

	for _, id := range ids {
		for _, sid := range index.getSourcesMatching(id) {
			distinctIDs[sid] = struct{}{}
		}
	}

	return mapKeys(distinctIDs)
}

func getListIndexFromCache() (*ListIndexFile, error) {
	r, err := cache.Get(filterListIndexKey)
	if err != nil {
		return nil, err
	}

	var index *ListIndexFile
	if r.IsWrapped() {
		index = new(ListIndexFile)
		if err := record.Unwrap(r, index); err != nil {
			return nil, err
		}
	} else {
		var ok bool
		index, ok = r.(*ListIndexFile)
		if !ok {
			return nil, fmt.Errorf("invalid type, expected ListIndexFile but got %T", r)
		}
	}

	return index, nil
}

var (
	// listIndexUpdate must only be used by updateListIndex.
	listIndexUpdate     *updater.File
	listIndexUpdateLock sync.Mutex
)

func updateListIndex() error {
	listIndexUpdateLock.Lock()
	defer listIndexUpdateLock.Unlock()

	// Check if an update is needed.
	switch {
	case listIndexUpdate == nil:
		// This is the first time this function is run, get updater file for index.
		var err error
		listIndexUpdate, err = updates.GetFile(listIndexFilePath)
		if err != nil {
			return err
		}

		// Check if the version in the cache is current.
		index, err := getListIndexFromCache()
		switch {
		case errors.Is(err, database.ErrNotFound):
			log.Info("filterlists: index not in cache, starting update")
		case err != nil:
			log.Warningf("filterlists: failed to load index from cache, starting update: %s", err)
		case !listIndexUpdate.EqualsVersion(strings.TrimPrefix(index.Version, "v")):
			log.Infof(
				"filterlists: index from cache is outdated, starting update (%s != %s)",
				strings.TrimPrefix(index.Version, "v"),
				listIndexUpdate.Version(),
			)
		default:
			// List is in cache and current, there is nothing to do.
			log.Debug("filterlists: index is up to date")

			// Update the unbreak filter list IDs on initial load.
			updateUnbreakFilterListIDs()

			return nil
		}
	case listIndexUpdate.UpgradeAvailable():
		log.Info("filterlists: index update available, starting update")
	default:
		// Index is loaded and no update is available, there is nothing to do.
		return nil
	}

	// Update list index from updates.
	blob, err := os.ReadFile(listIndexUpdate.Path())
	if err != nil {
		return err
	}

	index := &ListIndexFile{}
	_, err = dsd.Load(blob, index)
	if err != nil {
		return err
	}
	index.SetKey(filterListIndexKey)

	if err := cache.Put(index); err != nil {
		return err
	}
	log.Debugf("intel/filterlists: updated list index in cache to %s", index.Version)

	// Update the unbreak filter list IDs after an update.
	updateUnbreakFilterListIDs()

	return nil
}

// ResolveListIDs resolves a slice of source or category IDs into
// a slice of distinct source IDs.
func ResolveListIDs(ids []string) ([]string, error) {
	index, err := getListIndexFromCache()
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			if err := updateListIndex(); err != nil {
				return nil, err
			}

			// retry resolving IDs
			return ResolveListIDs(ids)
		}

		log.Errorf("failed to resolved ids %v: %s", ids, err)
		return nil, err
	}

	resolved := index.getDistictSourceIDs(ids...)

	log.Debugf("intel/filterlists: resolved ids %v to %v", ids, resolved)

	return resolved, nil
}

var (
	unbreakCategoryIDs = []string{"UNBREAK"}

	unbreakIDs     []string
	unbreakIDsLock sync.Mutex
)

// GetUnbreakFilterListIDs returns the resolved list of all unbreak filter lists.
func GetUnbreakFilterListIDs() []string {
	unbreakIDsLock.Lock()
	defer unbreakIDsLock.Unlock()

	return unbreakIDs
}

func updateUnbreakFilterListIDs() {
	unbreakIDsLock.Lock()
	defer unbreakIDsLock.Unlock()

	resolvedIDs, err := ResolveListIDs(unbreakCategoryIDs)
	if err != nil {
		log.Warningf("filter: failed to resolve unbreak filter list IDs: %s", err)
	} else {
		unbreakIDs = resolvedIDs
	}
}
