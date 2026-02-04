package interception

import (
	"github.com/safing/portmaster/base/config"
)

// Configuration Keys.
var (
	CfgOptionSplitTunEnableKey   = "splittun/enable"
	cfgOptionSplitTunEnableOrder = 70
	splitTunEnable               config.BoolOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name: "Activate Split Tunneling",
		Key:  CfgOptionSplitTunEnableKey,
		Description: `If enabled, the Portmaster will determine for each connection whether it should be routed through specified local interface or not.

This requires the split-tunneling interface to be defined (see 'Local Interface' option).`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionSplitTunEnableOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}
	splitTunEnable = config.Concurrent.GetAsBool(CfgOptionSplitTunEnableKey, false)

	return nil
}
