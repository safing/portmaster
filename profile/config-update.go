package profile

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/modules"
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
		func(ctx context.Context, _ interface{}) error {
			return updateGlobalConfigProfile(ctx, nil)
		},
	)
}

const globalConfigProfileErrorID = "profile:global-profile-error"

func updateGlobalConfigProfile(ctx context.Context, task *modules.Task) error {
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

	// If there was any error, try again later until it succeeds.
	if lastErr == nil {
		module.Resolve(globalConfigProfileErrorID)
	} else {
		// Create task after first failure.
		if task == nil {
			task = module.NewTask(
				"retry updating global config profile",
				updateGlobalConfigProfile,
			)
		}

		// Schedule task.
		task.Schedule(time.Now().Add(15 * time.Second))

		// Add module warning to inform user.
		module.Warning(
			globalConfigProfileErrorID,
			fmt.Sprintf("Failed to process global settings: %s", err),
		)
	}

	return lastErr
}
