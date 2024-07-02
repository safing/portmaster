package main

import (
	"github.com/safing/portmaster/base/database"
	_ "github.com/safing/portmaster/base/database/storage/hashmap"
)

func setupDatabases(path string) error {
	err := database.InitializeWithPath(path)
	if err != nil {
		return err
	}

	_, err = database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "hashmap",
	})
	if err != nil {
		return err
	}

	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: "hashmap",
	})
	if err != nil {
		return err
	}

	return nil
}
