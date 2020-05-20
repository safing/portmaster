package resolver

import (
	"context"
	"strings"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/intel"

	// module dependencies
	_ "github.com/safing/portmaster/core/base"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("resolver", prep, start, nil, "base", "netenv")
}

func prep() error {
	intel.SetReverseResolver(ResolveIPAndValidate)

	return prepConfig()
}

func start() error {
	// load resolvers from config and environment
	loadResolvers()

	// reload after network change
	err := module.RegisterEventHook(
		"netenv",
		"network changed",
		"update nameservers",
		func(_ context.Context, _ interface{}) error {
			loadResolvers()
			log.Debug("resolver: reloaded nameservers due to network change")
			return nil
		},
	)
	if err != nil {
		return err
	}

	// reload after config change
	prevNameservers := strings.Join(configuredNameServers(), " ")
	err = module.RegisterEventHook(
		"config",
		"config change",
		"update nameservers",
		func(_ context.Context, _ interface{}) error {
			newNameservers := strings.Join(configuredNameServers(), " ")
			if newNameservers != prevNameservers {
				prevNameservers = newNameservers

				loadResolvers()
				log.Debug("resolver: reloaded nameservers due to config change")
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	module.StartServiceWorker(
		"mdns handler",
		5*time.Second,
		listenToMDNS,
	)

	return nil
}
