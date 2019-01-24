package intel

import (
	"os"
	"testing"

	"github.com/Safing/portbase/database/dbmodule"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portbase/modules"
)

func TestMain(m *testing.M) {
	// setup
	testDir := os.TempDir()
	dbmodule.SetDatabaseLocation(testDir)
	err := modules.Start()
	if err != nil {
		if err == modules.ErrCleanExit {
			os.Exit(0)
		} else {
			err = modules.Shutdown()
			if err != nil {
				log.Shutdown()
			}
			os.Exit(1)
		}
	}

	// run tests
	rv := m.Run()

	// teardown
	modules.Shutdown()
	os.RemoveAll(testDir)

	// exit with test run return value
	os.Exit(rv)
}
