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
		Name:            "API 监听地址",
		Key:             CfgDefaultListenAddressKey,
		Description:     "定义内部 API 监听的 IP 地址和端口。",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    getDefaultListenAddress(),
		ValidationRegex: "^([0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}:[0-9]{1,5}|\\[[:0-9A-Fa-f]+\\]:[0-9]{1,5})$",
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 513,
			config.CategoryAnnotation:     "开发",
		},
	})
	if err != nil {
		return err
	}
	listenAddressConfig = config.GetAsString(CfgDefaultListenAddressKey, getDefaultListenAddress())

	err = config.Register(&config.Option{
		Name:           "API 密钥",
		Key:            CfgAPIKeys,
		Description:    "定义用于特权访问 API 的 API 密钥。每个条目都是一个具有相应权限的独立 API 密钥。格式为 `<key>?read=<perm>&write=<perm>`。权限可以是 `anyone`、`user` 和 `admin`，也可以省略。",
		Sensitive:      true,
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   []string{},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
			config.CategoryAnnotation:     "开发",
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
