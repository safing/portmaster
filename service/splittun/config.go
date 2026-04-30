package splittun

import (
	"github.com/safing/portmaster/base/config"
)

var (
	CfgOptionSplitTunEnableKey   = "splittun/enable"
	cfgOptionSplitTunEnable      config.BoolOption
	cfgOptionSplitTunEnableOrder = 210
)

func prepConfig() error {
	// Register spn module setting.
	err := config.Register(&config.Option{
		Name:         "Split Tunnel Module",
		Key:          CfgOptionSplitTunEnableKey,
		Description:  "Start the Split Tunnel module. If turned off, the Split Tunnel is fully disabled on this device.",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionSplitTunEnableOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSplitTunEnable = config.Concurrent.GetAsBool(CfgOptionSplitTunEnableKey, false)

	return nil
}
