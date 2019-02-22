package firewall

import (
	"github.com/Safing/portbase/config"
	"github.com/Safing/portmaster/status"
)

var (
	permanentVerdicts  config.BoolOption
	filterDNSByScope   status.SecurityLevelOption
	filterDNSByProfile status.SecurityLevelOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Permanent Verdicts",
		Key:            "firewall/permanentVerdicts",
		Description:    "With permanent verdicts, control of a connection is fully handed back to the OS after the initial decision. This brings a great performance increase, but makes it impossible to change the decision of a link later on.",
		ExpertiseLevel: config.ExpertiseLevelExpert,
		OptType:        config.OptTypeBool,
		DefaultValue:   true,
	})
	if err != nil {
		return err
	}
	permanentVerdicts = config.Concurrent.GetAsBool("firewall/permanentVerdicts", true)

	err = config.Register(&config.Option{
		Name:            "Filter DNS Responses by Server Scope",
		Key:             "firewall/filterDNSByScope",
		Description:     "This option will filter out DNS answers that are outside of the scope of the server. A server on the public Internet may not respond with a private LAN address.",
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	filterDNSByScope = status.ConfigIsActiveConcurrent("firewall/filterDNSByScope")

	err = config.Register(&config.Option{
		Name:            "Filter DNS Responses by Application Profile",
		Key:             "firewall/filterDNSByProfile",
		Description:     "This option will filter out DNS answers that an application would not be allowed to connect, based on its profile.",
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	filterDNSByProfile = status.ConfigIsActiveConcurrent("firewall/filterDNSByProfile")

	return nil
}
