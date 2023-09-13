package core

import (
	"flag"

	"github.com/safing/portbase/config"
)

// Configuration Keys.
var (
	// CfgDevModeKey was previously defined here.
	CfgDevModeKey = config.CfgDevModeKey

	CfgNetworkServiceKey      = "core/networkService"
	defaultNetworkServiceMode bool

	CfgLocaleKey = "core/localeID"
)

func init() {
	flag.BoolVar(
		&defaultNetworkServiceMode,
		"network-service",
		false,
		"set default network service mode; configuration is stronger",
	)
}

func registerConfig() error {
	if err := config.Register(&config.Option{
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
	}); err != nil {
		return err
	}

	if err := config.Register(&config.Option{
		Name:           "Locale",
		Key:            CfgLocaleKey,
		Description:    "Configures the locale for the user interface. This mainly affects rendering of dates, currency and numbers. Note that the Portmaster does not yet support different languages.",
		OptType:        config.OptTypeString,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   "en-GB",
		PossibleValues: []config.PossibleValue{
			{
				Name:  "en-GB",
				Value: "en-GB",
			},
			{
				Name:  "en-US",
				Value: "en-US",
			},
		},
		Annotations: config.Annotations{
			config.CategoryAnnotation:         "User Interface",
			config.DisplayHintAnnotation:      config.DisplayHintOneOf,
			config.RequiresUIReloadAnnotation: true,
		},
	}); err != nil {
		return err
	}

	return nil
}
