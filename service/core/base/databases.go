package base

import (
	"github.com/safing/portbase/database"
	_ "github.com/safing/portbase/database/dbmodule"
	_ "github.com/safing/portbase/database/storage/bbolt"
)

// Default Values (changeable for testing).
var (
	DefaultDatabaseStorageType = "bbolt"
)

func registerDatabases() error {
	_, err := database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: DefaultDatabaseStorageType,
	})
	if err != nil {
		return err
	}

	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: DefaultDatabaseStorageType,
	})
	if err != nil {
		return err
	}

	// _, err = database.Register(&database.Database{
	//   Name:        "history",
	//   Description: "Historic event data",
	//   StorageType: DefaultDatabaseStorageType,
	// })
	// if err != nil {
	//   return err
	// }

	return nil
}
