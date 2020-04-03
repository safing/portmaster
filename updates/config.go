package updates

import (
	"context"
	"fmt"

	"github.com/safing/portbase/config"
)

var (
	releaseChannel config.StringOption
	devMode        config.BoolOption

	previousReleaseChannel string
	previousDevMode        bool
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
		ExternalOptType: "string list",
		ValidationRegex: fmt.Sprintf("^(%s|%s)$", releaseChannelStable, releaseChannelBeta),
	})
	if err != nil {
		return err
	}

	return module.RegisterEventHook("config", "config change", "update registry config", updateRegistryConfig)
}

func initConfig() {
	releaseChannel = config.GetAsString(releaseChannelKey, releaseChannelStable)
	devMode = config.GetAsBool("core/devMode", false)
}

func updateRegistryConfig(_ context.Context, _ interface{}) error {
	changed := false
	if releaseChannel() != previousReleaseChannel {
		registry.SetBeta(releaseChannel() == releaseChannelBeta)
		previousReleaseChannel = releaseChannel()
		changed = true
	}

	if devMode() != previousDevMode {
		registry.SetBeta(devMode())
		previousDevMode = devMode()
		changed = true
	}

	if changed {
		registry.SelectVersions()
		module.TriggerEvent(VersionUpdateEvent, nil)
	}

	return nil
}
