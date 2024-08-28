package profile

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/service/intel/filterlists"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/profile/endpoints"
)

var (
	cfgLock sync.RWMutex

	cfgDefaultAction       uint8
	cfgEndpoints           endpoints.Endpoints
	cfgServiceEndpoints    endpoints.Endpoints
	cfgSPNUsagePolicy      endpoints.Endpoints
	cfgSPNTransitHubPolicy endpoints.Endpoints
	cfgSPNExitHubPolicy    endpoints.Endpoints
	cfgFilterLists         []string
)

func registerGlobalConfigProfileUpdater() error {
	module.instance.Config().EventConfigChange.AddCallback("update global config profile", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		return false, updateGlobalConfigProfile(wc.Ctx())
	})

	return nil
}

const globalConfigProfileErrorID = "profile:global-profile-error"

func updateGlobalConfigProfile(_ context.Context) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	var err error
	var lastErr error

	action := cfgOptionDefaultAction()
	switch action {
	case DefaultActionPermitValue:
		cfgDefaultAction = DefaultActionPermit
	case DefaultActionAskValue:
		cfgDefaultAction = DefaultActionAsk
	case DefaultActionBlockValue:
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

	list = cfgOptionSPNUsagePolicy()
	cfgSPNUsagePolicy, err = endpoints.ParseEndpoints(list)
	if err != nil {
		// TODO: module error?
		lastErr = err
	}

	list = cfgOptionTransitHubPolicy()
	cfgSPNTransitHubPolicy, err = endpoints.ParseEndpoints(list)
	if err != nil {
		// TODO: module error?
		lastErr = err
	}

	list = cfgOptionExitHubPolicy()
	cfgSPNExitHubPolicy, err = endpoints.ParseEndpoints(list)
	if err != nil {
		// TODO: module error?
		lastErr = err
	}

	// Build config.
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

	// Build global profile for reference.
	profile := New(&Profile{
		ID:       "global-config",
		Source:   SourceSpecial,
		Name:     "Global Configuration",
		Config:   newConfig,
		Internal: true,
	})

	// save profile
	err = profile.Save()
	if err != nil && lastErr == nil {
		// other errors are more important
		lastErr = err
	}

	// If there was any error, try again later until it succeeds.
	if lastErr == nil {
		module.states.Remove(globalConfigProfileErrorID)
	} else {
		// Create task after first failure.

		// Schedule task.
		_ = module.mgr.Delay("retry updating global config profile", 15*time.Second,
			func(w *mgr.WorkerCtx) error {
				return updateGlobalConfigProfile(w.Ctx())
			})

		// Add module warning to inform user.
		module.states.Add(mgr.State{
			ID:      globalConfigProfileErrorID,
			Name:    "Internal Settings Failure",
			Message: fmt.Sprintf("Some global settings might not be applied correctly. You can try restarting the Portmaster to resolve this problem. Error: %s", err),
			Type:    mgr.StateTypeWarning,
		})
	}

	return lastErr
}
