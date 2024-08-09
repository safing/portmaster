package database

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

var (
	registry     = make(map[string]*Database)
	registryLock sync.Mutex

	nameConstraint = regexp.MustCompile("^[A-Za-z0-9_-]{3,}$")
)

// Register registers a new database.
// If the database is already registered, only
// the description and the primary API will be
// updated and the effective object will be returned.
func Register(db *Database) (*Database, error) {
	registryLock.Lock()
	defer registryLock.Unlock()

	registeredDB, ok := registry[db.Name]

	if ok {
		// update database
		if registeredDB.Description != db.Description {
			registeredDB.Description = db.Description
		}
		if registeredDB.ShadowDelete != db.ShadowDelete {
			registeredDB.ShadowDelete = db.ShadowDelete
		}
	} else {
		// register new database
		if !nameConstraint.MatchString(db.Name) {
			return nil, errors.New("database name must only contain alphanumeric and `_-` characters and must be at least 3 characters long")
		}

		now := time.Now().Round(time.Second)
		db.Registered = now
		db.LastUpdated = now
		db.LastLoaded = time.Time{}

		registry[db.Name] = db
	}

	if ok {
		return registeredDB, nil
	}
	return nil, nil
}

func getDatabase(name string) (*Database, error) {
	registryLock.Lock()
	defer registryLock.Unlock()

	registeredDB, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf(`database "%s" not registered`, name)
	}
	registeredDB.Loaded()

	return registeredDB, nil
}
