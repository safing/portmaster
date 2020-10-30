package profile

import (
	"context"
	"fmt"
	"sync"

	"github.com/safing/portbase/config"

	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	cfgLock sync.RWMutex

	cfgDefaultAction    uint8
	cfgEndpoints        endpoints.Endpoints
	cfgServiceEndpoints endpoints.Endpoints
	cfgFilterLists      []string
)

func registerConfigUpdater() error {
	return module.RegisterEventHook(
		"config",
		"config change",
		"update global config profile",
		updateGlobalConfigProfile,
	)
}

func updateGlobalConfigProfile(ctx context.Context, data interface{}) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	var err error
	var lastErr error

	action := cfgOptionDefaultAction()
	switch action {
	case "permit":
		cfgDefaultAction = DefaultActionPermit
	case "ask":
		cfgDefaultAction = DefaultActionAsk
	case "block":
		cfgDefaultAction = DefaultActionBlock
	default:
		// TODO: module error?
		lastErr = fmt.Errorf(`default action "%s" invalid`, action)
		cfgDefaultAction = DefaultActionBlock // default to block in worst case
	}

	list := cfgOptionEndpoints()
	cfgEndpoints, err = endpoints.ParseEndpoints(list)
	if err != nil {
		// TODO: module error?
		lastErr = err
	}

	list = cfgOptionServiceEndpoints()
	cfgServiceEndpoints, err = endpoints.ParseEndpoints(list)
	if err != nil {
		// TODO: module error?
		lastErr = err
	}

	list = cfgOptionFilterLists()
	cfgFilterLists, err = filterlists.ResolveListIDs(list)
	if err != nil {
		lastErr = err
	}

	// build global profile for reference
	profile := New(SourceSpecial, "global-config")
	profile.Name = "Global Configuration"
	profile.Internal = true

	newConfig := make(map[string]interface{})
	// fill profile config options
	for key, value := range cfgStringOptions {
		newConfig[key] = value()
	}
	for key, value := range cfgStringArrayOptions {
		newConfig[key] = value()
	}
	for key, value := range cfgIntOptions {
		newConfig[key] = value()
	}
	for key, value := range cfgBoolOptions {
		newConfig[key] = value()
	}

	// expand and assign
	profile.Config = config.Expand(newConfig)

	// save profile
	err = profile.Save()
	if err != nil && lastErr == nil {
		// other errors are more important
		lastErr = err
	}

	return lastErr
}
