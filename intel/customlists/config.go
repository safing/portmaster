package customlists

import (
	"github.com/safing/portbase/config"
)

var (
	// CfgOptionCustomListBlockingKey is the config key for the listen address..
	CfgOptionCustomListBlockingKey        = "filter/customListBlocking"
	cfgOptionCustomListBlockingOrder      = 35
	cfgOptionCustomListCategoryAnnotation = "Filter Lists"
)

var getFilePath config.StringOption

func registerConfig() error {
	help := `File that contains list of all domains, Ip addresses, country codes and autonomous system that you want to block, where each entry is on a new line.  
Lines that start with a '#' symbol are ignored.  
Everything after the first space/tab is ignored.  
Example:  
#############  
\# Domains:  
example.com  
google.com  
  
\# IP addresses  
1.2.3.4  
4.3.2.1  
  
\# Countries  
AU  
BG  
  
\# Autonomous Systems  
AS123  
#############
> * All the records are stored in RAM, careful with large block lists.  
> * Hosts files are not supported.`

	// register a setting for the file path in the ui
	err := config.Register(&config.Option{
		Name:            "Custom Filter List",
		Key:             CfgOptionCustomListBlockingKey,
		Description:     "Path to the file that contains a list of Domain, IP addresses, country codes and autonomous systems that you want to block",
		Help:            help,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    "",
		RequiresRestart: false,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionCustomListBlockingOrder,
			config.CategoryAnnotation:     cfgOptionCustomListCategoryAnnotation,
			config.DisplayHintAnnotation:  config.DisplayHintFilePicker,
		},
	})
	if err != nil {
		return err
	}

	getFilePath = config.GetAsString(CfgOptionCustomListBlockingKey, "")

	return nil
}
