package firewall

import (
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/spn/captain"
)

// Configuration Keys.
var (
	CfgOptionEnableFilterKey = "filter/enable"
	filterEnabled            config.BoolOption

	CfgOptionAskWithSystemNotificationsKey   = "filter/askWithSystemNotifications"
	cfgOptionAskWithSystemNotificationsOrder = 2
	askWithSystemNotifications               config.BoolOption

	CfgOptionAskTimeoutKey   = "filter/askTimeout"
	cfgOptionAskTimeoutOrder = 3
	askTimeout               config.IntOption

	CfgOptionPermanentVerdictsKey   = "filter/permanentVerdicts"
	cfgOptionPermanentVerdictsOrder = 80
	permanentVerdicts               config.BoolOption

	CfgOptionDNSQueryInterceptionKey   = "filter/dnsQueryInterception"
	cfgOptionDNSQueryInterceptionOrder = 81
	dnsQueryInterception               config.BoolOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Enable Privacy Filter",
		Key:            CfgOptionEnableFilterKey,
		Description:    "Enable the Privacy Filter. If turned off, all privacy filter protections are fully disabled on this device. Not meant to be disabled in production - only turn off for testing.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.CategoryAnnotation: "General",
		},
	})
	if err != nil {
		return err
	}
	filterEnabled = config.Concurrent.GetAsBool(CfgOptionEnableFilterKey, true)

	err = config.Register(&config.Option{
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
		Name:           "Seamless DNS Integration",
		Key:            CfgOptionDNSQueryInterceptionKey,
		Description:    "Intercept and redirect astray DNS queries to the Portmaster's internal DNS server. This enables seamless DNS integration without having to configure the system or other software. However, this may lead to compatibility issues with other software that attempts the same.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDNSQueryInterceptionOrder,
			config.CategoryAnnotation:     "Advanced",
		},
	})
	if err != nil {
		return err
	}
	dnsQueryInterception = config.Concurrent.GetAsBool(CfgOptionDNSQueryInterceptionKey, true)

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
				Key:   notifications.CfgUseSystemNotificationsKey,
				Value: true,
			},
		},
	})
	if err != nil {
		return err
	}
	askWithSystemNotifications = config.Concurrent.GetAsBool(CfgOptionAskWithSystemNotificationsKey, true)

	err = config.Register(&config.Option{
		Name:           "Prompt Timeout",
		Key:            CfgOptionAskTimeoutKey,
		Description:    "How long the Portmaster will wait for a reply to a prompt notification. Please note that Desktop Notifications might not respect this or have their own limits.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   60,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskTimeoutOrder,
			config.UnitAnnotation:         "seconds",
			config.CategoryAnnotation:     "General",
		},
		ValidationRegex: `^[1-9][0-9]{1,5}$`,
	})
	if err != nil {
		return err
	}
	askTimeout = config.Concurrent.GetAsInt(CfgOptionAskTimeoutKey, 60)

	return nil
}

// Config variables for interception and filter module.
// Everything is registered by the interception module, as the filter module
// can be disabled.
var (
	devMode          config.BoolOption
	apiListenAddress config.StringOption

	tunnelEnabled     config.BoolOption
	useCommunityNodes config.BoolOption

	configReady = abool.New()
)

func getConfig() {
	devMode = config.Concurrent.GetAsBool(core.CfgDevModeKey, false)
	apiListenAddress = config.GetAsString(api.CfgDefaultListenAddressKey, "")

	tunnelEnabled = config.Concurrent.GetAsBool(captain.CfgOptionEnableSPNKey, false)
	useCommunityNodes = config.Concurrent.GetAsBool(captain.CfgOptionUseCommunityNodesKey, true)

	configReady.Set()
}
