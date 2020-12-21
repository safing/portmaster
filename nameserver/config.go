package nameserver

import (
	"flag"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
)

// Config Keys
const (
	CfgDefaultNameserverAddressKey = "dns/listenAddress"
)

var (
	nameserverAddressFlag   string
	nameserverAddressConfig config.StringOption
)

func init() {
	flag.StringVar(&nameserverAddressFlag, "nameserver-address", "", "override nameserver listen address")
}

func logFlagOverrides() {
	if nameserverAddressFlag != "" {
		log.Warning("nameserver: dns/listenAddress default config is being overridden by the -nameserver-address flag")
	}
}

func getDefaultNameserverAddress() string {
	// check if overridden
	if nameserverAddressFlag != "" {
		return nameserverAddressFlag
	}
	// return internal default
	return defaultNameserverAddress
}

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:            "Internal DNS Server Listen Address",
		Key:             CfgDefaultNameserverAddressKey,
		Description:     "Defines the IP address and port on which the internal DNS Server listens.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelDeveloper,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    getDefaultNameserverAddress(),
		ValidationRegex: "^([0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}:[0-9]{1,5}|\\[[:0-9A-Fa-f]+\\]:[0-9]{1,5})$",
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: 514,
			config.CategoryAnnotation:     "Development",
		},
	})
	if err != nil {
		return err
	}
	nameserverAddressConfig = config.GetAsString(CfgDefaultNameserverAddressKey, getDefaultNameserverAddress())

	return nil
}
