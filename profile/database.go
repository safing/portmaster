package profile

import (
	"context"
	"errors"
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
)

// Database paths:
// core:profiles/<scope>/<id>
// cache:profiles/index/<identifier>/<value>

const profilesDBPath = "core:profiles/"

var profileDB = database.NewInterface(&database.Options{
	Local:    true,
	Internal: true,
})

func makeScopedID(source profileSource, id string) string {
	return string(source) + "/" + id
}

func makeProfileKey(source profileSource, id string) string {
	return profilesDBPath + string(source) + "/" + id
}

func registerValidationDBHook() (err error) {
	_, err = database.RegisterHook(query.New(profilesDBPath), &databaseHook{})
	return
}

func startProfileUpdateChecker() error {
	module.StartServiceWorker("update active profiles", 0, func(ctx context.Context) (err error) {
		profilesSub, err := profileDB.Subscribe(query.New(profilesDBPath))
		if err != nil {
			return err
		}
		defer func() {
			err := profilesSub.Cancel()
			if err != nil {
				log.Warningf("profile: failed to cancel subscription for updating active profiles: %s", err)
			}
		}()

	profileFeed:
		for {
			select {
			case r := <-profilesSub.Feed:
				// Check if subscription was canceled.
				if r == nil {
					return errors.New("subscription canceled")
				}

				// Get active profile.
				activeProfile := getActiveProfile(strings.TrimPrefix(r.Key(), profilesDBPath))
				if activeProfile == nil {
					// Don't do any additional actions if the profile is not active.
					continue profileFeed
				}

				// Always increase the revision counter of the layer profile.
				// This marks previous connections in the UI as decided with outdated settings.
				if activeProfile.layeredProfile != nil {
					activeProfile.layeredProfile.increaseRevisionCounter(true)
				}

				// Always mark as outdated if the record is being deleted.
				if r.Meta().IsDeleted() {
					activeProfile.outdated.Set()
					module.TriggerEvent(profileConfigChange, nil)
					continue
				}

				// If the profile is saved externally (eg. via the API), have the
				// next one to use it reload the profile from the database.
				receivedProfile, err := EnsureProfile(r)
				if err != nil || !receivedProfile.savedInternally {
					activeProfile.outdated.Set()
					module.TriggerEvent(profileConfigChange, nil)
				}
			case <-ctx.Done():
				return nil
			}
		}
	})

	return nil
}

type databaseHook struct {
	database.HookBase
}

// UsesPrePut implements the Hook interface and returns false.
func (h *databaseHook) UsesPrePut() bool {
	return true
}

// PrePut implements the Hook interface.
func (h *databaseHook) PrePut(r record.Record) (record.Record, error) {
	// convert
	profile, err := EnsureProfile(r)
	if err != nil {
		return nil, err
	}

	// clean config
	config.CleanHierarchicalConfig(profile.Config)

	// prepare profile
	profile.prepProfile()

	// parse config
	err = profile.parseConfig()
	if err != nil {
		// error here, warning when loading
		return nil, err
	}

	return profile, nil
}
