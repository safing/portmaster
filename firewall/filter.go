package firewall

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/core"
)

var filterModule *modules.Module

func init() {
	filterModule = modules.Register("filter", nil, nil, nil, "core", "interception", "intel")
	subsystems.Register(
		"filter",
		"Privacy Filter",
		"DNS and Network Filter",
		filterModule,
		"config:filter/",
		&config.Option{
			Name:           "Privacy Filter Module",
			Key:            CfgOptionEnableFilterKey,
			Description:    "Start the Privacy Filter module. If turned off, all privacy filter protections are fully disabled on this device.",
			OptType:        config.OptTypeBool,
			ExpertiseLevel: config.ExpertiseLevelDeveloper,
			ReleaseLevel:   config.ReleaseLevelStable,
			DefaultValue:   true,
			Annotations: config.Annotations{
				config.CategoryAnnotation: "General",
			},
		},
	)
}
