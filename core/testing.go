package core

import (
	"io/ioutil"
	"os"

	"github.com/safing/portbase/log"

	"github.com/safing/portbase/database"

	// module dependencies
	_ "github.com/safing/portbase/database/storage/hashmap"
)

// InitForTesting initializes the core module directly. This is intended to be only used by unit tests that require the core (and depending) modules.
func InitForTesting() (tmpDir string, err error) {
	tmpDir, err = ioutil.TempDir(os.TempDir(), "pm-testing-")
	if err != nil {
		return "", err
	}

	err = database.Initialize(tmpDir, nil)
	if err != nil {
		return "", err
	}

	_, err = database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "hashmap",
		PrimaryAPI:  "",
	})
	if err != nil {
		return "", err
	}

	_, err = database.Register(&database.Database{
		Name:        "cache",
		Description: "Cached data, such as Intelligence and DNS Records",
		StorageType: "hashmap",
		PrimaryAPI:  "",
	})
	if err != nil {
		return "", err
	}

	// _, err = database.Register(&database.Database{
	//   Name:        "history",
	//   Description: "Historic event data",
	//   StorageType: "hashmap",
	//   PrimaryAPI:  "",
	// })
	// if err != nil {
	//   return err
	// }

	// start logging
	err = log.Start()
	if err != nil {
		return "", err
	}
	log.SetLogLevel(log.TraceLevel)

	return tmpDir, nil
}

// StopTesting shuts the test environment.
func StopTesting() {
	log.Shutdown()
}
