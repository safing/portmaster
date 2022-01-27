package updates

import (
	"context"

	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/updates/helper"
	"github.com/tevino/abool"

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

	initialReleaseChannel   string
	previousReleaseChannel  string
	updatesCurrentlyEnabled bool
	previousDevMode         bool
	forceUpdate             = abool.New()
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "Release Channel",
		Key:             helper.ReleaseChannelKey,
		Description:     `Use "Stable" for the best experience. The "Beta" channel will have the newest features and fixes, but may also break and cause interruption. Use others only temporarily and when instructed.`,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: true,
		DefaultValue:    helper.ReleaseChannelStable,
		PossibleValues: []config.PossibleValue{
			{
				Name:        "Stable",
				Description: "Production releases.",
				Value:       helper.ReleaseChannelStable,
			},
			{
				Name:        "Beta",
				Description: "Production releases for testing new features that may break and cause interruption.",
				Value:       helper.ReleaseChannelBeta,
			},
			{
				Name:        "Support",
				Description: "Support releases or version changes for troubleshooting. Only use temporarily and when instructed.",
				Value:       helper.ReleaseChannelSupport,
			},
			{
				Name:        "Staging",
				Description: "Dangerous development releases for testing random things and experimenting. Only use temporarily and when instructed.",
				Value:       helper.ReleaseChannelStaging,
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
	releaseChannel = config.Concurrent.GetAsString(helper.ReleaseChannelKey, helper.ReleaseChannelStable)
	initialReleaseChannel = releaseChannel()
	previousReleaseChannel = releaseChannel()

	enableUpdates = config.Concurrent.GetAsBool(enableUpdatesKey, true)
	updatesCurrentlyEnabled = enableUpdates()

	devMode = config.Concurrent.GetAsBool(cfgDevModeKey, false)
	previousDevMode = devMode()
}

func createWarningNotification() {
	notifications.NotifyWarn(
		updatesDisabledNotificationID,
		"Automatic Updates Disabled",
		"Automatic updates are disabled through configuration. Please note that this is potentially dangerous, as this also affects security updates as well as the filter lists and threat intelligence feeds.",
		notifications.Action{
			Text: "Change",
			Type: notifications.ActionTypeOpenSetting,
			Payload: &notifications.ActionTypeOpenSettingPayload{
				Key: enableUpdatesKey,
			},
		},
	).AttachToModule(module)
}

func updateRegistryConfig(_ context.Context, _ interface{}) error {
	changed := false

	if releaseChannel() != previousReleaseChannel {
		previousReleaseChannel = releaseChannel()
		warning := helper.SetIndexes(registry, releaseChannel(), true)
		if warning != nil {
			log.Warningf("updates: %s", warning)
		}
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
			if err := TriggerUpdate(false); err != nil {
				log.Warningf("updates: failed to trigger update: %s", err)
			}
			log.Infof("updates: automatic updates are now enabled")
		} else {
			createWarningNotification()
			log.Warningf("updates: automatic updates are now disabled")
		}
	}

	return nil
}
