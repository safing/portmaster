package sync

import (
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/modules"
)

var (
	module *modules.Module

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})
)

func init() {
	module = modules.Register("sync", prep, nil, nil, "profiles")
}

func prep() error {
	if err := registerSettingsAPI(); err != nil {
		return err
	}
	if err := registerSingleSettingAPI(); err != nil {
		return err
	}
	if err := registerProfileAPI(); err != nil {
		return err
	}
	return nil
}
