package api

import (
	"flag"

	"github.com/safing/portmaster/base/config"
)

// Config Keys.
const (
	CfgDefaultListenAddressKey = "core/listenAddress"
	CfgAPIKeys                 = "core/apiKeys"
)

var (
	listenAddressFlag    string
	listenAddressConfig  config.StringOption
	defaultListenAddress string

	configuredAPIKeys config.StringArrayOption

	devMode config.BoolOption
)

func init() {
	flag.StringVar(
		&listenAddressFlag,
		"api-address",
		"",
		"set api listen address; configuration is stronger",
	)
}

func getDefaultListenAddress() string {
	// check if overridden
	if listenAddressFlag != "" {
		return listenAddressFlag
	}
	// return internal default
	return defaultListenAddress
}

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "API Listen Address",
		Key:             CfgDefaultListenAddressKey,
		Description:     "Defines the IP address and port on which the internal API listens.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    getDefaultListenAddress(),
		ValidationRegex: "^([0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}:[0-9]{1,5}|\\[[:0-9A-Fa-f]+\\]:[0-9]{1,5})$",
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 513,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}
	listenAddressConfig = config.GetAsString(CfgDefaultListenAddressKey, getDefaultListenAddress())

	err = config.Register(&config.Option{
		Name:           "API Keys",
		Key:            CfgAPIKeys,
		Description:    "Define API keys for privileged access to the API. Every entry is a separate API key with respective permissions. Format is `<key>?read=<perm>&write=<perm>`. Permissions are `anyone`, `user` and `admin`, and may be omitted.",
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   []string{},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}
	configuredAPIKeys = config.GetAsStringArray(CfgAPIKeys, []string{})

	devMode = config.Concurrent.GetAsBool(config.CfgDevModeKey, false)

	return nil
}

// SetDefaultAPIListenAddress sets the default listen address for the API.
func SetDefaultAPIListenAddress(address string) {
	defaultListenAddress = address
}
