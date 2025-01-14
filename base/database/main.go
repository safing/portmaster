package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/safing/portmaster/base/utils"
	"github.com/tevino/abool"
)

var (
	initialized = abool.NewBool(false)

	shuttingDown   = abool.NewBool(false)
	shutdownSignal = make(chan struct{})

	rootDir string
)

// Initialize initializes the database at the specified location.
func Initialize(databasesRootDir string) error {
	if initialized.SetToIf(false, true) {
		rootDir = databasesRootDir

		err := os.MkdirAll(rootDir, 0o0700)
		if err != nil {
			return fmt.Errorf("failed to create/check database dir %q: %w", rootDir, err)
		}
		// ensure root and databases dirs
		err = utils.EnsureDirectory(rootDir, utils.AdminOnlyPermission)
		if err != nil {
			return fmt.Errorf("could not set permissions to database directory (%s): %w", rootDir, err)
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
	location := filepath.Join(rootDir, name, storageType)

	// Make sure location exists.
	err := os.MkdirAll(location, 0o0700)
	if err != nil {
		return "", fmt.Errorf("failed to create/check database dir %q: %w", location, err)
	}
	err = utils.EnsureDirectory(location, utils.AdminOnlyPermission)
	if err != nil {
		return "", fmt.Errorf("could not set permissions to directory (%s): %w", location, err)
	}
	return location, nil
}
