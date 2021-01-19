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

	CfgNetworkServiceKey      = "core/networkService"
	defaultNetworkServiceMode bool

	CfgUseSystemNotificationsKey = "core/useSystemNotifications"
)

func init() {
	flag.BoolVar(&defaultDevMode, "devmode", false, "force development mode")
	flag.BoolVar(&defaultNetworkServiceMode, "network-service", false, "force network service mode")
}

func logFlagOverrides() {
	if defaultDevMode {
		log.Warningf("core: %s config is being forced by the -devmode flag", CfgDevModeKey)
	}
	if defaultNetworkServiceMode {
		log.Warningf("core: %s config is being forced by the -network-service flag", CfgNetworkServiceKey)
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
		Name:           "Network Service",
		Key:            CfgNetworkServiceKey,
		Description:    "Use the Portmaster as a network service, where applicable. You will have to take care of lots of network setup yourself in order to run this properly and securely.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   defaultNetworkServiceMode,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 513,
			config.CategoryAnnotation:     "Network Service",
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
