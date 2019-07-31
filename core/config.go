package core

import (
	"github.com/safing/portbase/config"
)

var (
	devMode config.BoolOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Development Mode",
		Key:            "core/devMode",
		Description:    "In Development Mode security restrictions are lifted/softened to enable easier access to Portmaster for debugging and testing purposes. This is potentially very insecure, only activate if you know what you are doing.",
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		OptType:        config.OptTypeBool,
		DefaultValue:   true,
	})
	if err != nil {
		return err
	}

	return nil
}
