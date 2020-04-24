package updates

import (
	"context"
	"fmt"

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
		Order:           1,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
		RequiresRestart: false,
		DefaultValue:    releaseChannelStable,
		ExternalOptType: "string list",
		ValidationRegex: fmt.Sprintf("^(%s|%s)$", releaseChannelStable, releaseChannelBeta),
	})
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:            "Disable Updates",
		Key:             disableUpdatesKey,
		Description:     "Disable automatic updates.",
		Order:           64,
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    false,
		ExternalOptType: "disable updates",
	})
	if err != nil {
		return err
	}

	return nil
}

func initConfig() {
	releaseChannel = config.GetAsString(releaseChannelKey, releaseChannelStable)
	disableUpdates = config.GetAsBool(disableUpdatesKey, false)

	devMode = config.GetAsBool("core/devMode", false)
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
		} else {
			module.Warning(updateFailed, "Updates are disabled!")
			log.Warningf("updates: automatic updates are now disabled.")
		}
	}

	return nil
}
