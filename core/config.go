package core

import (
	"flag"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
)

// Configuration Keys
var (
	CfgDevModeKey  = "core/devMode"
	defaultDevMode bool

	CfgUseSystemNotificationsKey = "core/useSystemNotifications"
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
		Description:    "In Development Mode, security restrictions are lifted/softened to enable easier access to Portmaster for debugging and testing purposes.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   defaultDevMode,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 512,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:           "Desktop Notifications",
		Key:            CfgUseSystemNotificationsKey,
		Description:    "In addition to showing notifications in the Portmaster App, also send them to the Desktop. This requires the Portmaster Notifier to be running.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true, // TODO: turn off by default on unsupported systems
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -15,
			config.CategoryAnnotation:     "User Interface",
		},
	})
	if err != nil {
		return err
	}

	return nil
}
