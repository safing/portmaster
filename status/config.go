package status

import "github.com/safing/portbase/config"

// Configuration Keys.
var (
	CfgEnableNetworkRatingSystemKey = "core/enableNetworkRating"
	cfgEnableNetworkRatingSystem    config.BoolOption
)

func registerConfig() error {
	if err := config.Register(&config.Option{
		Name: "Enable Network Rating System",
		Key:  CfgEnableNetworkRatingSystemKey,
		Description: `Enable the Network Rating System, which allows you to configure settings to be active in one environment but not in the other, like allowing sensitive connections at home but not at the public library.

Please note that this feature will be replaced by a superior and easier to understand system in the future.`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
			config.CategoryAnnotation:     "User Interface",
		},
	}); err != nil {
		return err
	}
	cfgEnableNetworkRatingSystem = config.Concurrent.GetAsBool(CfgEnableNetworkRatingSystemKey, false)

	return nil
}

// NetworkRatingEnabled returns true if the network rating system has been enabled.
func NetworkRatingEnabled() bool {
	return cfgEnableNetworkRatingSystem()
}

// SetNetworkRating enables or disables the network rating system.
func SetNetworkRating(enabled bool) error {
	return config.SetConfigOption(CfgEnableNetworkRatingSystemKey, enabled)
}
