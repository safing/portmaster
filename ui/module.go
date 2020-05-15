package ui

import (
	"context"

	"github.com/safing/portbase/dataroot"

	resources "github.com/cookieo9/resources-go"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

const (
	eventReload = "reload"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("ui", prep, start, nil, "api", "updates")
}

func prep() error {
	module.RegisterEvent(eventReload)

	return registerRoutes()
}

func start() error {
	err := dataroot.Root().ChildDir("exec", 0777).Ensure()
	if err != nil {
		log.Warningf("ui: failed to create safe exec dir: %s", err)
	}

	return module.RegisterEventHook("ui", eventReload, "reload assets", reloadUI)
}

func reloadUI(ctx context.Context, _ interface{}) error {
	log.Info("core: user/UI requested UI reload")

	appsLock.Lock()
	defer appsLock.Unlock()

	// close all bundles
	for id, bundle := range apps {
		err := bundle.Close()
		if err != nil {
			log.Warningf("ui: failed to close bundle %s: %s", id, err)
		}
	}

	// reset index
	apps = make(map[string]*resources.BundleSequence)

	return nil
}
