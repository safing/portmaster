package updates

import (
	"context"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
)

const (
	cfgDevModeKey = "core/devMode"
)

var (
	releaseChannel config.StringOption
	devMode        config.BoolOption
	disableUpdates config.BoolOption

	previousReleaseChannel   string
	updatesCurrentlyDisabled bool
	previousDevMode          bool
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "Release Channel",
		Key:             releaseChannelKey,
		Description:     "The Release Channel changes which updates are applied. When using beta, you will receive new features earlier and Portmaster will update more frequently. Some beta or experimental features are also available in the stable release channel.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
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
			config.DisplayOrderAnnotation: 1,
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
		},
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:            "Disable Updates",
		Key:             disableUpdatesKey,
		Description:     "Disable automatic updates.",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 64,
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

	disableUpdates = config.GetAsBool(disableUpdatesKey, false)
	updatesCurrentlyDisabled = disableUpdates()

	devMode = config.GetAsBool(cfgDevModeKey, false)
	previousDevMode = devMode()
}

func updateRegistryConfig(_ context.Context, _ interface{}) error {
	changed := false
	forceUpdate := false

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

	if disableUpdates() != updatesCurrentlyDisabled {
		updatesCurrentlyDisabled = disableUpdates()
		changed = true
		forceUpdate = !updatesCurrentlyDisabled
	}

	if changed {
		registry.SelectVersions()
		module.TriggerEvent(VersionUpdateEvent, nil)

		if forceUpdate {
			module.Resolve(updateFailed)
			_ = TriggerUpdate()
			log.Infof("updates: automatic updates enabled again.")
		} else if updatesCurrentlyDisabled {
			module.Warning(updateFailed, "Automatic updates are disabled! This also affects security updates and threat intelligence.")
			log.Warningf("updates: automatic updates are now disabled.")
		}
	}

	return nil
}
