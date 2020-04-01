package core

import (
	"flag"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
)

var (
	CfgDevModeKey  = "core/devMode"
	defaultDevMode bool
)

func init() {
	flag.BoolVar(&defaultDevMode, "devmode", false, "force development mode")
}

func logFlagOverrides() {
	if defaultDevMode {
		log.Warning("core: core/devMode default config is being forced by -devmode flag")
	}
}

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Development Mode",
		Key:            CfgDevModeKey,
		Description:    "In Development Mode security restrictions are lifted/softened to enable easier access to Portmaster for debugging and testing purposes.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   defaultDevMode,
	})
	if err != nil {
		return err
	}

	return nil
}
