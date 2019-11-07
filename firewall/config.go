package firewall

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/status"
)

var (
	permanentVerdicts  config.BoolOption
	filterDNSByScope   status.SecurityLevelOption
	filterDNSByProfile status.SecurityLevelOption
	promptTimeout      config.IntOption

	devMode          config.BoolOption
	apiListenAddress config.StringOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Permanent Verdicts",
		Key:            "firewall/permanentVerdicts",
		Description:    "With permanent verdicts, control of a connection is fully handed back to the OS after the initial decision. This brings a great performance increase, but makes it impossible to change the decision of a link later on.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelExperimental,
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
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
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
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	filterDNSByProfile = status.ConfigIsActiveConcurrent("firewall/filterDNSByProfile")

	err = config.Register(&config.Option{
		Name:           "Timeout for prompt notifications",
		Key:            "firewall/promptTimeout",
		Description:    "Amount of time how long Portmaster will wait for a response when prompting about a connection via a notification. In seconds.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   60,
	})
	if err != nil {
		return err
	}
	promptTimeout = config.Concurrent.GetAsInt("firewall/promptTimeout", 30)

	devMode = config.Concurrent.GetAsBool("firewall/permanentVerdicts", false)
	apiListenAddress = config.GetAsString("api/listenAddress", "")

	return nil
}
