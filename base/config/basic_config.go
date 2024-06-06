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
		Name:           "Log Level",
		Key:            CfgLogLevel,
		Description:    "Configure the logging level.",
		OptType:        OptTypeString,
		ExpertiseLevel: ExpertiseLevelDeveloper,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   defaultLogLevel,
		Annotations: Annotations{
			DisplayOrderAnnotation: 513,
			DisplayHintAnnotation:  DisplayHintOneOf,
			CategoryAnnotation:     "Development",
		},
		PossibleValues: []PossibleValue{
			{
				Name:        "Critical",
				Value:       "critical",
				Description: "The critical level only logs errors that lead to a partial, but imminent failure.",
			},
			{
				Name:        "Error",
				Value:       "error",
				Description: "The error level logs errors that potentially break functionality. Everything logged by the critical level is included here too.",
			},
			{
				Name:        "Warning",
				Value:       "warning",
				Description: "The warning level logs minor errors and worse. Everything logged by the error level is included here too.",
			},
			{
				Name:        "Info",
				Value:       "info",
				Description: "The info level logs the main events that are going on and are interesting to the user. Everything logged by the warning level is included here too.",
			},
			{
				Name:        "Debug",
				Value:       "debug",
				Description: "The debug level logs some additional debugging details. Everything logged by the info level is included here too.",
			},
			{
				Name:        "Trace",
				Value:       "trace",
				Description: "The trace level logs loads of detailed information as well as operation and request traces. Everything logged by the debug level is included here too.",
			},
		},
	}); err != nil {
		return err
	}
	logLevel = GetAsString(CfgLogLevel, defaultLogLevel)

	// Register to hook to update the log level.
	module.EventConfigChange.AddCallback("update log level", setLogLevel)

	return Register(&Option{
		Name:           "Development Mode",
		Key:            CfgDevModeKey,
		Description:    "In Development Mode, security restrictions are lifted/softened to enable unrestricted access for debugging and testing purposes.",
		OptType:        OptTypeBool,
		ExpertiseLevel: ExpertiseLevelDeveloper,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   defaultDevMode,
		Annotations: Annotations{
			DisplayOrderAnnotation: 512,
			CategoryAnnotation:     "Development",
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
