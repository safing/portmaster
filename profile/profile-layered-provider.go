package profile

import (
	"errors"
	"strings"

	"github.com/safing/portbase/database"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/runtime"
)

const (
	revisionProviderPrefix = "runtime:layeredProfile/"
)

var (
	errProfileNotActive = errors.New("profile not active")
)

func registerRevisionProvider() error {
	_, err := runtime.DefaultRegistry.Register(
		revisionProviderPrefix,
		runtime.SimpleValueGetterFunc(getRevision),
	)
	return err
}

func getRevision(key string) ([]record.Record, error) {
	key = strings.TrimPrefix(key, revisionProviderPrefix)

	// Get active profile.
	profile := getActiveProfile(key)
	if profile == nil {
		return nil, errProfileNotActive
	}

	// Get layered profile.
	layeredProfile := profile.LayeredProfile()
	if layeredProfile == nil {
		return nil, database.ErrNotFound
	}

	// Update profiles if necessary.
	if layeredProfile.NeedsUpdate() {
		layeredProfile.Update()
	}

	return []record.Record{layeredProfile}, nil
}
