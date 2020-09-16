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
		Description:    "In Development Mode security restrictions are lifted/softened to enable easier access to Portmaster for debugging and testing purposes.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   defaultDevMode,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 127,
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:           "Use System Notifications",
		Key:            CfgUseSystemNotificationsKey,
		Description:    "Send notifications to your operating system's notification system. When this setting is turned off, notifications will only be visible in the Portmaster App. This affects both alerts from the Portmaster and questions from the Privacy Filter.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true, // TODO: turn off by default on unsupported systems
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 32,
		},
	})
	if err != nil {
		return err
	}

	return nil
}
