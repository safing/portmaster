package base

import (
	"path/filepath"

	"github.com/safing/portmaster/base/database"
	_ "github.com/safing/portmaster/base/database/storage/bbolt"
	_ "github.com/safing/portmaster/base/database/storage/sqlite"
	"github.com/safing/portmaster/base/utils"
)

// Default Values (changeable for testing).
var (
	DefaultDatabaseStorageType = "sqlite"
)

func registerDatabases() error {
	// If there is an existing bbolt core database, use it instead.
	coreStorageType := DefaultDatabaseStorageType
	if utils.PathExists(filepath.Join(module.instance.DataDir(), "databases", "core", "bbolt")) {
		coreStorageType = "bbolt"
	}

	// Register core database.
	_, err := database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: coreStorageType,
	})
	if err != nil {
		return err
	}

	// If there is an existing cache bbolt database, use it instead.
	cacheStorageType := DefaultDatabaseStorageType
	if utils.PathExists(filepath.Join(module.instance.DataDir(), "databases", "cache", "bbolt")) {
		cacheStorageType = "bbolt"
	}

	// Register cache database.
	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: cacheStorageType,
	})
	if err != nil {
		return err
	}

	return nil
}
