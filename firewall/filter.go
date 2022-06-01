package firewall

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/core"
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/spn/captain"
)

var (
	filterModule  *modules.Module
	filterEnabled config.BoolOption
	tunnelEnabled config.BoolOption

	unbreakFilterListIDs         = []string{"UNBREAK"}
	resolvedUnbreakFilterListIDs []string
)

func init() {
	filterModule = modules.Register("filter", filterPrep, filterStart, nil, "core", "intel")
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

func filterPrep() (err error) {
	err = registerConfig()
	if err != nil {
		return err
	}

	filterEnabled = config.Concurrent.GetAsBool(CfgOptionEnableFilterKey, true)
	tunnelEnabled = config.Concurrent.GetAsBool(captain.CfgOptionEnableSPNKey, false)
	return nil
}

func filterStart() error {
	// TODO: Re-resolve IDs when filterlist index changes.
	resolvedIDs, err := filterlists.ResolveListIDs(unbreakFilterListIDs)
	if err != nil {
		log.Warningf("filter: failed to resolve unbreak filter list IDs: %s", err)
	} else {
		resolvedUnbreakFilterListIDs = resolvedIDs
	}
	return nil
}
