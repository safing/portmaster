package process

import (
	"github.com/safing/portmaster/base/config"
)

// Configuration Keys.
var (
	CfgOptionEnableProcessDetectionKey = "core/enableProcessDetection"

	enableProcessDetection config.BoolOption
)

func registerConfiguration() error {
	// Enable Process Detection
	// This should be always enabled. Provided as an option to disable in case there are severe problems on a system, or for debugging.
	err := config.Register(&config.Option{
		Name:           "Process Detection",
		Key:            CfgOptionEnableProcessDetectionKey,
		Description:    "This option enables the attribution of network traffic to processes. Without it, app settings are effectively disabled.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 528,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}
	enableProcessDetection = config.Concurrent.GetAsBool(CfgOptionEnableProcessDetectionKey, true)

	return nil
}
