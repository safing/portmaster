package profile

import (
	"context"
	"errors"
	"strings"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
)

// Database paths:
// core:profiles/<scope>/<id>
// cache:profiles/index/<identifier>/<value>

const (
	profilesDBPath = "core:profiles/"
)

var (
	profileDB = database.NewInterface(nil)
)

func makeScopedID(source, id string) string {
	return source + "/" + id
}

func makeProfileKey(source, id string) string {
	return profilesDBPath + source + "/" + id
}

func registerValidationDBHook() (err error) {
	_, err = database.RegisterHook(query.New(profilesDBPath), &databaseHook{})
	return
}

func startProfileUpdateChecker() error {
	profilesSub, err := profileDB.Subscribe(query.New(profilesDBPath))
	if err != nil {
		return err
	}

	module.StartServiceWorker("update active profiles", 0, func(ctx context.Context) (err error) {
	feedSelect:
		for {
			select {
			case r := <-profilesSub.Feed:
				// check if nil
				if r == nil {
					return errors.New("subscription canceled")
				}

				// check if internal save
				if !r.IsWrapped() {
					profile, ok := r.(*Profile)
					if ok && profile.internalSave {
						continue feedSelect
					}
				}

				// mark as outdated
				markActiveProfileAsOutdated(strings.TrimPrefix(r.Key(), profilesDBPath))
			case <-ctx.Done():
				return profilesSub.Cancel()
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

	// prepare config
	err = profile.prepConfig()
	if err != nil {
		// error here, warning when loading
		return nil, err
	}

	// parse config
	err = profile.parseConfig()
	if err != nil {
		// error here, warning when loading
		return nil, err
	}

	return profile, nil
}
