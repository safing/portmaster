package geoip

import (
	"context"
	"fmt"

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

	return module.RegisterEventHook(
		"updates",
		"resource update",
		"upgrade databases",
		upgradeDatabases,
	)
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
