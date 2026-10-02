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
		Name:           "进程检测",
		Key:            CfgOptionEnableProcessDetectionKey,
		Description:    "此选项启用将网络流量归属到进程的功能。如果没有它，应用设置实际上将被禁用。",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 528,
			config.CategoryAnnotation:     "开发",
		},
	})
	if err != nil {
		return err
	}
	enableProcessDetection = config.Concurrent.GetAsBool(CfgOptionEnableProcessDetectionKey, true)

	return nil
}
