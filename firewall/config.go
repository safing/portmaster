package firewall

import (
	"github.com/safing/portbase/config"
)

var (
	CfgOptionEnableFilterKey = "filter/enable"

	CfgOptionPermanentVerdictsKey = "filter/permanentVerdicts"
	permanentVerdicts             config.BoolOption

	CfgOptionPromptTimeoutKey = "filter/promptTimeout"
	promptTimeout             config.IntOption

	devMode          config.BoolOption
	apiListenAddress config.StringOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Permanent Verdicts",
		Key:            CfgOptionPermanentVerdictsKey,
		Description:    "With permanent verdicts, control of a connection is fully handed back to the OS after the initial decision. This brings a great performance increase, but makes it impossible to change the decision of a link later on.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
	})
	if err != nil {
		return err
	}
	permanentVerdicts = config.Concurrent.GetAsBool(CfgOptionPermanentVerdictsKey, true)

	err = config.Register(&config.Option{
		Name:           "Timeout for prompt notifications",
		Key:            CfgOptionPromptTimeoutKey,
		Description:    "Amount of time how long Portmaster will wait for a response when prompting about a connection via a notification. In seconds.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   60,
	})
	if err != nil {
		return err
	}
	promptTimeout = config.Concurrent.GetAsInt(CfgOptionPromptTimeoutKey, 60)

	devMode = config.Concurrent.GetAsBool("core/devMode", false)
	apiListenAddress = config.GetAsString("api/listenAddress", "")

	return nil
}
