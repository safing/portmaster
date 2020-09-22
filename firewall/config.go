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
		ExpertiseLevel: config.ExpertiseLevelExpert,
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
		Name:           "Ask with System Notifications",
		Key:            CfgOptionAskWithSystemNotificationsKey,
		Description:    `Ask about connections using your operating system's notification system. For this to be enabled, the setting "Use System Notifications" must enabled too. This only affects questions from the Privacy Filter, and does not affect alerts from the Portmaster.`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskWithSystemNotificationsOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:           "Timeout for Ask Notifications",
		Key:            CfgOptionAskTimeoutKey,
		Description:    "Amount of time (in seconds) how long the Portmaster will wait for a response when prompting about a connection via a notification. Please note that system notifications might not respect this or have it's own limits.",
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
	askTimeout = config.Concurrent.GetAsInt(CfgOptionAskTimeoutKey, 60)

	devMode = config.Concurrent.GetAsBool(core.CfgDevModeKey, false)
	apiListenAddress = config.GetAsString(api.CfgDefaultListenAddressKey, "")

	return nil
}
