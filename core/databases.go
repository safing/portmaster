package core

import (
	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/modules"
	"github.com/Safing/portbase/notifications"

	// module dependencies
	_ "github.com/Safing/portbase/database/dbmodule"
	_ "github.com/Safing/portbase/database/storage/badger"
)

func init() {
	modules.Register("core", nil, start, nil, "database")

	notifications.SetPersistenceBasePath("core:notifications")
}

func start() error {
	_, err := database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "badger",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: "badger",
		PrimaryAPI:  "",
	})
	if err != nil {
		return err
	}

	// _, err = database.Register(&database.Database{
	//   Name:        "history",
	//   Description: "Historic event data",
	//   StorageType: "badger",
	//   PrimaryAPI:  "",
	// })
	// if err != nil {
	//   return err
	// }

	return nil
}
