package firewall

import (
	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/core"
)

// Configuration Keys
var (
	CfgOptionEnableFilterKey = "filter/enable"

	CfgOptionAskWithSystemNotificationsKey   = "filter/askWithSystemNotifications"
	cfgOptionAskWithSystemNotificationsOrder = 2

	CfgOptionAskTimeoutKey   = "filter/askTimeout"
	cfgOptionAskTimeoutOrder = 3
	askTimeout               config.IntOption

	CfgOptionPermanentVerdictsKey   = "filter/permanentVerdicts"
	cfgOptionPermanentVerdictsOrder = 128
	permanentVerdicts               config.BoolOption

	devMode          config.BoolOption
	apiListenAddress config.StringOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Permanent Verdicts",
		Key:            CfgOptionPermanentVerdictsKey,
		Description:    "With permanent verdicts, control of a connection is fully handed back to the OS after the initial decision. This brings a great performance increase, but makes it impossible to change the decision of a link later on.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionPermanentVerdictsOrder,
			config.CategoryAnnotation:     "Advanced",
		},
	})
	if err != nil {
		return err
	}
	permanentVerdicts = config.Concurrent.GetAsBool(CfgOptionPermanentVerdictsKey, true)

	err = config.Register(&config.Option{
		Name:           "Prompt Desktop Notifications",
		Key:            CfgOptionAskWithSystemNotificationsKey,
		Description:    `In addition to showing prompt notifications in the Portmaster App, also send them to the Desktop. This requires the Portmaster Notifier to be running. Requires Desktop Notifications to be enabled.`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskWithSystemNotificationsOrder,
			config.CategoryAnnotation:     "General",
			config.RequiresAnnotation: config.ValueRequirement{
				Key:   core.CfgUseSystemNotificationsKey,
				Value: true,
			},
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:           "Prompt Timeout",
		Key:            CfgOptionAskTimeoutKey,
		Description:    "How long the Portmaster will wait for a reply to a prompt notification. Please note that Desktop Notifications might not respect this or have it's own limits.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   60,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskTimeoutOrder,
			config.UnitAnnotation:         "seconds",
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}
	askTimeout = config.Concurrent.GetAsInt(CfgOptionAskTimeoutKey, 15)

	devMode = config.Concurrent.GetAsBool(core.CfgDevModeKey, false)
	apiListenAddress = config.GetAsString(api.CfgDefaultListenAddressKey, "")

	return nil
}
