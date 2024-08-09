package database

import (
	"errors"
	"fmt"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/utils"
)

const (
	databasesSubDir = "databases"
)

var (
	initialized = abool.NewBool(false)

	shuttingDown   = abool.NewBool(false)
	shutdownSignal = make(chan struct{})

	rootStructure      *utils.DirStructure
	databasesStructure *utils.DirStructure
)

// InitializeWithPath initializes the database at the specified location using a path.
func InitializeWithPath(dirPath string) error {
	return Initialize(utils.NewDirStructure(dirPath, 0o0755))
}

// Initialize initializes the database at the specified location using a dir structure.
func Initialize(dirStructureRoot *utils.DirStructure) error {
	if initialized.SetToIf(false, true) {
		rootStructure = dirStructureRoot

		// ensure root and databases dirs
		databasesStructure = rootStructure.ChildDir(databasesSubDir, 0o0700)
		err := databasesStructure.Ensure()
		if err != nil {
			return fmt.Errorf("could not create/open database directory (%s): %w", rootStructure.Path, err)
		}

		return nil
	}
	return errors.New("database already initialized")
}

// Shutdown shuts down the whole database system.
func Shutdown() (err error) {
	if shuttingDown.SetToIf(false, true) {
		close(shutdownSignal)
	} else {
		return
	}

	controllersLock.RLock()
	defer controllersLock.RUnlock()

	for _, c := range controllers {
		err = c.Shutdown()
		if err != nil {
			return
		}
	}
	return
}

// getLocation returns the storage location for the given name and type.
func getLocation(name, storageType string) (string, error) {
	location := databasesStructure.ChildDir(name, 0o0700).ChildDir(storageType, 0o0700)
	// check location
	err := location.Ensure()
	if err != nil {
		return "", fmt.Errorf(`failed to create/check database dir "%s": %w`, location.Path, err)
	}
	return location.Path, nil
}
