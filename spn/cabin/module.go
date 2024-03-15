package cabin

import (
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/spn/conf"
)

var module *modules.Module

func init() {
	module = modules.Register("cabin", prep, nil, nil, "base", "rng")
}

func prep() error {
	if err := initProvidedExchKeySchemes(); err != nil {
		return err
	}

	if conf.PublicHub() {
		if err := prepPublicHubConfig(); err != nil {
			return err
		}
	}

	return nil
}
