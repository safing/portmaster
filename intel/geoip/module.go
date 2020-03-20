package geoip

import (
	"context"

	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("geoip", prep, nil, nil, "updates")
}

func prep() error {
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
