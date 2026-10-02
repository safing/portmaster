package core

import (
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/mgr"
)

// Release Channel Configuration Keys.
const (
	ReleaseChannelKey     = "core/releaseChannel"
	ReleaseChannelJSONKey = "core.releaseChannel"
)

// Release Channels.
const (
	ReleaseChannelStable  = "stable"
	ReleaseChannelBeta    = "beta"
	ReleaseChannelStaging = "staging"
	ReleaseChannelSupport = "support"
)

const (
	enableSoftwareUpdatesKey = "core/automaticUpdates"
	enableIntelUpdatesKey    = "core/automaticIntelUpdates"
)

var (
	releaseChannel        config.StringOption
	enableSoftwareUpdates config.BoolOption
	enableIntelUpdates    config.BoolOption

	initialReleaseChannel string
)

func registerUpdateConfig() error {
	err := config.Register(&config.Option{
		Name:            "发布渠道",
		Key:             ReleaseChannelKey,
		Description:     `使用“稳定”渠道可获得最佳体验。“测试”渠道包含最新的功能和修复，但也可能出现故障并导致中断。其他渠道请仅在得到指示时临时使用。`,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: true,
		DefaultValue:    ReleaseChannelStable,
		PossibleValues: []config.PossibleValue{
			{
				Name:        "稳定",
				Description: "正式发布版本。",
				Value:       ReleaseChannelStable,
			},
			{
				Name:        "测试",
				Description: "用于测试新功能的正式发布版本，可能出现故障并导致中断。",
				Value:       ReleaseChannelBeta,
			},
			{
				Name:        "支持",
				Description: "用于故障排除的支持版本或版本变更。请仅在得到指示时临时使用。",
				Value:       ReleaseChannelSupport,
			},
			{
				Name:        "预发布",
				Description: "用于测试和实验的危险开发版本。请仅在得到指示时临时使用。",
				Value:       ReleaseChannelStaging,
			},
		},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -4,
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.CategoryAnnotation:     "更新",
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:            "自动软件更新",
		Key:             enableSoftwareUpdatesKey,
		Description:     "自动检查并下载软件更新。这不包括情报数据更新。",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -12,
			config.CategoryAnnotation:     "更新",
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:            "自动情报数据更新",
		Key:             enableIntelUpdatesKey,
		Description:     "自动检查并下载情报数据更新，包括过滤列表、地理 IP 数据等。不包括软件更新。",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -11,
			config.CategoryAnnotation:     "更新",
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func initUpdateConfig() {
	releaseChannel = config.Concurrent.GetAsString(ReleaseChannelKey, ReleaseChannelStable)
	enableSoftwareUpdates = config.Concurrent.GetAsBool(enableSoftwareUpdatesKey, true)
	enableIntelUpdates = config.Concurrent.GetAsBool(enableIntelUpdatesKey, true)

	initialReleaseChannel = releaseChannel()

	module.instance.Config().EventConfigChange.AddCallback("configure updates", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		configureUpdates()
		return false, nil
	})
	configureUpdates()
}

func configureUpdates() {
	module.instance.BinaryUpdates().Configure(enableSoftwareUpdates(), configure.GetBinaryUpdateURLs(releaseChannel()))
	module.instance.IntelUpdates().Configure(enableIntelUpdates(), configure.DefaultIntelIndexURLs)
}
