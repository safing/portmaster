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
		Name:           "启用隐私过滤器",
		Key:            CfgOptionEnableFilterKey,
		Description:    "启用隐私过滤器。如果关闭，此设备上的所有隐私过滤保护都将完全禁用。不应在正式使用中禁用 —— 仅在测试时关闭。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.CategoryAnnotation: "常规",
		},
	})
	if err != nil {
		return err
	}
	filterEnabled = config.Concurrent.GetAsBool(CfgOptionEnableFilterKey, true)

	err = config.Register(&config.Option{
		Name:           "永久判定",
		Key:            CfgOptionPermanentVerdictsKey,
		Description:    "Portmaster 的系统集成会拦截每一个数据包。通常第一个数据包就足以让 Portmaster 对连接做出判定 —— 即允许或拒绝。将这些判定设为永久，意味着 Portmaster 会告知系统集成不再需要查看该连接的后续数据包。这会带来显著的性能提升。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionPermanentVerdictsOrder,
			config.CategoryAnnotation:     "高级",
		},
	})
	if err != nil {
		return err
	}
	permanentVerdicts = config.Concurrent.GetAsBool(CfgOptionPermanentVerdictsKey, true)

	err = config.Register(&config.Option{
		Name:           "无缝 DNS 集成",
		Key:            CfgOptionDNSQueryInterceptionKey,
		Description:    "拦截游离的 DNS 查询并将其重定向到 Portmaster 的内部 DNS 服务器。这样无需配置系统或其他软件即可实现无缝 DNS 集成。但这可能与尝试执行相同操作的其他软件产生兼容性问题。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDNSQueryInterceptionOrder,
			config.CategoryAnnotation:     "高级",
		},
	})
	if err != nil {
		return err
	}
	dnsQueryInterception = config.Concurrent.GetAsBool(CfgOptionDNSQueryInterceptionKey, true)

	err = config.Register(&config.Option{
		Name:           "询问桌面通知",
		Key:            CfgOptionAskWithSystemNotificationsKey,
		Description:    `除了在 Portmaster 应用中显示询问通知外，还将其发送到桌面。这需要 Portmaster Notifier 正在运行，并且需要启用桌面通知。`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskWithSystemNotificationsOrder,
			config.CategoryAnnotation:     "常规",
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
		Name:           "询问超时",
		Key:            CfgOptionAskTimeoutKey,
		Description:    "Portmaster 等待询问通知回复的时长。请注意，桌面通知可能不遵守此设置，或有其自身的限制。",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   60,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskTimeoutOrder,
			config.UnitAnnotation:         "秒",
			config.CategoryAnnotation:     "常规",
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
