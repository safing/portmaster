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
	cfgOptionPermanentVerdictsOrder = 96
	permanentVerdicts               config.BoolOption

	devMode          config.BoolOption
	apiListenAddress config.StringOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Permanent Verdicts",
		Key:            CfgOptionPermanentVerdictsKey,
		Description:    "The Portmaster's system integration intercepts every single packet. Usually the first packet is enough for the Portmaster to set the verdict for a connection - ie. to allow or deny it. Making these verdicts permanent means that the Portmaster will tell the system integration that is does not want to see any more packets of that single connection. This brings a major performance increase.",
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
		Description:    "How long the Portmaster will wait for a reply to a prompt notification. Please note that Desktop Notifications might not respect this or have their own limits.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   20,
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
