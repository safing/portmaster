package index

import (
  "sync"
  "fmt"
  "errors"
	"encoding/base64"

  "github.com/Safing/portbase/database/record"
  "github.com/Safing/portbase/utils"
)

// ProfileIndex links an Identifier to Profiles
type ProfileIndex struct {
  record.Base
  sync.Mutex

  ID string
	UserProfiles []string
  StampProfiles []string
}

func makeIndexRecordKey(id string) string {
  return fmt.Sprintf("core:profiles/index/%s", base64.RawURLEncoding.EncodeToString([]byte(id)))
}

// NewIndex returns a new ProfileIndex.
func NewIndex(id string) *ProfileIndex {
	return &ProfileIndex{
		ID: id,
	}
}

// AddUserProfile adds a User Profile to the index.
func (pi *ProfileIndex) AddUserProfile(id string) (changed bool) {
  if !utils.StringInSlice(pi.UserProfiles, id) {
    pi.UserProfiles = append(pi.UserProfiles, id)
    return true
  }
  return false
}

// AddStampProfile adds a Stamp Profile to the index.
func (pi *ProfileIndex) AddStampProfile(id string) (changed bool) {
  if !utils.StringInSlice(pi.StampProfiles, id) {
    pi.StampProfiles = append(pi.StampProfiles, id)
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

// GetIndex gets a ProfileIndex from the database.
func GetIndex(id string) (*ProfileIndex, error) {
	key := makeIndexRecordKey(id)

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
  if pi.Key() == "" {
    if pi.ID != "" {
      pi.SetKey(makeIndexRecordKey(pi.ID))
    } else {
      return errors.New("missing identification Key")
    }
  }

	return indexDB.Put(pi)
}
