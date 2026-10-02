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
	// Register split tunnel module setting.
	err := config.Register(&config.Option{
		Name:         "分离隧道模块",
		Key:          CfgOptionSplitTunEnableKey,
		Description:  "启动分离隧道模块。如果关闭，此设备上的分离隧道功能将被完全禁用。",
		OptType:      config.OptTypeBool,
		DefaultValue: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionSplitTunEnableOrder,
			config.CategoryAnnotation:     "常规",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionSplitTunEnable = config.Concurrent.GetAsBool(CfgOptionSplitTunEnableKey, false)

	return nil
}
