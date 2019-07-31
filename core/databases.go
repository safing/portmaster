package core

import (
	"github.com/safing/portbase/database"

	// module dependencies
	_ "github.com/safing/portbase/database/storage/bbolt"
)

func registerDatabases() error {
	_, err := database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "bbolt",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: "bbolt",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	// _, err = database.Register(&database.Database{
	//   Name:        "history",
	//   Description: "Historic event data",
	//   StorageType: "bbolt",
	//   PrimaryAPI:  "",
	// })
	// if err != nil {
	//   return err
	// }

	return nil
}
