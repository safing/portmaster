package firewall

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/modules/subsystems"

	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portmaster/core"
)

var (
	filterModule  *modules.Module
	filterEnabled config.BoolOption
)

func init() {
	filterModule = modules.Register("filter", filterPrep, nil, nil, "core", "intel")
	subsystems.Register(
		"filter",
		"Privacy Filter",
		"DNS and Network Filter",
		filterModule,
		"config:filter/",
		&config.Option{
			Name:           "Enable Privacy Filter",
			Key:            CfgOptionEnableFilterKey,
			Description:    "Enable the Privacy Filter Subsystem to filter DNS queries and network requests.",
			OptType:        config.OptTypeBool,
			ExpertiseLevel: config.ExpertiseLevelUser,
			ReleaseLevel:   config.ReleaseLevelBeta,
			DefaultValue:   true,
			Annotations: config.Annotations{
				config.CategoryAnnotation: "General",
			},
		},
	)
}

func filterPrep() (err error) {
	err = registerConfig()
	if err != nil {
		return err
	}

	filterEnabled = config.GetAsBool(CfgOptionEnableFilterKey, true)
	return nil
}
