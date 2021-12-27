package status

import "github.com/safing/portbase/config"

var (
	CfgEnableNetworkRatingSystemKey = "core/enableNetworkRating"
	cfgEnableNetworkRatingSystem    config.BoolOption
)

func registerConfig() error {
	if err := config.Register(&config.Option{
		Name:           "Enable Network Rating System",
		Key:            CfgEnableNetworkRatingSystemKey,
		Description:    "Enables the Network Rating System, which allows you to quickly increase security and privacy throughout the settings by changing your the network rating level in the top left. Please note that this feature is now in the sunset phase and will be replaced by a superior and easier to understand system in the future.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
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
func SetNetworkRating(enabled bool) {
	config.SetConfigOption(CfgEnableNetworkRatingSystemKey, enabled)
}
