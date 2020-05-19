package geoip

import (
	"context"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("geoip", prep, nil, nil, "base", "updates")
}

func prep() error {
	return module.RegisterEventHook(
		updates.ModuleName,
		updates.ResourceUpdateEvent,
		"Check for GeoIP database updates",
		upgradeDatabases,
	)
}

func upgradeDatabases(_ context.Context, _ interface{}) error {
	dbFileLock.Lock()
	reload := false
	if geoDBv4File != nil && geoDBv4File.UpgradeAvailable() {
		reload = true
	}
	if geoDBv6File != nil && geoDBv6File.UpgradeAvailable() {
		reload = true
	}
	dbFileLock.Unlock()

	if reload {
		return ReloadDatabases()
	}
	return nil
}
