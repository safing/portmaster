package core

import (
	"sync"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"

	"github.com/safing/portmaster/core/structure"
)

var (
	maintenanceWg                sync.WaitGroup
	maintenanceShortTickDuration = 10 * time.Minute
	maintenanceLongTickDuration  = 1 * time.Hour
)

func startDB() error {
	err := database.Initialize(dataDir, structure.Root())
	if err == nil {
		maintenanceWg.Add(1)
		go maintenanceWorker()
	}
	return err
}

func stopDB() error {
	maintenanceWg.Wait()
	return database.Shutdown()
}

func maintenanceWorker() {
	ticker := time.NewTicker(maintenanceShortTickDuration)
	longTicker := time.NewTicker(maintenanceLongTickDuration)

	for {
		select {
		case <-ticker.C:
			err := database.Maintain()
			if err != nil {
				log.Errorf("database: maintenance error: %s", err)
			}
		case <-longTicker.C:
			err := database.MaintainRecordStates()
			if err != nil {
				log.Errorf("database: record states maintenance error: %s", err)
			}
			err = database.MaintainThorough()
			if err != nil {
				log.Errorf("database: thorough maintenance error: %s", err)
			}
		case <-shuttingDown:
			maintenanceWg.Done()
			return
		}
	}
}
