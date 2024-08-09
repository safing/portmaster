package nameserver

import (
	"flag"
	"runtime"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/core"
)

// CfgDefaultNameserverAddressKey is the config key for the listen address..
const CfgDefaultNameserverAddressKey = "dns/listenAddress"

var (
	defaultNameserverAddress = "localhost:53"
	nameserverAddress        string
	nameserverAddressConfig  config.StringOption

	networkServiceMode config.BoolOption
)

func init() {
	// On Windows, packets are redirected to the same interface.
	if runtime.GOOS == "windows" {
		defaultNameserverAddress = "0.0.0.0:53"
	}

	flag.StringVar(
		&nameserverAddress,
		"nameserver-address",
		defaultNameserverAddress,
		"set default nameserver address; configuration is stronger",
	)
}

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "Internal DNS Server Listen Address",
		Key:             CfgDefaultNameserverAddressKey,
		Description:     "Defines the IP address and port on which the internal DNS Server listens.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    nameserverAddress,
		ValidationRegex: "^(localhost|[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}|\\[[:0-9A-Fa-f]+\\]):[0-9]{1,5}$",
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}
	nameserverAddressConfig = config.GetAsString(CfgDefaultNameserverAddressKey, nameserverAddress)

	networkServiceMode = config.Concurrent.GetAsBool(core.CfgNetworkServiceKey, false)

	return nil
}
