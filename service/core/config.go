package core

import (
	"flag"

	locale "github.com/Xuanwo/go-locale"
	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
)

// Configuration Keys.
var (
	// CfgDevModeKey was previously defined here.
	CfgDevModeKey = config.CfgDevModeKey

	CfgNetworkServiceKey      = "core/networkService"
	defaultNetworkServiceMode bool

	CfgLocaleKey = "core/locale"
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
		Name:           "Time and Date Format",
		Key:            CfgLocaleKey,
		Description:    "Configures the time and date format for the user interface. Selection is an example and correct formatting in the UI is a continual work in progress.",
		OptType:        config.OptTypeString,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   getDefaultLocale(),
		PossibleValues: []config.PossibleValue{
			{
				Name:  "24h DD-MM-YYYY",
				Value: enGBLocale,
			},
			{
				Name:  "12h MM/DD/YYYY",
				Value: enUSLocale,
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

func getDefaultLocale() string {
	// Get locales from system.
	detectedLocales, err := locale.DetectAll()
	if err != nil {
		log.Warningf("core: failed to detect locale: %s", err)
		return enGBLocale
	}

	// log.Debugf("core: detected locales: %s", detectedLocales)

	// Check if there is a locale that corresponds to the en-US locale.
	for _, detectedLocale := range detectedLocales {
		if slices.Contains[[]string, string](defaultEnUSLocales, detectedLocale.String()) {
			return enUSLocale
		}
	}

	// Otherwise, return the en-GB locale as default.
	return enGBLocale
}

var (
	enGBLocale = "en-GB"
	enUSLocale = "en-US"

	defaultEnUSLocales = []string{
		"en-AS", // English (American Samoa)
		"en-GU", // English (Guam)
		"en-UM", // English (U.S. Minor Outlying Islands)
		"en-US", // English (United States)
		"en-VI", // English (U.S. Virgin Islands)
	}
)
