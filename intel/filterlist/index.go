package filterlist

import (
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/formats/dsd"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/updates"
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

// Source defines an external filterlist source.
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

	// URL points to the filterlist file.
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

func updateListIndex() error {
	index, err := updates.GetFile(listIndexFilePath)
	if err != nil {
		return err
	}

	blob, err := ioutil.ReadFile(index.Path())
	if err != nil {
		return err
	}

	res, err := dsd.Load(blob, &ListIndexFile{})
	if err != nil {
		return err
	}

	content, ok := res.(*ListIndexFile)
	if !ok {
		return fmt.Errorf("unexpected format in list index")
	}

	content.SetKey(filterListIndexKey)

	if err := cache.Put(content); err != nil {
		return err
	}

	log.Debugf("intel/filterlists: updated cache record for list index with version %s", content.Version)

	return nil
}

func ResolveListIDs(ids []string) ([]string, error) {
	index, err := getListIndexFromCache()

	if err != nil {
		if err == database.ErrNotFound {
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
