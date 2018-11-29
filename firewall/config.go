package firewall

import (
	"github.com/Safing/portbase/config"
)

var (
	permanentVerdicts config.BoolOption
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

	return nil
}
