package customlists

import "github.com/safing/portbase/config"

var (
	// CfgOptionCustomListBlockingKey is the config key for the listen address..
	CfgOptionCustomListBlockingKey        = "filter/customListBlocking"
	cfgOptionCustomListBlockingOrder      = 37
	cfgOptionCustomListCategoryAnnotation = "Filter Lists"
)

var (
	getFilePath func() string
)

func registerConfig() error {
	// register a setting for the file path in the ui
	err := config.Register(&config.Option{
		Name:            "Custom Filter List",
		Key:             CfgOptionCustomListBlockingKey,
		Description:     "Path to the file that contains a list of Domain, IP addresses, country codes and autonomous systems that you want to block",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    "",
		RequiresRestart: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionCustomListBlockingOrder,
			config.CategoryAnnotation:     cfgOptionCustomListCategoryAnnotation,
		},
	})
	if err != nil {
		return err
	}

	getFilePath = config.GetAsString(CfgOptionCustomListBlockingKey, "")

	return nil
}
