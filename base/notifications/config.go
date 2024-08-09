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
		Name:           "Desktop Notifications",
		Key:            CfgUseSystemNotificationsKey,
		Description:    "In addition to showing notifications in the Portmaster App, also send them to the Desktop. This requires the Portmaster Notifier to be running.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   true, // TODO: turn off by default on unsupported systems
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -15,
			config.CategoryAnnotation:     "User Interface",
		},
	}); err != nil {
		return err
	}
	useSystemNotifications = config.Concurrent.GetAsBool(CfgUseSystemNotificationsKey, true)

	return nil
}
