package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sync"
	"time"

	"github.com/tevino/abool"
)

const (
	registryFileName = "databases.json"
)

var (
	registryPersistence = abool.NewBool(false)
	writeRegistrySoon   = abool.NewBool(false)

	registry     = make(map[string]*Database)
	registryLock sync.Mutex

	nameConstraint = regexp.MustCompile("^[A-Za-z0-9_-]{3,}$")
)

// Register registers a new database.
// If the database is already registered, only
// the description and the primary API will be
// updated and the effective object will be returned.
func Register(db *Database) (*Database, error) {
	if !initialized.IsSet() {
		return nil, errors.New("database not initialized")
	}

	registryLock.Lock()
	defer registryLock.Unlock()

	registeredDB, ok := registry[db.Name]
	save := false

	if ok {
		// update database
		if registeredDB.Description != db.Description {
			registeredDB.Description = db.Description
			save = true
		}
		if registeredDB.ShadowDelete != db.ShadowDelete {
			registeredDB.ShadowDelete = db.ShadowDelete
			save = true
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
		save = true
	}

	if save && registryPersistence.IsSet() {
		if ok {
			registeredDB.Updated()
		}
		err := saveRegistry(false)
		if err != nil {
			return nil, err
		}
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
	if time.Now().Add(-24 * time.Hour).After(registeredDB.LastLoaded) {
		writeRegistrySoon.Set()
	}
	registeredDB.Loaded()

	return registeredDB, nil
}

// EnableRegistryPersistence enables persistence of the database registry.
func EnableRegistryPersistence() {
	if registryPersistence.SetToIf(false, true) {
		// start registry writer
		go registryWriter()
		// TODO: make an initial write if database system is already initialized
	}
}

func loadRegistry() error {
	registryLock.Lock()
	defer registryLock.Unlock()

	// read file
	filePath := path.Join(rootStructure.Path, registryFileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	// parse
	databases := make(map[string]*Database)
	err = json.Unmarshal(data, &databases)
	if err != nil {
		return err
	}

	// set
	registry = databases
	return nil
}

func saveRegistry(lock bool) error {
	if lock {
		registryLock.Lock()
		defer registryLock.Unlock()
	}

	// marshal
	data, err := json.MarshalIndent(registry, "", "\t")
	if err != nil {
		return err
	}

	// write file
	// TODO: write atomically (best effort)
	filePath := path.Join(rootStructure.Path, registryFileName)
	return os.WriteFile(filePath, data, 0o0600)
}

func registryWriter() {
	for {
		select {
		case <-time.After(1 * time.Hour):
			if writeRegistrySoon.SetToIf(true, false) {
				_ = saveRegistry(true)
			}
		case <-shutdownSignal:
			_ = saveRegistry(true)
			return
		}
	}
}
