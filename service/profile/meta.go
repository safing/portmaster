package profile

import (
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database/record"
)

// ProfilesMetadata holds metadata about all profiles that are not fit to be
// stored with the profiles themselves.
type ProfilesMetadata struct {
	record.Base
	sync.Mutex

	States map[string]*MetaState
}

// MetaState describes the state of a profile.
type MetaState struct {
	State string
	At    time.Time
}

// Profile metadata states.
const (
	MetaStateSeen    = "seen"
	MetaStateDeleted = "deleted"
)

// EnsureProfilesMetadata ensures that the given record is a *ProfilesMetadata, and returns it.
func EnsureProfilesMetadata(r record.Record) (*ProfilesMetadata, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newMeta := &ProfilesMetadata{}
		err := record.Unwrap(r, newMeta)
		if err != nil {
			return nil, err
		}
		return newMeta, nil
	}

	// or adjust type
	newMeta, ok := r.(*ProfilesMetadata)
	if !ok {
		return nil, fmt.Errorf("record not of type *Profile, but %T", r)
	}
	return newMeta, nil
}

var (
	profilesMetadataKey = "core:profile-states"

	meta *ProfilesMetadata

	removeDeletedEntriesAfter = 30 * 24 * time.Hour
)

// loadProfilesMetadata loads the profile metadata from the database.
// It may only be called during module starting, as there is no lock for "meta" itself.
func loadProfilesMetadata() error {
	r, err := profileDB.Get(profilesMetadataKey)
	if err != nil {
		return err
	}
	loadedMeta, err := EnsureProfilesMetadata(r)
	if err != nil {
		return err
	}

	// Set package variable.
	meta = loadedMeta
	return nil
}

func (meta *ProfilesMetadata) check() {
	if meta.States == nil {
		meta.States = make(map[string]*MetaState)
	}
}

// Save saves the profile metadata to the database.
func (meta *ProfilesMetadata) Save() error {
	if meta == nil {
		return nil
	}

	func() {
		meta.Lock()
		defer meta.Unlock()

		if !meta.KeyIsSet() {
			meta.SetKey(profilesMetadataKey)
		}
	}()

	meta.Clean()
	return profileDB.Put(meta)
}

// Clean removes old entries.
func (meta *ProfilesMetadata) Clean() {
	if meta == nil {
		return
	}

	meta.Lock()
	defer meta.Unlock()

	for key, state := range meta.States {
		switch {
		case state == nil:
			delete(meta.States, key)
		case state.State != MetaStateDeleted:
			continue
		case time.Since(state.At) > removeDeletedEntriesAfter:
			delete(meta.States, key)
		}
	}
}

// GetLastSeen returns when the profile with the given ID was last seen.
func (meta *ProfilesMetadata) GetLastSeen(scopedID string) *time.Time {
	if meta == nil {
		return nil
	}

	meta.Lock()
	defer meta.Unlock()

	state := meta.States[scopedID]
	switch {
	case state == nil:
		return nil
	case state.State == MetaStateSeen:
		return &state.At
	default:
		return nil
	}
}

// UpdateLastSeen sets the profile with the given ID as last seen now.
func (meta *ProfilesMetadata) UpdateLastSeen(scopedID string) {
	if meta == nil {
		return
	}

	meta.Lock()
	defer meta.Unlock()

	meta.States[scopedID] = &MetaState{
		State: MetaStateSeen,
		At:    time.Now().UTC(),
	}
}

// MarkDeleted marks the profile with the given ID as deleted.
func (meta *ProfilesMetadata) MarkDeleted(scopedID string) {
	if meta == nil {
		return
	}

	meta.Lock()
	defer meta.Unlock()

	meta.States[scopedID] = &MetaState{
		State: MetaStateDeleted,
		At:    time.Now().UTC(),
	}
}

// RemoveState removes any state of the profile with the given ID.
func (meta *ProfilesMetadata) RemoveState(scopedID string) {
	if meta == nil {
		return
	}

	meta.Lock()
	defer meta.Unlock()

	delete(meta.States, scopedID)
}
