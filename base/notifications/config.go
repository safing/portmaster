package notifications

import (
	"github.com/safing/portmaster/base/config"
)

// Configuration Keys.
var (
	CfgUseSystemNotificationsKey = "core/useSystemNotifications"
	useSystemNotifications       config.BoolOption
)

func registerConfig() error {
	if err := config.Register(&config.Option{
		Name:           "桌面通知",
		Key:            CfgUseSystemNotificationsKey,
		Description:    "除了在 Portmaster 应用中显示通知外，还将通知发送到桌面。这需要 Portmaster Notifier 正在运行。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true, // TODO: turn off by default on unsupported systems
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -15,
			config.CategoryAnnotation:     "用户界面",
		},
	}); err != nil {
		return err
	}
	useSystemNotifications = config.Concurrent.GetAsBool(CfgUseSystemNotificationsKey, true)

	return nil
}
