package updates

import (
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/helper"
)

const cfgDevModeKey = "core/devMode"

var (
	releaseChannel        config.StringOption
	devMode               config.BoolOption
	enableSoftwareUpdates config.BoolOption
	enableIntelUpdates    config.BoolOption

	initialReleaseChannel  string
	previousReleaseChannel string

	softwareUpdatesCurrentlyEnabled bool
	intelUpdatesCurrentlyEnabled    bool
	previousDevMode                 bool
	forceCheck                      = abool.New()
	forceDownload                   = abool.New()
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
		Name:            "Automatic Software Updates",
		Key:             enableSoftwareUpdatesKey,
		Description:     "Automatically check for and download software updates. This does not include intelligence data updates.",
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

	err = config.Register(&config.Option{
		Name:            "Automatic Intelligence Data Updates",
		Key:             enableIntelUpdatesKey,
		Description:     "Automatically check for and download intelligence data updates. This includes filter lists, geo-ip data, and more. Does not include software updates.",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		RequiresRestart: false,
		DefaultValue:    true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: -11,
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

	enableSoftwareUpdates = config.Concurrent.GetAsBool(enableSoftwareUpdatesKey, true)
	enableIntelUpdates = config.Concurrent.GetAsBool(enableIntelUpdatesKey, true)
	softwareUpdatesCurrentlyEnabled = enableSoftwareUpdates()
	intelUpdatesCurrentlyEnabled = enableIntelUpdates()

	devMode = config.Concurrent.GetAsBool(cfgDevModeKey, false)
	previousDevMode = devMode()
}

func updateRegistryConfig(_ *mgr.WorkerCtx, _ struct{}) (cancel bool, err error) {
	changed := false

	if enableSoftwareUpdates() != softwareUpdatesCurrentlyEnabled {
		softwareUpdatesCurrentlyEnabled = enableSoftwareUpdates()
		changed = true
	}

	if enableIntelUpdates() != intelUpdatesCurrentlyEnabled {
		intelUpdatesCurrentlyEnabled = enableIntelUpdates()
		changed = true
	}

	if devMode() != previousDevMode {
		registry.SetDevMode(devMode())
		previousDevMode = devMode()
		changed = true
	}

	if releaseChannel() != previousReleaseChannel {
		previousReleaseChannel = releaseChannel()
		changed = true
	}

	if changed {
		// Update indexes based on new settings.
		warning := helper.SetIndexes(
			registry,
			releaseChannel(),
			true,
			softwareUpdatesCurrentlyEnabled,
			intelUpdatesCurrentlyEnabled,
		)
		if warning != nil {
			log.Warningf("updates: %s", warning)
		}

		// Select versions depending on new indexes and modes.
		registry.SelectVersions()
		module.EventVersionsUpdated.Submit(struct{}{})

		if softwareUpdatesCurrentlyEnabled || intelUpdatesCurrentlyEnabled {
			module.states.Clear()
			if err := TriggerUpdate(true, false); err != nil {
				log.Warningf("updates: failed to trigger update: %s", err)
			}
			log.Infof("updates: automatic updates are now enabled")
		} else {
			log.Warningf("updates: automatic updates are now completely disabled")
		}
	}

	return false, nil
}
