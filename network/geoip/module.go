package geoip

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("geoip", nil, start, nil, "updates")
}

func start() error {
	err := prepDatabaseForUse()
	if err != nil {
		return fmt.Errorf("goeip: failed to load databases: %s", err)
	}

	module.RegisterEventHook(
		"updates",
		"resource update",
		"upgrade databases",
		upgradeDatabases,
	)

	// TODO: replace with update subscription
	module.NewTask("update databases", func(ctx context.Context, task *modules.Task) {

		dbFileLock.Lock()
		defer dbFileLock.Unlock()

	}).Repeat(10 * time.Minute).MaxDelay(1 * time.Hour)

	return nil
}

func upgradeDatabases(_ context.Context, _ interface{}) error {
	dbFileLock.Lock()
	reload := false
	if dbCityFile != nil && dbCityFile.UpgradeAvailable() {
		reload = true
	}
	if dbASNFile != nil && dbASNFile.UpgradeAvailable() {
		reload = true
	}
	dbFileLock.Unlock()

	if reload {
		return ReloadDatabases()
	}
	return nil
}
