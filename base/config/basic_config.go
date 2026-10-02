package config

import (
	"flag"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
)

// Configuration Keys.
var (
	CfgDevModeKey  = "core/devMode"
	defaultDevMode bool

	CfgLogLevel     = "core/log/level"
	defaultLogLevel = log.InfoLevel.String()
	logLevel        StringOption
)

func init() {
	flag.BoolVar(&defaultDevMode, "devmode", false, "enable development mode; configuration is stronger")
}

func registerBasicOptions() error {
	// Get the default log level from the log package.
	defaultLogLevel = log.GetLogLevel().Name()

	// Register logging setting.
	// The log package cannot do that, as it would trigger and import loop.
	if err := Register(&Option{
		Name:           "日志级别",
		Key:            CfgLogLevel,
		Description:    "配置日志记录级别。",
		OptType:        OptTypeString,
		ExpertiseLevel: ExpertiseLevelDeveloper,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   defaultLogLevel,
		Annotations: Annotations{
			DisplayOrderAnnotation: 513,
			DisplayHintAnnotation:  DisplayHintOneOf,
			CategoryAnnotation:     "开发",
		},
		PossibleValues: []PossibleValue{
			{
				Name:        "严重",
				Value:       "critical",
				Description: "严重级别仅记录会导致局部但迫在眉睫的故障的错误。",
			},
			{
				Name:        "错误",
				Value:       "error",
				Description: "错误级别记录可能破坏功能的错误。严重级别记录的所有内容也包含在内。",
			},
			{
				Name:        "警告",
				Value:       "warning",
				Description: "警告级别记录轻微错误及更严重的问题。错误级别记录的所有内容也包含在内。",
			},
			{
				Name:        "信息",
				Value:       "info",
				Description: "信息级别记录正在发生且用户关心的主要事件。警告级别记录的所有内容也包含在内。",
			},
			{
				Name:        "调试",
				Value:       "debug",
				Description: "调试级别记录一些额外的调试细节。信息级别记录的所有内容也包含在内。",
			},
			{
				Name:        "跟踪",
				Value:       "trace",
				Description: "跟踪级别记录大量详细信息以及操作和请求的跟踪。调试级别记录的所有内容也包含在内。",
			},
		},
	}); err != nil {
		return err
	}
	logLevel = GetAsString(CfgLogLevel, defaultLogLevel)

	// Register to hook to update the log level.
	module.EventConfigChange.AddCallback("update log level", setLogLevel)

	return Register(&Option{
		Name:           "开发模式",
		Key:            CfgDevModeKey,
		Description:    "在开发模式下，安全限制会被解除或放宽，以便为调试和测试目的提供不受限制的访问。",
		OptType:        OptTypeBool,
		ExpertiseLevel: ExpertiseLevelDeveloper,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   defaultDevMode,
		Annotations: Annotations{
			DisplayOrderAnnotation: 512,
			CategoryAnnotation:     "开发",
		},
	})
}

func loadLogLevel() error {
	return setDefaultConfigOption(CfgLogLevel, log.GetLogLevel().Name(), false)
}

func setLogLevel(_ *mgr.WorkerCtx, _ struct{}) (cancel bool, err error) {
	log.SetLogLevel(log.ParseLevel(logLevel()))

	return false, nil
}
