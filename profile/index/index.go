package index

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/utils"
)

// ProfileIndex links an Identifier to Profiles
type ProfileIndex struct {
	record.Base
	sync.Mutex

	ID string

	UserProfiles  []string
	StampProfiles []string
}

func makeIndexRecordKey(fpType, id string) string {
	return fmt.Sprintf("index:profiles/%s:%s", fpType, base64.RawURLEncoding.EncodeToString([]byte(id)))
}

// NewIndex returns a new ProfileIndex.
func NewIndex(id string) *ProfileIndex {
	return &ProfileIndex{
		ID: id,
	}
}

// AddUserProfile adds a User Profile to the index.
func (pi *ProfileIndex) AddUserProfile(identifier string) (changed bool) {
	if !utils.StringInSlice(pi.UserProfiles, identifier) {
		pi.UserProfiles = append(pi.UserProfiles, identifier)
		return true
	}
	return false
}

// AddStampProfile adds a Stamp Profile to the index.
func (pi *ProfileIndex) AddStampProfile(identifier string) (changed bool) {
	if !utils.StringInSlice(pi.StampProfiles, identifier) {
		pi.StampProfiles = append(pi.StampProfiles, identifier)
		return true
	}
	return false
}

// RemoveUserProfile removes a profile from the index.
func (pi *ProfileIndex) RemoveUserProfile(id string) {
	pi.UserProfiles = utils.RemoveFromStringSlice(pi.UserProfiles, id)
}

// RemoveStampProfile removes a profile from the index.
func (pi *ProfileIndex) RemoveStampProfile(id string) {
	pi.StampProfiles = utils.RemoveFromStringSlice(pi.StampProfiles, id)
}

// Get gets a ProfileIndex from the database.
func Get(fpType, id string) (*ProfileIndex, error) {
	key := makeIndexRecordKey(fpType, id)

	r, err := indexDB.Get(key)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &ProfileIndex{}
		err = record.Unwrap(r, new)
		if err != nil {
			return nil, err
		}
		return new, nil
	}

	// or adjust type
	new, ok := r.(*ProfileIndex)
	if !ok {
		return nil, fmt.Errorf("record not of type *ProfileIndex, but %T", r)
	}
	return new, nil
}

// Save saves the Identifiers to the database
func (pi *ProfileIndex) Save() error {
	if !pi.KeyIsSet() {
		if pi.ID != "" {
			pi.SetKey(makeIndexRecordKey(pi.ID))
		} else {
			return errors.New("missing identification Key")
		}
	}

	return indexDB.Put(pi)
}
