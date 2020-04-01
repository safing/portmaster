package process

import (
	"github.com/safing/portbase/config"
)

var (
	CfgOptionEnableProcessDetectionKey = "core/enableProcessDetection"
	enableProcessDetection             config.BoolOption
)

func registerConfiguration() error {
	// Enable Process Detection
	// This should be always enabled. Provided as an option to disable in case there are severe problems on a system, or for debugging.
	err := config.Register(&config.Option{
		Name:           "Enable Process Detection",
		Key:            CfgOptionEnableProcessDetectionKey,
		Description:    "This option enables the attribution of network traffic to processes. This should be always enabled, and effectively disables app profiles if disabled.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   true,
	})
	if err != nil {
		return err
	}
	enableProcessDetection = config.Concurrent.GetAsBool(CfgOptionEnableProcessDetectionKey, true)

	return nil
}
