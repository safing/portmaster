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
		Name:           "网络服务",
		Key:            CfgNetworkServiceKey,
		Description:    "在适用的情况下，将 Portmaster 用作网络服务。您需要自行处理大量网络设置，才能正确且安全地运行此功能。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   defaultNetworkServiceMode,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 513,
			config.CategoryAnnotation:     "网络服务",
		},
	}); err != nil {
		return err
	}

	if err := config.Register(&config.Option{
		Name:           "时间和日期格式",
		Key:            CfgLocaleKey,
		Description:    "配置用户界面的时间和日期格式。选项仅为示例，界面中的正确格式化仍在持续完善中。",
		OptType:        config.OptTypeString,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   getDefaultLocale(),
		PossibleValues: []config.PossibleValue{
			{
				Name:  "24 小时制 DD-MM-YYYY",
				Value: enGBLocale,
			},
			{
				Name:  "12 小时制 MM/DD/YYYY",
				Value: enUSLocale,
			},
		},
		Annotations: config.Annotations{
			config.CategoryAnnotation:         "用户界面",
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
