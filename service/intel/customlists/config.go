package customlists

import (
	"github.com/safing/portmaster/base/config"
)

var (
	// CfgOptionCustomListFileKey is the config key for custom filter list file.
	CfgOptionCustomListFileKey            = "filter/customListFile"
	cfgOptionCustomListFileOrder          = 35
	cfgOptionCustomListCategoryAnnotation = "Filter Lists"
)

var getFilePath config.StringOption

func registerConfig() error {
	help := `The file (.txt) is checked every couple minutes and will be automatically reloaded when it has changed.  

Entries (one per line) may be one of:
- Domain: "example.com"
- IP Address: "10.0.0.1"
- Country Code (based on IP): "US"
- AS (Autonomous System): "AS1234"  

Everything after the first element of a line, comments starting with a '#', and empty lines are ignored.  
The settings "Block Subdomains of Filter List Entries" and "Block Domain Aliases" also apply to the custom filter list.  
Lists in the "Hosts" format are not supported.  

Please note that the custom filter list is fully loaded into memory. This can have a negative impact on your device if big lists are loaded.`

	// Register a setting for the file path in the ui
	err := config.Register(&config.Option{
		Name:            "Custom Filter List",
		Key:             CfgOptionCustomListFileKey,
		Description:     "Specify the file path to a custom filter list (.txt), which will be automatically refreshed. Any connections matching a domain, IP address, Country or ASN in the file will be blocked.",
		Help:            help,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    "",
		RequiresRestart: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionCustomListFileOrder,
			config.CategoryAnnotation:     cfgOptionCustomListCategoryAnnotation,
			config.DisplayHintAnnotation:  config.DisplayHintFilePicker,
		},
	})
	if err != nil {
		return err
	}

	getFilePath = config.GetAsString(CfgOptionCustomListFileKey, "")

	return nil
}
