package customlists

import (
	"github.com/safing/portmaster/base/config"
)

var (
	// CfgOptionCustomListFileKey is the config key for custom filter list file.
	CfgOptionCustomListFileKey            = "filter/customListFile"
	cfgOptionCustomListFileOrder          = 35
	cfgOptionCustomListCategoryAnnotation = "过滤列表"
)

var getFilePath config.StringOption

func registerConfig() error {
	help := `该文件（.txt）每隔几分钟检查一次，发生变化时会自动重新加载。  

条目（每行一个）可以是以下之一：
- 域名："example.com"
- IP 地址："10.0.0.1"
- 国家/地区代码（基于 IP）："US"
- AS（自治系统）："AS1234"  

每行第一个元素之后的所有内容、以 '#' 开头的注释以及空行都会被忽略。  
"阻止过滤列表条目的子域名" 和 "阻止域名别名" 设置同样适用于自定义过滤列表。  
不支持 "Hosts" 格式的列表。  

请注意，自定义过滤列表会被完整加载到内存中。如果加载较大的列表，可能会对您的设备产生负面影响。`

	// Register a setting for the file path in the ui
	err := config.Register(&config.Option{
		Name:            "自定义过滤列表",
		Key:             CfgOptionCustomListFileKey,
		Description:     "指定自定义过滤列表（.txt）的文件路径，该列表会自动刷新。任何与文件中的域名、IP 地址、国家/地区或 ASN 匹配的连接都将被阻止。",
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
