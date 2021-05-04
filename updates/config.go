package updates

import (
	"context"

	"github.com/safing/portbase/notifications"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
)

const (
	cfgDevModeKey                 = "core/devMode"
	updatesDisabledNotificationID = "updates:disabled"
)

var (
	releaseChannel config.StringOption
	devMode        config.BoolOption
	enableUpdates  config.BoolOption

	previousReleaseChannel  string
	updatesCurrentlyEnabled bool
	previousDevMode         bool
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "Release Channel",
		Key:             releaseChannelKey,
		Description:     "Switch release channel.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelExperimental,
		RequiresRestart: false,
		DefaultValue:    releaseChannelStable,
		PossibleValues: []config.PossibleValue{
			{
				Name:  "Stable",
				Value: releaseChannelStable,
			},
			{
				Name:  "Beta",
				Value: releaseChannelBeta,
			},
		},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -4,
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.CategoryAnnotation:     "Updates",
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:            "Automatic Updates",
		Key:             enableUpdatesKey,
		Description:     "Enable automatic checking, downloading and applying of updates. This affects all kinds of updates, including intelligence feeds and broadcast notifications.",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -12,
			config.CategoryAnnotation:     "Updates",
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func initConfig() {
	releaseChannel = config.GetAsString(releaseChannelKey, releaseChannelStable)
	previousReleaseChannel = releaseChannel()

	enableUpdates = config.GetAsBool(enableUpdatesKey, true)
	updatesCurrentlyEnabled = enableUpdates()

	devMode = config.GetAsBool(cfgDevModeKey, false)
	previousDevMode = devMode()
}

func updateRegistryConfig(_ context.Context, _ interface{}) error {
	changed := false

	if releaseChannel() != previousReleaseChannel {
		registry.SetBeta(releaseChannel() == releaseChannelBeta)
		previousReleaseChannel = releaseChannel()
		changed = true
	}

	if devMode() != previousDevMode {
		registry.SetDevMode(devMode())
		previousDevMode = devMode()
		changed = true
	}

	if enableUpdates() != updatesCurrentlyEnabled {
		updatesCurrentlyEnabled = enableUpdates()
		changed = true
	}

	if changed {
		registry.SelectVersions()
		module.TriggerEvent(VersionUpdateEvent, nil)

		if updatesCurrentlyEnabled {
			module.Resolve("")
			if err := TriggerUpdate(); err != nil {
				log.Warningf("updates: failed to trigger update: %s", err)
			}
			log.Infof("updates: automatic updates are now enabled")
		} else {
			notifications.NotifyWarn(
				updatesDisabledNotificationID,
				"Automatic Updates Disabled",
				"The automatic update system is disabled through configuration. Please note that this is potentially dangerous, as this also affects security updates as well as the filter lists and threat intelligence feeds.",
				notifications.Action{
					ID:   "change",
					Text: "Change",
					Type: notifications.ActionTypeOpenSetting,
					Payload: &notifications.ActionTypeOpenSettingPayload{
						Key: enableUpdatesKey,
					},
				},
			).AttachToModule(module)
			log.Warningf("updates: automatic updates are now disabled")
		}
	}

	return nil
}
