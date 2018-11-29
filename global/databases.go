package global

import (
	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/modules"

	// module dependencies
	_ "github.com/Safing/portbase/database/dbmodule"
	_ "github.com/Safing/portbase/database/storage/badger"
)

func init() {
	modules.Register("global", nil, start, nil, "database")
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
